package factile

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/factile/factile/pkg/conceptschema"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/vfs"
)

type schemaScope struct {
	Mount         vfs.Mount
	Path          string
	Concepts      []scopedConcept
	Issues        []ValidationIssue
	SkippedReason string
}

type validationScan struct {
	Path     string
	Concepts []scopedConcept
	Issues   []ValidationIssue
	Scopes   []schemaScope
}

func baseValidation(path string, issues []ValidationIssue) ValidationResult {
	if issues == nil {
		issues = []ValidationIssue{}
	}
	return ValidationResult{Path: path, Valid: !hasErrors(issues), Issues: issues, OKF: &OKFValidationResult{Valid: !hasErrors(issues), Issues: append([]ValidationIssue{}, issues...)}, ConceptSchemas: []BundleSchemaValidation{}}
}

func (w *LocalWorkspace) validatePathScope(ctx context.Context, normalized string) (validationScan, error) {
	mounts, issues, invalid, err := w.mountsForValidationScope(ctx, normalized)
	if err != nil {
		return validationScan{}, err
	}
	scan := validationScan{Path: normalized, Issues: issues}
	seen := map[string]bool{}
	collect := func(mount vfs.Mount, prefix string, exact bool) error {
		logical := cleanVirtualJoin(mount.MountPath, prefix)
		key := mount.MountPath + "\x00" + logical
		if seen[key] {
			return nil
		}
		seen[key] = true
		scope := schemaScope{Mount: mount, Path: logical}
		if invalid[mount.MountPath] {
			scope.SkippedReason = "Source could not be loaded"
			scan.Scopes = append(scan.Scopes, scope)
			return nil
		}
		if err := ensureReadable(mount); err != nil {
			return err
		}
		if exact {
			item, issues, err := w.validateConcept(mount, prefix)
			if err != nil {
				return err
			}
			if item != nil {
				scope.Concepts = append(scope.Concepts, *item)
			}
			scope.Issues = issues
		} else {
			var err error
			scope.Concepts, scope.Issues, err = w.validateMountScope(mount, prefix)
			if err != nil {
				return err
			}
		}
		scan.Concepts = append(scan.Concepts, scope.Concepts...)
		scan.Issues = append(scan.Issues, scope.Issues...)
		scan.Scopes = append(scan.Scopes, scope)
		return nil
	}
	if normalized == "/" {
		for _, mount := range mounts {
			if err := collect(mount, "", false); err != nil {
				return validationScan{}, err
			}
		}
		return scan, nil
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		selected := mountsForVirtualPath(mounts, normalized)
		if len(selected) == 0 {
			return validationScan{}, NormalizeError(err)
		}
		for _, mount := range selected {
			if err := collect(mount, "", false); err != nil {
				return validationScan{}, err
			}
		}
		return scan, nil
	}
	scan.Path = target.Path
	if target.Kind == TargetPath && !target.Exists && !invalid[target.Mount.MountPath] {
		return validationScan{}, errorf(ErrMountNotFound, "Path not found: %s", target.Path)
	}
	if err := collect(target.Mount, target.ConceptID, target.Kind == TargetConcept); err != nil {
		return validationScan{}, err
	}
	if target.Kind != TargetConcept {
		for _, mount := range mountsForVirtualPath(mounts, normalized) {
			if err := collect(mount, "", false); err != nil {
				return validationScan{}, err
			}
		}
	}
	return scan, nil
}

func (w *LocalWorkspace) applyConceptSchemas(ctx context.Context, result ValidationResult, scopes []schemaScope) (ValidationResult, error) {
	groups := map[string][]schemaScope{}
	for _, scope := range scopes {
		groups[scope.Mount.MountPath] = append(groups[scope.Mount.MountPath], scope)
	}
	paths := make([]string, 0, len(groups))
	for path := range groups {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, bundlePath := range paths {
		group := groups[bundlePath]
		entry := BundleSchemaValidation{BundlePath: bundlePath, ScopePaths: []string{}}
		selected := map[string]bool{}
		seen := map[string]bool{}
		skipped := map[string]bool{}
		concepts := []conceptschema.Concept{}
		for _, scope := range group {
			selected[scope.Path] = true
			if scope.Path == bundlePath {
				entry.CompleteBundle = true
			}
			if scope.SkippedReason != "" {
				entry.SkippedReason = scope.SkippedReason
			}
			for _, issue := range scope.Issues {
				if issue.Severity == "error" && issue.ConceptID != "" {
					skipped[issue.ConceptID] = true
				}
			}
			for _, item := range scope.Concepts {
				if item.Concept.ConceptID == "" || okf.IsReservedFile(item.Concept.ConceptID+".md") || seen[item.Concept.ConceptID] {
					continue
				}
				seen[item.Concept.ConceptID] = true
				if conceptType, ok := item.Concept.Frontmatter["type"].(string); !ok || strings.TrimSpace(conceptType) == "" {
					skipped[item.Concept.ConceptID] = true
					continue
				}
				concepts = append(concepts, conceptschema.Concept{ID: item.Concept.ConceptID, Frontmatter: item.Concept.Frontmatter})
			}
		}
		for path := range selected {
			entry.ScopePaths = append(entry.ScopePaths, path)
		}
		sort.Strings(entry.ScopePaths)
		if entry.CompleteBundle {
			entry.ScopePaths = []string{bundlePath}
		}
		entry.SkippedConcepts = len(skipped)
		if entry.SkippedReason != "" {
			result.ConceptSchemas = append(result.ConceptSchemas, entry)
			continue
		}
		report, diagnostics, err := conceptschema.Evaluate(ctx, group[0].Mount.SourcePath, concepts)
		if err != nil {
			var resource *conceptschema.ResourceError
			if errors.As(err, &resource) {
				return ValidationResult{}, NewError("schema_resource_limit", resource.Error())
			}
			return ValidationResult{}, NewError("schema_evaluation_failed", err.Error())
		}
		if !entry.CompleteBundle || entry.SkippedConcepts > 0 {
			report.Contract = ""
		}
		entry.Result = &report
		result.ConceptSchemas = append(result.ConceptSchemas, entry)
		if !report.Conformant {
			result.Valid = false
		}
		for _, issue := range report.Issues {
			details := map[string]any{"bundle_path": bundlePath}
			if issue.SchemaID != "" {
				details["schema_id"] = issue.SchemaID
			}
			if issue.ConceptType != "" {
				details["concept_type"] = issue.ConceptType
			}
			for key, value := range issue.Details {
				details[key] = value
			}
			message := schemaIssueMessage(issue)
			result.Issues = append(result.Issues, ValidationIssue{Severity: issue.Severity, Code: issue.Code, Message: message, Path: cleanVirtualJoin(bundlePath, strings.TrimPrefix(issue.Path, "/")), ConceptID: issue.ConceptID, Details: details})
		}
		for _, diagnostic := range diagnostics {
			diagnostic.BundlePath = bundlePath
			diagnostic.Path = cleanVirtualJoin(bundlePath, strings.TrimPrefix(diagnostic.Path, "/"))
			result.SchemaDiagnostics = append(result.SchemaDiagnostics, diagnostic)
		}
	}
	sort.SliceStable(result.Issues, func(i, j int) bool {
		a, b := result.Issues[i], result.Issues[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.ConceptID < b.ConceptID
	})
	return result, nil
}

func schemaIssueMessage(issue conceptschema.Issue) string {
	switch issue.Code {
	case "concept_schema_violation":
		return "Concept frontmatter does not satisfy its schema"
	case "duplicate_concept_schema_id":
		return "Schema id is declared more than once in this bundle"
	case "duplicate_concept_type_schema":
		return "Concept type has more than one schema in this bundle"
	default:
		return "Invalid concept schema: " + issue.Details["reason"]
	}
}
