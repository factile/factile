package factile_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func writeProfile(t *testing.T, root, owner string) {
	t.Helper()
	schema := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": true, "required": []string{"type", "owner"}, "properties": map[string]any{"type": map[string]any{"const": "Probe"}, "owner": map[string]any{"const": owner}}, "x-factile-concept-schema": map[string]any{"format": "concept-schema/v1", "id": "same-id", "concept_type": "Probe"}}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteWorkspace(t, filepath.Join(root, "concept-schemas/probe.schema.json"), string(data))
}

func schemaWorkspace(t *testing.T) (*factile.LocalWorkspace, string, map[string]string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	writeRootConfig(t, root)
	writeProfile(t, root, "root")
	mustWriteWorkspace(t, filepath.Join(root, "probe.md"), "---\ntype: Probe\nowner: root\n---\n")
	bundles := map[string]string{}
	for _, name := range []string{"alpha", "beta"} {
		bundle := filepath.Join(t.TempDir(), name)
		bundles[name] = bundle
		mustWriteWorkspace(t, filepath.Join(bundle, "factile.toml"), "version = 2\n[bundle]\nname = "+strconv.Quote(name)+"\n")
		writeProfile(t, bundle, name)
		mustWriteWorkspace(t, filepath.Join(bundle, "probe.md"), "---\ntype: Probe\nowner: "+name+"\n---\n")
		mustWriteWorkspace(t, filepath.Join(root, name+".mount.toml"), "source = "+strconv.Quote(bundle)+"\nwritable = true\n")
	}
	return factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root}), root, bundles
}

func TestSchemaWorkspaceIsolationAndScope(t *testing.T) {
	ws, _, bundles := schemaWorkspace(t)
	ctx := context.Background()
	result, err := ws.Validate(ctx, "/", factile.ValidateOptions{})
	if err != nil || !result.Valid || !result.OKF.Valid {
		t.Fatalf("%#v %v", result, err)
	}
	if len(result.ConceptSchemas) != 3 {
		t.Fatalf("%#v", result.ConceptSchemas)
	}
	for _, entry := range result.ConceptSchemas {
		if !entry.CompleteBundle || entry.Result.Contract != "concept-schema/v1" || entry.Result.EvaluatedConcepts != 1 || len(entry.Result.Schemas) != 1 {
			t.Fatalf("%#v", entry)
		}
	}
	mustWriteWorkspace(t, filepath.Join(bundles["alpha"], "bad.md"), "---\ntype: Probe\nowner: beta\n---\n")
	result, err = ws.Validate(ctx, "/alpha/probe", factile.ValidateOptions{})
	if err != nil || !result.Valid {
		t.Fatalf("%#v %v", result, err)
	}
	if len(result.ConceptSchemas) != 1 || result.ConceptSchemas[0].CompleteBundle || result.ConceptSchemas[0].Result.Contract != "" || result.ConceptSchemas[0].Result.EvaluatedConcepts != 1 {
		t.Fatalf("%#v", result.ConceptSchemas)
	}
	result, err = ws.Validate(ctx, "/alpha", factile.ValidateOptions{})
	if err != nil || result.Valid || !result.OKF.Valid {
		t.Fatalf("%#v %v", result, err)
	}
	if len(result.Issues) != 1 || result.Issues[0].Path != "/alpha/bad" || result.ConceptSchemas[0].Result.Issues[0].Path != "/bad" {
		t.Fatalf("paths leak or overlap: %#v", result)
	}
	if _, err := ws.SetView(ctx, "selected", factile.ViewInput{Paths: []string{"/alpha/probe", "/beta"}}); err != nil {
		t.Fatal(err)
	}
	result, err = ws.Validate(ctx, "/", factile.ValidateOptions{View: "selected"})
	if err != nil || !result.Valid || len(result.ConceptSchemas) != 2 {
		t.Fatalf("%#v %v", result, err)
	}
	if result.ConceptSchemas[0].CompleteBundle || !result.ConceptSchemas[1].CompleteBundle {
		t.Fatalf("%#v", result.ConceptSchemas)
	}
	if _, err := ws.SetView(ctx, "overlap", factile.ViewInput{Paths: []string{"/alpha", "/alpha/probe"}}); err != nil {
		t.Fatal(err)
	}
	result, err = ws.Validate(ctx, "/", factile.ValidateOptions{View: "overlap"})
	if err != nil || len(result.ConceptSchemas) != 1 || result.ConceptSchemas[0].Result.EvaluatedConcepts != 2 {
		t.Fatalf("%#v %v", result, err)
	}
}

func TestSchemaWorkspaceBaseGateAndCoverage(t *testing.T) {
	ws, root, bundles := schemaWorkspace(t)
	mustWriteWorkspace(t, filepath.Join(root, "bad.md"), "---\ntype: [\n---\n")
	mustWriteWorkspace(t, filepath.Join(root, "missing.md"), "---\ntitle: Missing type\n---\n")
	mustWriteWorkspace(t, filepath.Join(root, "unprofiled.md"), "---\ntype: Proeb\n---\n")
	if err := os.Remove(filepath.Join(bundles["alpha"], "concept-schemas/probe.schema.json")); err != nil {
		t.Fatal(err)
	}
	mustWriteWorkspace(t, filepath.Join(bundles["beta"], "probe.md"), "---\ntype: Unprofiled\n---\n")
	result, err := ws.Validate(context.Background(), "/", factile.ValidateOptions{})
	if err != nil || result.Valid || result.OKF.Valid {
		t.Fatalf("%#v %v", result, err)
	}
	if result.ConceptSchemas[0].SkippedConcepts != 2 || result.ConceptSchemas[0].Result.Contract != "" || result.ConceptSchemas[0].Result.EvaluatedConcepts != 1 {
		t.Fatalf("base gate: %#v", result.ConceptSchemas[0])
	}
	if len(result.ConceptSchemas[1].Result.Schemas) != 0 || result.ConceptSchemas[1].Result.EvaluatedConcepts != 0 {
		t.Fatal("no-profile coverage lost")
	}
	if len(result.ConceptSchemas[2].Result.Schemas) != 1 || result.ConceptSchemas[2].Result.EvaluatedConcepts != 0 {
		t.Fatal("no-matching-type coverage lost")
	}
}

func TestSchemaValidationDoesNotGateWrites(t *testing.T) {
	ws, _, _ := schemaWorkspace(t)
	ctx := context.Background()
	created, err := ws.Create(ctx, "/alpha/new", factile.CreateConceptInput{Type: "Probe", Title: "New", Markdown: "# New\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Write(ctx, "/alpha/new", factile.WriteConceptInput{ExpectedRevision: created.Concept.Revision, Markdown: "# Changed\n"}); err != nil {
		t.Fatal(err)
	}
	result, err := ws.Validate(ctx, "/alpha/new", factile.ValidateOptions{})
	if err != nil || result.Valid || !result.OKF.Valid {
		t.Fatalf("%#v %v", result, err)
	}
}

func TestSchemaUnavailableAndResourceFailureAreNotSuccess(t *testing.T) {
	ws, root, _ := schemaWorkspace(t)
	mustWriteWorkspace(t, filepath.Join(root, "invalid.mount.toml"), "source = \"https://example.invalid/repo.git\"\nwritable = false\nrevision = \"bad\"\n")
	result, err := ws.Validate(context.Background(), "/invalid", factile.ValidateOptions{})
	if err != nil || result.Valid || len(result.ConceptSchemas) != 1 || result.ConceptSchemas[0].Result != nil || result.ConceptSchemas[0].SkippedReason == "" {
		t.Fatalf("%#v %v", result, err)
	}
	mustWriteWorkspace(t, filepath.Join(root, "concept-schemas/huge.schema.json"), strings.Repeat(" ", 1<<20+1))
	result, err = ws.Validate(context.Background(), "/probe", factile.ValidateOptions{})
	if factile.ErrorCode(err) != "schema_resource_limit" || result.Valid || len(result.ConceptSchemas) != 0 {
		t.Fatalf("%#v %v", result, err)
	}
}

func TestSchemaGitUsesItsCachedPhysicalBundle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	writeProfile(t, root, "root")
	remote, remotePath, source := gitWorkspaceRemote(t)
	writeProfile(t, source, "git")
	mustWriteWorkspace(t, filepath.Join(source, "probe.md"), "---\ntype: Probe\nowner: git\n---\n")
	gitWorkspaceRun(t, source, "add", ".")
	gitWorkspaceRun(t, source, "commit", "-m", "Add schema fixture")
	gitWorkspaceRun(t, source, "push", remotePath, "HEAD")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.Mount(context.Background(), remote, "/git", factile.MountOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := ws.Validate(context.Background(), "/git", factile.ValidateOptions{})
	if err != nil || !result.Valid || result.ConceptSchemas[0].Result.EvaluatedConcepts != 1 {
		t.Fatalf("%#v %v", result, err)
	}
	if result.ConceptSchemas[0].BundlePath != "/git" || result.ConceptSchemas[0].Result.Schemas[0].Path != "/concept-schemas/probe.schema.json" {
		t.Fatalf("%#v", result.ConceptSchemas)
	}
}
