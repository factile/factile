package conceptschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sampleSchema() map[string]any {
	return map[string]any{
		"$schema": dialect, "type": "object", "additionalProperties": true,
		"required":                 []any{"type", "owner"},
		"properties":               map[string]any{"type": map[string]any{"const": "Probe"}, "owner": map[string]any{"type": "string"}, "state": map[string]any{"enum": []any{"draft", "ready"}}},
		"x-factile-concept-schema": map[string]any{"format": "concept-schema/v1", "id": "probe.profile", "concept_type": "Probe"},
	}
}
func putSchema(t *testing.T, root, name string, schema any) {
	t.Helper()
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "concept-schemas", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func probe(fields map[string]any) []Concept {
	values := map[string]any{"type": "Probe", "owner": "team", "state": "ready"}
	for key, value := range fields {
		values[key] = value
	}
	return []Concept{{ID: "nested/probe", Frontmatter: values}}
}

func TestReportAndActionableDiagnostics(t *testing.T) {
	root := t.TempDir()
	putSchema(t, root, "probe.schema.json", sampleSchema())
	concepts := probe(map[string]any{"state": "unknown"})
	delete(concepts[0].Frontmatter, "owner")
	before, _ := json.Marshal(concepts)
	report, diagnostics, err := Evaluate(context.Background(), root, concepts)
	if err != nil {
		t.Fatal(err)
	}
	want := Report{Contract: "concept-schema/v1", Conformant: false, Schemas: []Summary{{Path: "/concept-schemas/probe.schema.json", ID: "probe.profile", ConceptType: "Probe"}}, EvaluatedConcepts: 1, Issues: []Issue{{Severity: "error", Code: "concept_schema_violation", Path: "/nested/probe", ConceptID: "nested/probe", SchemaID: "probe.profile", ConceptType: "Probe"}}}
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("report: %#v", report)
	}
	if len(diagnostics) != 2 || diagnostics[0].Field != "/owner" || diagnostics[0].Message != "Required field is missing" || diagnostics[1].Field != "/state" || diagnostics[1].Keyword != "enum" {
		t.Fatalf("diagnostics: %#v", diagnostics)
	}
	after, _ := json.Marshal(concepts)
	if string(before) != string(after) {
		t.Fatal("instance changed")
	}
}

func TestOptionalProfilesAndIndependentBundles(t *testing.T) {
	for _, profile := range []bool{false, true} {
		root := t.TempDir()
		if profile {
			putSchema(t, root, "probe.schema.json", sampleSchema())
		}
		concepts := probe(nil)
		concepts = append(concepts, Concept{ID: "unprofiled", Frontmatter: map[string]any{"type": "Proeb"}})
		report, _, err := Evaluate(context.Background(), root, concepts)
		if err != nil || !report.Conformant {
			t.Fatalf("%#v %v", report, err)
		}
		want := 0
		if profile {
			want = 1
		}
		if report.EvaluatedConcepts != want {
			t.Fatalf("evaluated %d", report.EvaluatedConcepts)
		}
	}
}

func TestDefinitionStagesAndHiddenTargets(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		change       func(map[string]any)
	}{
		{"missing annotation", "invalid_definition", func(s map[string]any) { delete(s, "x-factile-concept-schema") }},
		{"invalid keyword", "invalid_definition", func(s map[string]any) { s["minProperties"] = "many" }},
		{"type mismatch", "concept_type_mismatch", func(s map[string]any) { s["properties"].(map[string]any)["type"].(map[string]any)["const"] = "Other" }},
		{"remote reference", "unsupported_reference", func(s map[string]any) { s["$ref"] = "https://example.invalid/schema" }},
		{"other file", "unsupported_reference", func(s map[string]any) { s["$ref"] = "other.schema.json#/properties/type" }},
		{"unresolved pointer", "unsupported_reference", func(s map[string]any) { s["$ref"] = "#/missing" }},
		{"escaped pointer", "unsupported_reference", func(s map[string]any) { s["$ref"] = "#/~2invalid" }},
		{"hidden id", "unsupported_reference", func(s map[string]any) { s["$ref"] = "#/x-hidden"; s["x-hidden"] = map[string]any{"$id": "urn:hidden"} }},
		{"hidden remote", "unsupported_reference", func(s map[string]any) {
			s["$ref"] = "#/x-hidden"
			s["x-hidden"] = map[string]any{"$ref": "https://example.invalid/never"}
		}},
		{"hidden invalid", "invalid_definition", func(s map[string]any) { s["$ref"] = "#/x-hidden"; s["x-hidden"] = map[string]any{"type": 42} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			schema := sampleSchema()
			test.change(schema)
			putSchema(t, root, "probe.schema.json", schema)
			report, _, err := Evaluate(context.Background(), root, probe(nil))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Issues) != 1 || report.Issues[0].Details["reason"] != test.reason || report.EvaluatedConcepts != 0 || len(report.Schemas) != 0 {
				t.Fatalf("%#v", report)
			}
		})
	}
}

func TestLiteralAnnotationsAndFiniteRecursiveReferences(t *testing.T) {
	root := t.TempDir()
	schema := sampleSchema()
	schema["x-example"] = map[string]any{"$id": "literal", "$ref": "https://example.invalid"}
	schema["$defs"] = map[string]any{"node": map[string]any{"type": "object", "properties": map[string]any{"child": map[string]any{"$ref": "#/$defs/node"}}}}
	schema["properties"].(map[string]any)["tree"] = map[string]any{"$ref": "#/$defs/node"}
	putSchema(t, root, "probe.schema.json", schema)
	report, _, err := Evaluate(context.Background(), root, probe(map[string]any{"tree": map[string]any{"child": map[string]any{}}}))
	if err != nil || !report.Conformant {
		t.Fatalf("%#v %v", report, err)
	}
}

func TestDiscoveryDuplicatesAndMalformedJSON(t *testing.T) {
	root := t.TempDir()
	schema := sampleSchema()
	putSchema(t, root, "a.schema.json", schema)
	putSchema(t, root, "b.schema.json", schema)
	putSchema(t, root, "nested/ignored.schema.json", nil)
	if err := os.Symlink(filepath.Join(root, "concept-schemas/a.schema.json"), filepath.Join(root, "concept-schemas/link.schema.json")); err != nil {
		t.Fatal(err)
	}
	report, _, err := Evaluate(context.Background(), root, probe(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Schemas) != 2 || len(report.Issues) != 2 || report.EvaluatedConcepts != 0 || report.Issues[0].Code != "duplicate_concept_schema_id" || report.Issues[1].Code != "duplicate_concept_type_schema" {
		t.Fatalf("%#v", report)
	}
	for _, input := range []string{`{"type":"object","type":"string"}`, `{`, "\xff", `{} {}`} {
		tmp := t.TempDir()
		putSchema(t, tmp, "bad.schema.json", nil)
		if err := os.WriteFile(filepath.Join(tmp, "concept-schemas/bad.schema.json"), []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		report, _, err := Evaluate(context.Background(), tmp, probe(nil))
		if err != nil || report.Issues[0].Details["reason"] != "malformed_json" {
			t.Fatalf("%#v %v", report, err)
		}
	}
	outside := t.TempDir()
	putSchema(t, outside, "outside.schema.json", schema)
	tmp := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "concept-schemas"), filepath.Join(tmp, "concept-schemas")); err != nil {
		t.Fatal(err)
	}
	report, _, err = Evaluate(context.Background(), tmp, probe(nil))
	if err != nil || len(report.Schemas) != 0 {
		t.Fatalf("symlink directory followed: %#v %v", report, err)
	}
}

func TestExactNumbersAndNoCoercion(t *testing.T) {
	root := t.TempDir()
	schema := sampleSchema()
	schema["properties"].(map[string]any)["number"] = map[string]any{"type": "number", "multipleOf": json.Number("0.1")}
	putSchema(t, root, "probe.schema.json", schema)
	for _, test := range []struct {
		v     any
		valid bool
	}{{json.Number("0.3"), true}, {json.Number("1.00000000000000000000000000001"), false}, {"0.3", false}} {
		report, _, err := Evaluate(context.Background(), root, probe(map[string]any{"number": test.v}))
		if err != nil || report.Conformant != test.valid {
			t.Fatalf("%#v %v", report, err)
		}
	}
}

func TestFormatsDefaultsAndRegexDialect(t *testing.T) {
	root := t.TempDir()
	schema := sampleSchema()
	properties := schema["properties"].(map[string]any)
	properties["owner"] = map[string]any{"pattern": "^(?=team$)team$", "format": "email"}
	properties["extra"] = map[string]any{"default": "never inserted"}
	putSchema(t, root, "probe.schema.json", schema)
	concepts := probe(nil)
	report, _, err := Evaluate(context.Background(), root, concepts)
	if err != nil || !report.Conformant {
		t.Fatalf("%#v %v", report, err)
	}
	if _, exists := concepts[0].Frontmatter["extra"]; exists {
		t.Fatal("default inserted")
	}
}

func TestCandidateAndWorkLimits(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= maxSchemas; i++ {
		putSchema(t, root, fmt.Sprintf("%03d.schema.json", i), sampleSchema())
	}
	_, _, err := Evaluate(context.Background(), root, probe(nil))
	var limit *ResourceError
	if !errors.As(err, &limit) || limit.Limit != "schema_candidates" {
		t.Fatalf("%v", err)
	}
	root = t.TempDir()
	schema := sampleSchema()
	schema["properties"].(map[string]any)["items"] = map[string]any{"uniqueItems": true}
	putSchema(t, root, "probe.schema.json", schema)
	items := make([]any, 150)
	for i := range items {
		items[i] = i
	}
	report, _, err := Evaluate(context.Background(), root, probe(map[string]any{"items": items}))
	if !errors.As(err, &limit) || report.Contract != "" || limit.Limit != "evaluation_work" {
		t.Fatalf("%#v %v", report, err)
	}
}

func TestResourceFailuresNeverReturnReports(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string) ([]Concept, context.Context)
	}{
		{"bytes", func(t *testing.T, root string) ([]Concept, context.Context) {
			putSchema(t, root, "huge.schema.json", strings.Repeat("x", maxSchemaBytes))
			return probe(nil), context.Background()
		}},
		{"regex", func(t *testing.T, root string) ([]Concept, context.Context) {
			schema := sampleSchema()
			schema["properties"].(map[string]any)["owner"] = map[string]any{"pattern": "^(a+)+$"}
			putSchema(t, root, "probe.schema.json", schema)
			return probe(map[string]any{"owner": strings.Repeat("a", 32) + "!"}), context.Background()
		}},
		{"recursion", func(t *testing.T, root string) ([]Concept, context.Context) {
			schema := sampleSchema()
			schema["$ref"] = "#/x"
			schema["x"] = map[string]any{"$ref": "#/x"}
			putSchema(t, root, "probe.schema.json", schema)
			return probe(nil), context.Background()
		}},
		{"time", func(t *testing.T, root string) ([]Concept, context.Context) {
			putSchema(t, root, "probe.schema.json", sampleSchema())
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return probe(nil), ctx
		}},
		{"number", func(t *testing.T, root string) ([]Concept, context.Context) {
			putSchema(t, root, "probe.schema.json", sampleSchema())
			return probe(map[string]any{"n": json.Number("1e1000000000")}), context.Background()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			concepts, ctx := test.setup(t, root)
			start := time.Now()
			report, diagnostics, err := Evaluate(ctx, root, concepts)
			var limit *ResourceError
			if !errors.As(err, &limit) || report.Contract != "" || diagnostics != nil {
				t.Fatalf("%#v %#v %v", report, diagnostics, err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("resource failure took too long")
			}
		})
	}
}
