package factile

import (
	"path"
	"regexp"
	"strings"
	"time"

	graphpkg "github.com/factile/factile/pkg/graph"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func storeForMount(mount vfs.Mount) (storage.Local, error) {
	store, err := storage.NewLocal(mount.SourcePath)
	store.Portable = mount.Portable
	return store, err
}

func typedConcept(doc okf.Document) bool {
	value, _ := doc.Frontmatter["type"].(string)
	return strings.TrimSpace(value) != ""
}

func conceptReview(concept Concept, at string) okf.ReviewState {
	if concept.portable {
		return okf.PortableReview(concept.Frontmatter, at)
	}
	state, _ := okf.Review(concept.Frontmatter, at)
	return state
}

func portableConceptSources(concept Concept, mount vfs.Mount) Concept {
	store := storage.Local{Root: mount.SourcePath, Portable: true}
	lookup := func(rel string) string {
		kind := store.EntryKind(rel)
		id, candidate := okf.ConceptIDFromRel(rel)
		if kind != "file" || !candidate {
			return kind
		}
		data, _, err := store.ReadConcept(id)
		if err != nil {
			return "file"
		}
		doc, err := okf.ParseConcept(id, data)
		if err == nil && typedConcept(doc) {
			return "concept"
		}
		return "file"
	}
	resolve := func(field, value string) (string, string) {
		kind, rel := okf.ResolvePath(concept.ConceptID, value, field, mount.OKFVersion, lookup)
		if kind == "file" && !strings.HasSuffix(rel, ".md") {
			kind = "asset"
		}
		return kind, rel
	}
	concept.SourceReferences, concept.ClaimReferences, concept.MetadataDiagnostics = okf.PortableSourceReferences(concept.Frontmatter, concept.Markdown, func(resource string) (string, string) {
		kind, rel := resolve("sources[].resource", resource)
		switch kind {
		case "external", "scope":
			return kind, ""
		case "concept", "asset", "directory", "file":
			return "internal", cleanVirtualJoin(mount.MountPath, strings.TrimSuffix(rel, ".md"))
		default:
			return "internal", ""
		}
	})
	concept.MetadataDiagnostics = append(okf.PortableMetadataDiagnostics(concept.Frontmatter), concept.MetadataDiagnostics...)
	selected := ""
	seen := map[string]bool{}
	for _, reference := range okf.PathReferences(concept.Frontmatter, concept.Markdown) {
		kind, rel := resolve(reference.Field, reference.Value)
		if reference.Field == "computation" {
			selected = kind
		}
		if kind == "concept" {
			target := cleanVirtualJoin(mount.MountPath, strings.TrimSuffix(rel, ".md"))
			if target != concept.Path && !seen[target] {
				seen[target] = true
				concept.portableLinks = append(concept.portableLinks, graphpkg.Link{Raw: reference.Value, Target: target, Kind: reference.Field})
			}
		}
		if kind == "missing" || kind == "outside" || kind == "invalid" {
			code := "broken_link"
			if reference.Field != "markdown" {
				code = "broken_path_field"
			}
			if reference.Field != "sources[].resource" {
				concept.MetadataDiagnostics = append(concept.MetadataDiagnostics, okf.Diagnostic{Code: code, Field: reference.Field, Message: "Unresolved OKF path: " + reference.Value})
			}
		}
	}
	if concept.Frontmatter["type"] == "Attested Computation" {
		declaration := okf.DescribeComputation(concept.Frontmatter, concept.Markdown, selected)
		concept.Computation = &declaration
		if !declaration.Attestable {
			concept.MetadataDiagnostics = append(concept.MetadataDiagnostics, okf.Diagnostic{Code: "invalid_computation", Field: "computation", Message: "Attested Computation declaration is incomplete"})
		}
	}
	return concept
}

func portableValidation(mount vfs.Mount, doc okf.Document, concept Concept) []ValidationIssue {
	if okf.IsReservedFile(doc.ConceptID + ".md") {
		return portableReservedIssues(mount, doc)
	}
	issues := []ValidationIssue{}
	for _, diagnostic := range concept.MetadataDiagnostics {
		issues = append(issues, ValidationIssue{
			Severity: "warning", Code: diagnostic.Code, Message: diagnostic.Message,
			Path: concept.Path, ConceptID: concept.ConceptID, Details: map[string]any{"field": diagnostic.Field},
		})
	}
	return issues
}

var logDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func portableReservedIssues(mount vfs.Mount, doc okf.Document) []ValidationIssue {
	var issues []ValidationIssue
	add := func(severity, code, message string) {
		issues = append(issues, ValidationIssue{Severity: severity, Code: code, Message: message, Path: cleanVirtualJoin(mount.MountPath, doc.ConceptID)})
	}
	if doc.ConceptID == "index" {
		if mount.OKFVersion != "" && mount.OKFVersion != "0.1" && mount.OKFVersion != "0.2" && mount.OKFVersion != "0.3" {
			add("warning", "unsupported_okf_version", "Unknown OKF version: "+mount.OKFVersion)
		}
		for _, field := range []string{"okf_version", "title", "description", "license"} {
			value := doc.Frontmatter[field]
			if text, _ := value.(string); !okf.EmptyMetadata(value) && strings.TrimSpace(text) == "" {
				add("warning", "invalid_metadata", "Root index field must be a string: "+field)
			}
		}
		return issues
	}
	if doc.HasFrontmatter {
		add("error", "invalid_reserved_file", "Subdirectory indexes and logs have no frontmatter")
	}
	if path.Base(doc.ConceptID) == "log" {
		input := []byte(doc.Markdown)
		tree := goldmark.DefaultParser().Parse(text.NewReader(input))
		last := ""
		orderIssue := false
		_ = ast.Walk(tree, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if heading, ok := node.(*ast.Heading); entering && ok && heading.Level == 2 {
				value := string(heading.Text(input))
				if parsed, err := time.Parse(time.DateOnly, value); !logDate.MatchString(value) || err != nil || parsed.Year() == 0 {
					add("error", "invalid_reserved_file", "Log date heading must be YYYY-MM-DD")
				} else {
					orderIssue = orderIssue || last != "" && last < value
					last = value
				}
			}
			return ast.WalkContinue, nil
		})
		if orderIssue {
			add("warning", "invalid_log_order", "Log date headings should be newest first")
		}
	}
	return issues
}
