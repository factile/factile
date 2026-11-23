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

func TestPortableEditsDistinguishEncodedPathFromFragment(t *testing.T) {
	for _, field := range []string{"resource", "markdown"} {
		t.Run(field, func(t *testing.T) {
			dir, ws := editingWorkspace(t)
			ctx := context.Background()
			mustWriteWorkspace(t, filepath.Join(dir, "index.md"), "---\nokf_version: \"0.3\"\n---\n")
			mustWriteWorkspace(t, filepath.Join(dir, "ledger#old.csv"), "first asset\n")
			mustWriteWorkspace(t, filepath.Join(dir, "ledger"), "second asset\n")
			original := "---\ntype: Note\ngenerated: {by: producer/1, at: 2020-01-01}\n" +
				"verified: {by: 'human:reviewer', at: 2020-02-01}\n"
			if field == "resource" {
				original += "resource: ./ledger%23old.csv\n---\nEvidence.\n"
			} else {
				original += "---\n[Evidence](./ledger%23old.csv)\n"
			}
			mustWriteWorkspace(t, filepath.Join(dir, "guide.md"), original)
			before, err := ws.Read(ctx, "/guide", factile.ReadOptions{IncludeReview: true})
			if err != nil || before.Concept.ReviewState.Tier != "human-reviewed" {
				t.Fatalf("original review = %#v, %v", before, err)
			}
			edit := func(revision, reference string) factile.Concept {
				t.Helper()
				input := factile.PatchConceptInput{ExpectedRevision: revision}
				if field == "resource" {
					input.Set = map[string]any{"resource": reference}
				} else {
					body := "[Evidence](" + reference + ")\n"
					input.ReplaceBody = &body
				}
				after, err := ws.Patch(ctx, "/guide", input)
				if err != nil {
					t.Fatal(err)
				}
				return after.Concept
			}
			repaired := edit(before.Concept.Revision, "/ledger%23old.csv")
			if !reflect.DeepEqual(repaired.Frontmatter["generated"], before.Concept.Frontmatter["generated"]) {
				t.Fatal("equivalent path changed generation time")
			}
			changed := edit(repaired.Revision, "/ledger#old.csv")
			current, err := ws.Read(ctx, "/guide", factile.ReadOptions{IncludeReview: true})
			if err != nil || current.Concept.ReviewState.Tier != "unverified" {
				t.Fatalf("review remains current for a different asset: %#v, %v", current, err)
			}
			if !reflect.DeepEqual(changed.Frontmatter["verified"], before.Concept.Frontmatter["verified"]) ||
				reflect.DeepEqual(changed.Frontmatter["generated"], before.Concept.Frontmatter["generated"]) {
				t.Fatalf("asset change should advance generation and retain review history: %#v", changed.Frontmatter)
			}
		})
	}
}
