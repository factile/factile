package factile_test

import (
	"context"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestContextBudgetsCompleteEvidence(t *testing.T) {
	_, ws := editingWorkspace(t)
	ctx := context.Background()
	read, _ := ws.Read(ctx, "/guide", factile.ReadOptions{})
	_, err := ws.Patch(ctx, "/guide", factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision, Set: map[string]any{
		"sources":     []any{map[string]any{"id": "source", "resource": "project scope", "evidence": strings.Repeat("evidence", 3000)}},
		"verified":    map[string]any{"by": "human:claimed", "at": "2020-01-01T00:00:00Z"},
		"stale_after": "2026-09-11T08:00:00Z",
	}})
	if err != nil {
		t.Fatal(err)
	}
	opts := factile.ContextOptions{MaxTokens: 4000, Depth: 0, EvaluatedAt: "2026-09-11T08:00:00Z"}
	pack, err := ws.Context(ctx, "/", "guide", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Concepts) != 0 || len(pack.Omitted) != 1 || pack.Omitted[0].EstimatedTokens <= opts.MaxTokens || pack.Budget.UsedTokens != 0 {
		t.Fatalf("large evidence bypassed budget: %#v", pack)
	}
	opts.MaxTokens = pack.Omitted[0].EstimatedTokens
	pack, err = ws.Context(ctx, "/", "guide", opts)
	if err != nil || len(pack.Concepts) != 1 || pack.Budget.UsedTokens != opts.MaxTokens {
		t.Fatalf("exact whole-document boundary: %#v %v", pack, err)
	}
	concept := pack.Concepts[0]
	if concept.ReviewState == nil || !*concept.ReviewState.Stale || concept.ReviewState.Tier != "human-reviewed" || concept.Origin["bundle_path"] != "/" || len(concept.SourceReferences) != 1 {
		t.Fatal("context lost evidence, scope or review qualification")
	}
}
