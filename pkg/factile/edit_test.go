package factile_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/revision"
)

func editingWorkspace(t *testing.T) (string, *factile.LocalWorkspace) {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir("../../testdata/bundles/editing")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile("../../testdata/bundles/editing/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, entry.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, factile.NewWorkspace(factile.WorkspaceOptions{Workspace: dir})
}

func TestOrderedPatchAtomicityAndReceipt(t *testing.T) {
	dir, ws := editingWorkspace(t)
	ctx := context.Background()
	original, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	rev := revision.DigestBytes(original)
	operations := []factile.PatchOperation{
		{Op: "replace_text", Old: "An old sentence.", New: "A new sentence."},
		{Op: "replace_text", Old: "A new sentence.", New: "A final sentence."},
		{Op: "set", Key: "status", Value: "stable"},
	}
	failures := []struct {
		name, code string
		input      factile.PatchConceptInput
	}{
		{"missing revision", factile.ErrRevisionRequired, factile.PatchConceptInput{}},
		{"stale revision", factile.ErrRevisionMismatch, factile.PatchConceptInput{ExpectedRevision: "old"}},
		{"late missing match", "text_not_found", factile.PatchConceptInput{ExpectedRevision: rev, Operations: append(append([]factile.PatchOperation{}, operations...), factile.PatchOperation{Op: "replace_text", Old: "not here", New: "new"})}},
		{"invalid type", factile.ErrValidationFailed, factile.PatchConceptInput{ExpectedRevision: rev, DeleteKeys: []string{"type"}}},
		{"invalid op", "invalid_patch", factile.PatchConceptInput{ExpectedRevision: rev, Operations: []factile.PatchOperation{{Op: "typo"}}}},
		{"irrelevant fields", "invalid_patch", factile.PatchConceptInput{ExpectedRevision: rev, Operations: []factile.PatchOperation{{Op: "set", Key: "status", Old: "ignored"}}}},
		{"invalid key", factile.ErrOKFParse, factile.PatchConceptInput{ExpectedRevision: rev, Set: map[string]any{"bad\nkey": "value"}}},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ws.Patch(ctx, "/guide", tc.input)
			if factile.ErrorCode(err) != tc.code {
				t.Fatalf("error=%v, want %s", err, tc.code)
			}
			if tc.code == factile.ErrRevisionMismatch {
				var app *factile.AppError
				if !errors.As(err, &app) || app.Details["expected_revision"] != "old" || app.Details["current_revision"] != rev {
					t.Fatalf("conflict details: %#v", app)
				}
			}
			data, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
			if string(data) != string(original) {
				t.Fatal("failed patch changed bytes")
			}
		})
	}
	result, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: rev, Operations: operations, Brief: true, Diff: true})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(string(original), "An old sentence.", "A final sentence.", 1), "status: draft", "status: stable", 1)
	data, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	comparable, err := okf.PatchFrontmatter("guide", data, nil, []string{"generated"})
	if err != nil {
		t.Fatal(err)
	}
	if string(comparable) != want || result.Concept.Revision != revision.DigestBytes(data) {
		t.Fatalf("unexpected bytes: %s", data)
	}
	r := result.Receipt
	if r == nil || !r.Changed || r.Revision != result.Concept.Revision || r.Validation.Scope != "document_frontmatter" || !r.Validation.Valid || len(r.Validation.Issues) != 0 || r.Diff == nil || !strings.Contains(*r.Diff, "+A final sentence.") {
		t.Fatalf("receipt: %#v", r)
	}
	result, err = ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: r.Revision, Set: map[string]any{"status": "stable"}, Brief: true, Diff: true})
	if err != nil || result.Receipt.Changed || result.Receipt.Revision != r.Revision || *result.Receipt.Diff != "" {
		t.Fatalf("no-op: %#v %v", result, err)
	}
}

func TestWriteAndDeprecatePreserveFrontmatter(t *testing.T) {
	dir, ws := editingWorkspace(t)
	data, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	old := strings.ReplaceAll(string(data), "\n", "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "guide.md"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := ws.Write(context.Background(), "/guide", factile.WriteConceptInput{ExpectedRevision: revision.DigestBytes([]byte(old)), Markdown: "New body\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.SplitN(old, "---\r\n", 3)
	prefix := "---\r\n" + want[1] + "---\r\n"
	saved, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	comparable, err := okf.PatchFrontmatter("guide", saved, nil, []string{"generated"})
	if err != nil {
		t.Fatal(err)
	}
	if string(comparable) != prefix+"New body\r\n" {
		t.Fatalf("write reformats frontmatter: %q", saved)
	}
	_, err = ws.Deprecate(context.Background(), "/guide", factile.DeprecateOptions{ExpectedRevision: result.Concept.Revision, Reason: "Replaced"})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ = os.ReadFile(filepath.Join(dir, "guide.md"))
	if !strings.Contains(string(saved), "# Keep this explanation.\r\n") || !strings.Contains(string(saved), "title: \"Editing Guide\"\r\n") || !strings.Contains(string(saved), "---\r\nNew body\r\n") {
		t.Fatalf("deprecate changes unrelated bytes: %q", saved)
	}
}

func TestReservedReadWriteAndSourceGuards(t *testing.T) {
	dir, ws := editingWorkspace(t)
	for _, name := range []string{"index", "log"} {
		for _, prefix := range []string{"", "---\nokf_version: \"0.2\"\n---\n"} {
			source := prefix + "Body\n"
			if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			read, err := ws.Read(context.Background(), "/"+name, factile.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			patched, err := ws.Patch(context.Background(), "/"+name, factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision, Operations: []factile.PatchOperation{{Op: "replace_text", Old: "Body", New: "Changed"}}})
			if err != nil || patched.Concept.Markdown != "Changed\n" {
				t.Fatalf("reserved patch: %#v %v", patched, err)
			}
			if _, err = ws.Write(context.Background(), "/"+name, factile.WriteConceptInput{ExpectedRevision: patched.Concept.Revision, Markdown: "Final\n"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	ro := factile.NewWorkspace(factile.WorkspaceOptions{Workspace: dir, ReadOnly: true})
	read, err := ro.Read(context.Background(), "/index", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ro.Patch(context.Background(), "/index", factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision}); factile.ErrorCode(err) != factile.ErrSourceReadOnly {
		t.Fatalf("read only: %v", err)
	}
	if _, err = ws.Patch(context.Background(), "/../guide", factile.PatchConceptInput{ExpectedRevision: "bad"}); factile.ErrorCode(err) != factile.ErrInvalidPath {
		t.Fatalf("unsafe path: %v", err)
	}
}
