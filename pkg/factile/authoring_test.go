package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestPortableFormattingAndPathRepairsKeepGenerationTime(t *testing.T) {
	dir, ws := editingWorkspace(t)
	ctx := context.Background()
	index := "---\nokf_version: \"0.3\"\nlicense: 'MIT' # keep\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	const extension = "x-exact: {word: 'quoted value'} # authored\n"
	original := "---\ntype: 'Note'\n" + extension +
		"generated: {by: producer/1, at: 2026-01-01, x-detail: {keep: true}}\n" +
		"verified: {by: 'human:reviewer', at: 2026-02-01}\nresource: ./policy.md\n---\n" +
		"An *example* [link](./policy.md).\n"
	if err := os.WriteFile(filepath.Join(dir, "guide.md"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	read, err := ws.Read(ctx, "/guide", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body := "An   _example_ [link](/policy.md).\n"
	repaired, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{
		ExpectedRevision: read.Concept.Revision, ReplaceBody: &body, Set: map[string]any{"resource": "/policy.md"},
	})
	if err != nil || !reflect.DeepEqual(repaired.Concept.Frontmatter["generated"], read.Concept.Frontmatter["generated"]) {
		t.Fatalf("formatting changed generation: %#v, %v", repaired, err)
	}
	edited, err := ws.Write(ctx, "/guide", factile.WriteConceptInput{
		ExpectedRevision: repaired.Concept.Revision, Markdown: "Different content.\n",
	})
	if err != nil || reflect.DeepEqual(edited.Concept.Frontmatter["generated"], read.Concept.Frontmatter["generated"]) {
		t.Fatalf("content edit did not advance generation: %#v, %v", edited, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	if !strings.Contains(string(raw), extension) || !strings.Contains(string(raw), "type: 'Note'") {
		t.Fatalf("authored metadata changed: %s", raw)
	}
	reread, err := ws.Read(ctx, "/guide", factile.ReadOptions{IncludeReview: true})
	if err != nil || reread.Concept.ReviewState.Tier != "unverified" {
		t.Fatalf("historic review remained current: %#v, %v", reread, err)
	}
}
