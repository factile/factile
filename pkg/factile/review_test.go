package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestExplicitReviewPreservesHistoryAndRevisionFence(t *testing.T) {
	dir, ws := editingWorkspace(t)
	ctx := context.Background()
	read, err := ws.Read(ctx, "/guide", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]any{"by": "human:imported", "at": "2020-01-01T00:00:00Z", "extension": true}
	prepared, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision, Set: map[string]any{"verified": old, "generated": map[string]any{"by": "producer/1", "at": "2025-01-01T00:00:00Z"}}})
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := ws.Review(ctx, "/guide", factile.ReviewOptions{ExpectedRevision: prepared.Concept.Revision})
	if err != nil {
		t.Fatal(err)
	}
	state := reviewed.Concept.ReviewState
	if state == nil || len(state.Verified) != 2 || !reflect.DeepEqual(state.Verified[0], old) || !strings.HasPrefix(state.Verified[1]["by"].(string), "process:factile/") {
		t.Fatalf("review history: %#v", state)
	}
	if state.ChangedSinceReview == nil || *state.ChangedSinceReview {
		t.Fatal("review did not cover current content")
	}
	if !reflect.DeepEqual(reviewed.Concept.Frontmatter["generated"], prepared.Concept.Frontmatter["generated"]) || reviewed.Concept.Markdown != prepared.Concept.Markdown {
		t.Fatal("review changed producer or content")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	_, err = ws.Review(ctx, "/guide", factile.ReviewOptions{ExpectedRevision: prepared.Concept.Revision})
	if factile.ErrorCode(err) != factile.ErrRevisionMismatch {
		t.Fatalf("missing revision fence: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	if string(before) != string(after) {
		t.Fatal("failed review changed content")
	}
	changed, err := ws.Write(ctx, "/guide", factile.WriteConceptInput{ExpectedRevision: reviewed.Concept.Revision, Markdown: "A later change\n"})
	if err != nil {
		t.Fatal(err)
	}
	reread, err := ws.Read(ctx, "/guide", factile.ReadOptions{IncludeReview: true})
	if err != nil || reread.Concept.ReviewState.ChangedSinceReview == nil || !*reread.Concept.ReviewState.ChangedSinceReview || !reflect.DeepEqual(changed.Concept.Frontmatter["verified"], reviewed.Concept.Frontmatter["verified"]) {
		t.Fatalf("changed review history: %v %#v", err, reread)
	}
}

func TestReviewSearchFiltersExplainExclusionsWithoutHidingReads(t *testing.T) {
	_, ws := editingWorkspace(t)
	ctx := context.Background()
	read, _ := ws.Read(ctx, "/guide", factile.ReadOptions{})
	_, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision, Set: map[string]any{"status": "deprecated", "stale_after": "2026-09-11T08:00:00Z", "workflow_status": "approved"}})
	if err != nil {
		t.Fatal(err)
	}
	all, err := ws.Search(ctx, "/", "guide", factile.SearchOptions{})
	if err != nil || len(all.Results) == 0 {
		t.Fatal("default search lost historical content")
	}
	filtered, err := ws.Search(ctx, "/", "guide", factile.SearchOptions{Status: "stable", EvaluatedAt: "2026-09-11T10:00:00+02:00"})
	if err != nil || len(filtered.Results) != 0 || len(filtered.Selection.Excluded) != 1 || filtered.Selection.Excluded[0].Reason != "status" {
		t.Fatalf("filter explanation: %#v %v", filtered, err)
	}
	stale := true
	found, err := ws.Search(ctx, "/", "guide", factile.SearchOptions{Stale: &stale, EvaluatedAt: "2026-09-11T10:00:00+02:00"})
	if err != nil || len(found.Results) != 1 || found.Results[0].Concept.ReviewState.Status != "deprecated" {
		t.Fatalf("boundary filter: %#v %v", found, err)
	}
	read, err = ws.Read(ctx, "/guide", factile.ReadOptions{IncludeReview: true})
	if err != nil || read.Concept.Frontmatter["workflow_status"] != "approved" {
		t.Fatal("explicit read or workflow metadata lost")
	}
}
