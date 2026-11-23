package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/patch"
	"github.com/factile/factile/pkg/revision"
	"github.com/factile/factile/pkg/storage"
)

// migrateOKF performs an explicit physical bundle upgrade.
func migrateOKF(ctx context.Context, args []string, global globals, stdout io.Writer) (int, error) {
	const usageText = `factile migrate <physical-bundle-directory> [--apply] [--json]
Preview an upgrade from OKF 0.1, 0.2, or unversioned input to 0.3.
Use --apply to write the validated plan.
The selected directory is the physical bundle. Mounts are ignored.
Resolve blocking findings before applying. Historical producers and reviews are
never invented. Missing history is retained in legacy_metadata and reported.
The result includes per-file revisions, diffs, findings, and native validation.`
	if hasHelp(args) {
		return showUsage(stdout, usageText)
	}
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "")
	ordered, err := reorderFlags(args[1:], map[string]bool{"--apply": false})
	if err != nil {
		return 2, err
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		return usage(global, stdout, usageText)
	}
	store, err := storage.NewLocal(fs.Arg(0))
	if err != nil {
		return 0, factile.NormalizeError(err)
	}
	// Cached Git sources and tool-private state are never migration targets.
	for _, part := range strings.Split(filepath.ToSlash(store.Root), "/") {
		if strings.EqualFold(part, ".factile") || strings.EqualFold(part, ".git") {
			return 0, factile.NewError(factile.ErrSourceReadOnly, "Cannot migrate tool-private or Git cache content")
		}
	}
	store.Portable = true
	inventory, err := inspectMigrationInventory(store)
	if err != nil {
		return 0, err
	}
	ids := inventory.IDs
	sourceVersion := inventory.Version
	if sourceVersion == "" {
		sourceVersion = "unversioned"
	}
	type finding struct {
		Path     string `json:"path"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		Blocking bool   `json:"blocking"`
	}
	type change struct {
		Path    string `json:"path"`
		Before  string `json:"before_revision"`
		After   string `json:"after_revision"`
		Diff    string `json:"diff"`
		Applied bool   `json:"applied"`
	}
	report := struct {
		Bundle        string                   `json:"bundle"`
		TargetVersion string                   `json:"target_version"`
		SourceVersion string                   `json:"source_version"`
		Inventory     []migrationEntry         `json:"inventory"`
		Apply         bool                     `json:"apply"`
		Changes       []change                 `json:"changes"`
		Findings      []finding                `json:"findings"`
		Validation    factile.ValidationResult `json:"validation"`
	}{Bundle: store.Root, TargetVersion: "0.3", SourceVersion: sourceVersion, Inventory: inventory.Entries, Apply: *apply, Changes: []change{}, Findings: []finding{}}
	before := map[string][]byte{}
	after := map[string][]byte{}
	blocked := false
	// The closure only records diagnostics; all conversion decisions stay below.
	note := func(id, code, message string, blocking bool) {
		report.Findings = append(report.Findings, finding{id + ".md", code, message, blocking})
		blocked = blocked || blocking
	}
	for _, item := range inventory.Entries {
		if item.Kind == "nested_bundle" {
			report.Findings = append(report.Findings, finding{item.Path, "nested_bundle", "Select this physical bundle separately or remove it from the outer distribution before migrating the outer bundle", true})
			blocked = true
		}
	}
	switch inventory.Version {
	case "", "0.1", "0.2", "0.3":
	default:
		note("index", "unknown_version", "Unknown OKF version "+inventory.Version+"; migration cannot downgrade it", true)
	}

	if inventory.Version == "" || inventory.Version == "0.1" || inventory.Version == "0.2" {
		if _, exists := inventory.Inputs["index"]; !exists {
			ids = append(ids, "index")
		}
	}
	evaluatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	citationHeading := regexp.MustCompile(`^(#{1,6})[ \t]+Citations[ \t]*#*[ \t]*$`)
	heading := regexp.MustCompile(`^(#{1,6})[ \t]+`)
	linkedCitation := regexp.MustCompile(`^[-*+]\s+\[([^\]]+)\]\(([^\s)]+)\)(?:\s.*)?$`)
	bareCitation := regexp.MustCompile(`^[-*+]\s+(https?://\S+)\s*$`)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		data := inventory.Inputs[id]
		before[id] = data
		after[id] = data
		if inventory.Version == "0.3" || (inventory.Version != "" && inventory.Version != "0.1" && inventory.Version != "0.2") {
			continue
		}
		doc, err := okf.ParseConcept(id, data)
		if err != nil {
			note(id, "parse_error", err.Error(), true)
			continue
		}
		fields := doc.Frontmatter
		set := map[string]any{}
		remove := []string{}
		if id == "index" {
			updated, err := patchMigrationFrontmatter(doc, data, map[string]any{"okf_version": "0.3"}, nil, note)
			if err != nil {
				return 0, err
			}
			after[id] = updated
			continue
		}
		if okf.IsReservedFile(id + ".md") {
			if !doc.HasFrontmatter {
				continue
			}
			body := doc.Markdown
			if len(fields) > 0 {
				encoded := okf.Serialize(okf.Document{Frontmatter: fields, Order: doc.Order})
				fence := "```"
				for strings.Contains(string(encoded), fence) {
					fence += "`"
				}
				body += "\n\n### Previous navigation metadata\n\n" + fence + "yaml\n" + string(encoded) + fence + "\n"
				note(id, "index_metadata_preserved", "Reserved-file metadata is retained in the Markdown body", false)
			}
			after[id] = []byte(body)
			continue
		}
		legacy := map[string]any{}
		if existing, present := fields["legacy_metadata"]; present && fields["timestamp"] != nil {
			m, ok := existing.(map[string]any)
			if !ok {
				note(id, "conflicting_metadata", "legacy_metadata is not a mapping", true)
				continue
			}
			for k, v := range m {
				legacy[k] = v
			}
		}
		if timestamp, present := fields["timestamp"]; present {
			generated, hasGenerated := fields["generated"].(map[string]any)
			by, _ := generated["by"].(string)
			_, validTime := okf.PortableDatetime(timestamp)
			if hasGenerated && strings.TrimSpace(by) != "" && generated["at"] == nil && validTime {
				copy := map[string]any{}
				for k, v := range generated {
					copy[k] = v
				}
				copy["at"] = timestamp
				set["generated"] = copy
			} else if hasGenerated && reflect.DeepEqual(generated["at"], timestamp) {
				// An identical already recorded date needs no duplicate archive.
			} else {
				if previous, exists := legacy["timestamp"]; exists && !reflect.DeepEqual(previous, timestamp) {
					note(id, "conflicting_metadata", "timestamp conflicts with preserved legacy_metadata.timestamp", true)
					continue
				}
				legacy["timestamp"] = timestamp
				set["legacy_metadata"] = legacy
				note(id, "historical_information", "Preserved timestamp without inventing or replacing generation history", false)
			}
			remove = append(remove, "timestamp")
		}
		if status, present := fields["status"]; present && status != "draft" && status != "stable" && status != "deprecated" {
			note(id, "unknown_status", "Authored lifecycle value is retained and excluded from current-only results", false)
		}
		if value, present := fields["deprecated"]; present {
			deprecated, ok := value.(bool)
			if !ok {
				note(id, "ambiguous_deprecation", "deprecated must be a boolean to convert", true)
			} else if deprecated && fields["status"] != nil && fields["status"] != "deprecated" {
				note(id, "conflicting_status", "deprecated: true conflicts with the existing status; resolve explicitly", true)
			} else if !deprecated && fields["status"] == "deprecated" {
				note(id, "conflicting_status", "deprecated: false conflicts with status: deprecated", true)
			} else {
				if deprecated {
					set["status"] = "deprecated"
				}
				remove = append(remove, "deprecated")
			}
		}
		// Extract only explicit citation-list resources. Keep all prose and claim text;
		// no source IDs, source authors, or claim-to-source relationships are inferred.
		lines := strings.SplitAfter(doc.Markdown, "\n")
		fence := ""
		citationLevel := 0
		sawCitations := false
		sources := []any{}
		seen := map[string]bool{}
		if existing, present := fields["sources"]; present {
			sources = append(sources, okf.MetadataItems(existing)...)
			for _, item := range sources {
				if m, ok := item.(map[string]any); ok {
					if resource, ok := m["resource"].(string); ok {
						seen[resource] = true
					}
				}
			}
		}
		initialSources := len(sources)
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				marker := trimmed[:3]
				if fence == "" {
					fence = marker
				} else if fence == marker {
					fence = ""
				}
				continue
			}
			if fence != "" {
				continue
			}
			if h := heading.FindStringSubmatch(trimmed); h != nil && len(h[1]) <= citationLevel {
				citationLevel = 0
			}
			if h := citationHeading.FindStringSubmatch(trimmed); h != nil {
				citationLevel = len(h[1])
				sawCitations = true
				lines[i] = strings.Replace(line, "Citations", "Sources", 1)
				continue
			}
			if citationLevel == 0 || trimmed == "" {
				continue
			}
			resource, title := "", ""
			if m := linkedCitation.FindStringSubmatch(trimmed); m != nil {
				resource, title = m[2], m[1]
			} else if m := bareCitation.FindStringSubmatch(trimmed); m != nil {
				resource = m[1]
			} else {
				note(id, "ambiguous_citation", "Citation text retained; resolve its resource explicitly before migration: "+trimmed, true)
				continue
			}
			if !seen[resource] {
				entry := map[string]any{"resource": resource}
				if title != "" {
					entry["title"] = title
				}
				sources = append(sources, entry)
				seen[resource] = true
			}
		}
		if sawCitations && len(sources) > initialSources {
			set["sources"] = sources
		}
		migrateMetadataPaths(store, doc, set, note)
		updated, err := patchMigrationFrontmatter(doc, data, set, remove, note)
		if err != nil {
			return 0, err
		}
		if sawCitations {
			updated = append(updated[:len(updated)-len(doc.Markdown)], []byte(strings.Join(lines, ""))...)
		}
		after[id] = updated
		candidate, err := okf.ParseConcept(id, updated)
		if err != nil {
			note(id, "candidate_parse_error", err.Error(), true)
			continue
		}
		prior, _ := okf.Review(fields, evaluatedAt)
		current := okf.PortableReview(candidate.Frontmatter, evaluatedAt)
		if prior.Tier != current.Tier {
			note(id, "review_tier_changed", prior.Tier+" → "+current.Tier+" at "+evaluatedAt, false)
		}
	}
	// Validate the complete physical candidate in isolation.
	stage, err := os.MkdirTemp("", "factile-migrate-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(stage)
	bundle := stage
	if err := copyMigrationAssets(store, bundle, inventory.Entries); err != nil {
		return 0, err
	}
	staged, err := storage.NewLocal(bundle)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err = staged.AtomicReplace(id, after[id]); err != nil {
			return 0, err
		}
		if !bytes.Equal(before[id], after[id]) {
			report.Changes = append(report.Changes, change{Path: id + ".md", Before: migrationRevision(before[id]), After: revision.DigestBytes(after[id]), Diff: patch.Diff(id+".md", string(before[id]), string(after[id]))})
		}
	}
	report.Validation, err = factile.NewWorkspace(factile.WorkspaceOptions{OKFBundle: stage, IncludeConceptSchemas: true}).Validate(ctx, "/", factile.ValidateOptions{})
	if err != nil {
		return 0, err
	}
	blocked = blocked || !report.Validation.Valid
	if *apply && !blocked {
		targets := []string{}
		for _, id := range ids {
			file, e := store.ConceptFile(id)
			if e != nil {
				return 0, e
			}
			targets = append(targets, file)
		}
		err = storage.WithFileLocks(targets, func() error {
			// Check all observed input revisions before replacing any file.
			current, e := inspectMigrationInventory(store)
			if e != nil {
				return e
			}
			if !reflect.DeepEqual(current.Entries, inventory.Entries) {
				return factile.NewError(factile.ErrRevisionMismatch, "Bundle document inventory changed; preview again")
			}
			for _, id := range ids {
				current, _, e := store.ReadConcept(id)
				if errors.Is(e, os.ErrNotExist) && before[id] == nil {
					continue
				}
				if e != nil {
					return e
				}
				if !bytes.Equal(current, before[id]) {
					return factile.NewError(factile.ErrRevisionMismatch, "Bundle changed while migration was being prepared; preview again")
				}
			}
			for i := range report.Changes {
				item := &report.Changes[i]
				id := strings.TrimSuffix(item.Path, ".md")
				if e := ctx.Err(); e != nil {
					return e
				}
				if e := store.AtomicReplace(id, after[id]); e != nil {
					return e
				}
				item.Applied = true
			}
			return nil
		})
		if err != nil {
			note("", "apply_failed", err.Error(), true)
		}
	}
	sort.SliceStable(report.Findings, func(i, j int) bool { return report.Findings[i].Path < report.Findings[j].Path })
	if global.structuredOutput() {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if e := enc.Encode(report); e != nil {
			return 0, e
		}
	} else {
		fmt.Fprintf(stdout, "Bundle: %s\nSource version: %s\nInventory entries: %d\nChanges: %d; apply: %t; valid: %t\n", report.Bundle, report.SourceVersion, len(report.Inventory), len(report.Changes), report.Apply, report.Validation.Valid)
		for _, item := range report.Changes {
			fmt.Fprint(stdout, item.Diff)
		}
		for _, item := range report.Findings {
			fmt.Fprintf(stdout, "%s: %s (blocking=%t): %s\n", item.Path, item.Code, item.Blocking, item.Message)
		}
		for _, item := range report.Validation.Issues {
			fmt.Fprintf(stdout, "%s: %s: %s\n", item.Path, item.Code, item.Message)
		}
	}
	if blocked {
		return 3, nil
	}
	return 0, nil
}

func migrationRevision(data []byte) string {
	if data == nil {
		return ""
	}
	return revision.DigestBytes(data)
}

func patchMigrationFrontmatter(doc okf.Document, data []byte, set map[string]any, remove []string, note func(string, string, string, bool)) ([]byte, error) {
	updated, err := okf.PatchFrontmatter(doc.ConceptID, data, set, remove)
	if err == nil || !strings.Contains(err.Error(), "metadata patches require a block mapping") {
		return updated, err
	}
	doc.Frontmatter = maps.Clone(doc.Frontmatter)
	for key, value := range set {
		doc.Frontmatter[key] = value
	}
	for _, key := range remove {
		delete(doc.Frontmatter, key)
	}
	body := doc.Markdown
	doc.Markdown = ""
	note(doc.ConceptID, "frontmatter_presentation", "Flow frontmatter serialized as a block mapping; portable values and Markdown are preserved", false)
	return append(okf.Serialize(doc), []byte(body)...), nil
}
