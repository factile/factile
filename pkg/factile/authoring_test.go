package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/okf"
)

func TestContentEditsPreserveReviewHistoryAndNoOps(t *testing.T) {
	dir, ws := editingWorkspace(t)
	ctx := context.Background()
	read, err := ws.Read(ctx, "/guide", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	historical := map[string]any{"by": "human:original", "at": "2026-01-01T00:00:00Z", "custom": "preserved"}
	review := map[string]any{"by": "human:reviewer", "at": "2026-01-02T00:00:00Z"}
	prepared, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision, Set: map[string]any{"generated": historical, "verified": review}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ws.Write(ctx, "/guide", factile.WriteConceptInput{ExpectedRevision: prepared.Concept.Revision, Markdown: "Changed content\n"})
	if err != nil {
		t.Fatal(err)
	}
	generated := changed.Concept.Frontmatter["generated"].(map[string]any)
	if generated["by"] == "human:original" || generated["custom"] != "preserved" {
		t.Fatalf("wrong current producer or lost extension: %#v", generated)
	}
	if _, valid := okf.Datetime(generated["at"]); !valid {
		t.Fatalf("missing current change time: %#v", generated)
	}
	if !reflect.DeepEqual(changed.Concept.Frontmatter["verified"], review) {
		t.Fatal("content edit changed review history")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	noop, err := ws.Write(ctx, "/guide", factile.WriteConceptInput{ExpectedRevision: changed.Concept.Revision, Markdown: changed.Concept.Markdown})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	if string(before) != string(after) || noop.Concept.Revision != changed.Concept.Revision {
		t.Fatal("no-op regenerated content")
	}
	deadline, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: noop.Concept.Revision, Set: map[string]any{"stale_after": "2027-01-01T00:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deadline.Concept.Frontmatter["generated"], generated) {
		t.Fatal("deadline edit regenerated content")
	}
}
