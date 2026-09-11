package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestCLIValidationSchemaGoldens(t *testing.T) {
	for _, scenario := range []string{"valid", "unprofiled", "no-match", "base-invalid", "profile-invalid", "malformed-schema", "scoped"} {
		t.Run(scenario, func(t *testing.T) {
			workspace := t.TempDir()
			copyTestDir(t, "../../testdata/bundles/schema-profiles", workspace)
			path := "/"
			code := 0
			switch scenario {
			case "valid":
				if err := os.Remove(filepath.Join(workspace, "bad.md")); err != nil {
					t.Fatal(err)
				}
			case "unprofiled":
				if err := os.Remove(filepath.Join(workspace, "concept-schemas/note.schema.json")); err != nil {
					t.Fatal(err)
				}
			case "no-match":
				for _, name := range []string{"bad.md", "good.md"} {
					if err := os.Remove(filepath.Join(workspace, name)); err != nil {
						t.Fatal(err)
					}
				}
			case "base-invalid":
				writeCLITestFile(t, filepath.Join(workspace, "bad.md"), "---\ntitle: Missing type\n---\n")
				code = 3
			case "profile-invalid":
				code = 3
			case "malformed-schema":
				writeCLITestFile(t, filepath.Join(workspace, "concept-schemas/note.schema.json"), "{")
				code = 3
			case "scoped":
				path = "/good"
			}
			for _, format := range []string{"json", "text"} {
				var stdout, stderr bytes.Buffer
				args := []string{"--workspace", workspace, "validate", path, "--color", "never"}
				if format == "json" {
					args = append(args, "--json")
				}
				actual := Run(context.Background(), args, nil, &stdout, &stderr)
				if actual != code || stderr.Len() != 0 {
					t.Fatalf("exit %d want %d: %s", actual, code, stderr.String())
				}
				if format == "json" {
					var result factile.ValidationResult
					if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.OKF == nil || len(result.ConceptSchemas) != 1 || result.Valid != (code == 0) {
						t.Fatalf("lost validation result: %s", stdout.String())
					}
					if scenario == "profile-invalid" && (!result.OKF.Valid || len(result.SchemaDiagnostics) != 2) {
						t.Fatal("profile errors are not separate and actionable")
					}
					if scenario == "scoped" && (result.ConceptSchemas[0].CompleteBundle || result.ConceptSchemas[0].Result.Contract != "") {
						t.Fatal("scoped result claims complete conformance")
					}
				}
				golden := filepath.Join("../../testdata/golden/schema-validation", scenario+"."+format)
				if os.Getenv("UPDATE_SCHEMA_GOLDENS") == "1" {
					if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, stdout.Bytes(), 0644); err != nil {
						t.Fatal(err)
					}
				}
				expected, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(expected, stdout.Bytes()) {
					t.Fatalf("%s mismatch:\n%s", golden, stdout.String())
				}
			}
		})
	}
}

func TestCLIResourceFailureHasNoSuccessReport(t *testing.T) {
	workspace := t.TempDir()
	copyTestDir(t, "../../testdata/bundles/schema-profiles", workspace)
	writeCLITestFile(t, filepath.Join(workspace, "concept-schemas/huge.schema.json"), strings.Repeat(" ", (1<<20)+1))
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", workspace, "validate", "/", "--json"}, nil, &stdout, &stderr)
	if code == 0 || code == 3 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "schema_resource_limit") {
		t.Fatalf("exit %d: %s %s", code, stdout.String(), stderr.String())
	}
}
