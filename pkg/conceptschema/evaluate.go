package conceptschema

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

const dialect = "https://json-schema.org/draft/2020-12/schema"
const resourceURL = "https://factile.invalid/concept-schema.json"

type noLoader struct{}

func (noLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("external schema resolution is disabled")
}

func compiler(b *budget) *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(noLoader{})
	c.UseRegexpEngine(b.regexp)
	return c
}

type definition struct {
	summary Summary
	schema  *jsonschema.Schema
	blocked bool
}

// Evaluate returns a portable report for the supplied concepts in one physical
// bundle. A scoped caller clears Report.Contract and reports scope separately.
// On any operational error the report and diagnostics are absent.
func Evaluate(ctx context.Context, root string, concepts []Concept) (report Report, diagnostics []Diagnostic, err error) {
	defer func() {
		if err != nil {
			report = Report{}
			diagnostics = nil
		}
	}()
	defer recoverResource(&err)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b := &budget{ctx, maxWork}
	b.consume(0)
	definitions, issues, err := loadDefinitions(root, b)
	if err != nil {
		return Report{}, nil, err
	}
	report = Report{Contract: "concept-schema/v1", Schemas: []Summary{}, Issues: issues}
	for _, def := range definitions {
		report.Schemas = append(report.Schemas, def.summary)
	}
	for _, collision := range []struct {
		code  string
		value func(Summary) string
	}{
		{"duplicate_concept_schema_id", func(s Summary) string { return s.ID }},
		{"duplicate_concept_type_schema", func(s Summary) string { return s.ConceptType }},
	} {
		first := map[string]*definition{}
		for _, def := range definitions {
			key := collision.value(def.summary)
			if previous := first[key]; previous != nil {
				previous.blocked = true
				def.blocked = true
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: collision.code, Path: def.summary.Path, SchemaID: def.summary.ID, ConceptType: def.summary.ConceptType, Details: map[string]string{"conflicts_with": previous.summary.Path}})
			} else {
				first[key] = def
			}
		}
	}
	active := map[string]*definition{}
	for _, def := range definitions {
		if !def.blocked {
			active[def.summary.ConceptType] = def
		}
	}
	ordered := append([]Concept(nil), concepts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	diagnostics = []Diagnostic{}
	for _, concept := range ordered {
		conceptType, ok := concept.Frontmatter["type"].(string)
		if !ok || strings.TrimSpace(conceptType) == "" {
			return Report{}, nil, fmt.Errorf("concept schema evaluation requires base-valid concepts")
		}
		if concept.ID == "" || strings.HasPrefix(concept.ID, "/") || strings.Contains(concept.ID, "\\") {
			return Report{}, nil, fmt.Errorf("invalid bundle-relative concept id")
		}
		for _, part := range strings.Split(concept.ID, "/") {
			if part == "" || part == "." || part == ".." {
				return Report{}, nil, fmt.Errorf("invalid bundle-relative concept id")
			}
		}
		def := active[conceptType]
		if def == nil {
			continue
		}
		b.estimate(def.schema, concept.Frontmatter, 0, 0)
		report.EvaluatedConcepts++
		validationErr := def.schema.Validate(concept.Frontmatter)
		b.consume(0)
		if validationErr == nil {
			continue
		}
		var validation *jsonschema.ValidationError
		if !errors.As(validationErr, &validation) {
			return Report{}, nil, fmt.Errorf("schema evaluation failed")
		}
		path := "/" + concept.ID
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "concept_schema_violation", Path: path, ConceptID: concept.ID, SchemaID: def.summary.ID, ConceptType: def.summary.ConceptType})
		diagnostics = append(diagnostics, fieldDiagnostics(validation, path, def.summary)...)
	}
	sort.Slice(report.Issues, func(i, j int) bool {
		a, b := report.Issues[i], report.Issues[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.SchemaID < b.SchemaID
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i], diagnostics[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Keyword < b.Keyword
	})
	report.Conformant = len(report.Issues) == 0
	return report, diagnostics, nil
}

func loadDefinitions(root string, b *budget) ([]*definition, []Issue, error) {
	base, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open schema bundle")
	}
	defer base.Close()
	info, err := base.Lstat("concept-schemas")
	if errors.Is(err, os.ErrNotExist) {
		return nil, []Issue{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot inspect concept-schemas")
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, []Issue{}, nil
	}
	dir, err := base.Open("concept-schemas")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read concept-schemas")
	}
	defer dir.Close()
	var names []string
	for {
		entries, err := dir.ReadDir(128)
		if err != nil && err != io.EOF {
			return nil, nil, fmt.Errorf("cannot read concept-schemas")
		}
		for _, entry := range entries {
			b.consume(1)
			if !strings.HasSuffix(entry.Name(), ".schema.json") || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, nil, fmt.Errorf("cannot inspect schema candidate")
			}
			if info.Mode().IsRegular() {
				names = append(names, entry.Name())
				if len(names) > maxSchemas {
					return nil, nil, &ResourceError{"schema_candidates"}
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	sort.Strings(names)
	definitions := []*definition{}
	issues := []Issue{}
	if len(names) == 0 {
		return definitions, issues, nil
	}
	meta, err := compiler(b).Compile(dialect)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot load standard schema meta-schema: %w", err)
	}
	for _, name := range names {
		path := "/concept-schemas/" + name
		f, err := base.Open("concept-schemas/" + name)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read schema candidate %s", path)
		}
		data, readErr := io.ReadAll(io.LimitReader(f, maxSchemaBytes+1))
		f.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("cannot read schema candidate %s", path)
		}
		if len(data) > maxSchemaBytes {
			return nil, nil, &ResourceError{"schema_bytes"}
		}
		document, err := decodeJSON(data)
		if err != nil {
			var resource *ResourceError
			if errors.As(err, &resource) {
				return nil, nil, err
			}
			issues = append(issues, invalidIssue(Summary{Path: path}, "malformed_json"))
			continue
		}
		summary := identity(path, document)
		b.size(document, 0, true)
		if !validDefinition(document) {
			issues = append(issues, invalidIssue(summary, "invalid_definition"))
			continue
		}
		nodes, valid := executableSchemas(document, meta, b)
		if !valid {
			issues = append(issues, invalidIssue(summary, "invalid_definition"))
			continue
		}
		object := document.(map[string]any)
		if object["properties"].(map[string]any)["type"].(map[string]any)["const"] != summary.ConceptType {
			issues = append(issues, invalidIssue(summary, "concept_type_mismatch"))
			continue
		}
		if !safeReferences(document, nodes) {
			issues = append(issues, invalidIssue(summary, "unsupported_reference"))
			continue
		}
		c := compiler(b)
		if err := c.AddResource(resourceURL, document); err != nil {
			return nil, nil, fmt.Errorf("cannot register schema")
		}
		schema, err := c.Compile(resourceURL)
		if err != nil {
			var resource *ResourceError
			if errors.As(err, &resource) {
				return nil, nil, err
			}
			issues = append(issues, invalidIssue(summary, "invalid_definition"))
			continue
		}
		definitions = append(definitions, &definition{summary: summary, schema: schema})
	}
	return definitions, issues, nil
}

func identity(path string, document any) Summary {
	summary := Summary{Path: path}
	object, _ := document.(map[string]any)
	metadata, _ := object["x-factile-concept-schema"].(map[string]any)
	if value, ok := metadata["id"].(string); ok && strings.TrimSpace(value) != "" {
		summary.ID = value
	}
	if value, ok := metadata["concept_type"].(string); ok && strings.TrimSpace(value) != "" {
		summary.ConceptType = value
	}
	return summary
}

func validDefinition(document any) bool {
	object, ok := document.(map[string]any)
	if !ok {
		return false
	}
	metadata, ok := object["x-factile-concept-schema"].(map[string]any)
	if !ok || len(metadata) != 3 || metadata["format"] != "concept-schema/v1" {
		return false
	}
	summary := identity("", document)
	if summary.ID == "" || summary.ConceptType == "" {
		return false
	}
	if object["$schema"] != dialect || object["type"] != "object" {
		return false
	}
	if _, ok := object["additionalProperties"].(bool); !ok {
		return false
	}
	properties, ok := object["properties"].(map[string]any)
	if !ok {
		return false
	}
	typeSchema, ok := properties["type"].(map[string]any)
	if !ok {
		return false
	}
	constant, ok := typeSchema["const"].(string)
	if !ok || strings.TrimSpace(constant) == "" {
		return false
	}
	required, ok := object["required"].([]any)
	if !ok {
		return false
	}
	seen := map[string]bool{}
	for _, item := range required {
		key, ok := item.(string)
		if !ok || strings.TrimSpace(key) == "" || seen[key] {
			return false
		}
		seen[key] = true
	}
	return seen["type"]
}

func invalidIssue(s Summary, reason string) Issue {
	return Issue{Severity: "error", Code: "invalid_concept_schema", Path: s.Path, SchemaID: s.ID, ConceptType: s.ConceptType, Details: map[string]string{"reason": reason}}
}

func fieldDiagnostics(err *jsonschema.ValidationError, path string, s Summary) []Diagnostic {
	if len(err.Causes) > 0 {
		result := []Diagnostic{}
		for _, cause := range err.Causes {
			result = append(result, fieldDiagnostics(cause, path, s)...)
		}
		return result
	}
	field := ""
	for _, part := range err.InstanceLocation {
		field += "/" + escapePointer(part)
	}
	keys := err.ErrorKind.KeywordPath()
	keyword := "schema"
	if len(keys) > 0 {
		keyword = keys[len(keys)-1]
	}
	diagnostic := Diagnostic{Path: path, SchemaID: s.ID, ConceptType: s.ConceptType, Field: field, Keyword: keyword, Message: "Value does not satisfy " + keyword}
	if required, ok := err.ErrorKind.(*kind.Required); ok {
		result := []Diagnostic{}
		for _, name := range required.Missing {
			d := diagnostic
			d.Field += "/" + escapePointer(name)
			d.Message = "Required field is missing"
			result = append(result, d)
		}
		return result
	}
	if _, ok := err.ErrorKind.(*kind.Enum); ok {
		diagnostic.Message = "Value must be one of the allowed values"
	}
	return []Diagnostic{diagnostic}
}
