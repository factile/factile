package factile_test

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestWorkspaceSearchRelevanceOrdering(t *testing.T) {
	root := t.TempDir()
	writeRootConfig(t, root)
	writeOKFFile(
		t,
		filepath.Join(root, "workflows", "project-alignment.md"),
		"Workflow",
		"Project Alignment",
		"# Project Alignment\n\n"+strings.Repeat("existing repository align shared engineering practices\n", 3),
	)
	for _, name := range []string{"a", "b", "c"} {
		writeOKFFile(
			t,
			filepath.Join(root, "noise", name+".md"),
			"Reference",
			"Repository Engineering Practices",
			"# Noise "+strings.ToUpper(name)+"\n\n"+strings.Repeat("repository engineering practices\n", 10),
		)
	}

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	ctx := context.Background()
	judgments := []struct {
		query    string
		wantRank int
	}{
		{
			query:    "how should an existing repository align with shared engineering practices?",
			wantRank: 1,
		},
		{
			query:    "existing repository align shared engineering practices",
			wantRank: 1,
		},
	}

	var reciprocalRank float64
	var topOne, topThree int
	for _, judgment := range judgments {
		searchResults, err := ws.Search(ctx, "/", judgment.query, factile.SearchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		searchPaths := make([]string, 0, len(searchResults.Results))
		for _, result := range searchResults.Results {
			searchPaths = append(searchPaths, result.Concept.Path)
		}
		rank := slices.Index(searchPaths, "/workflows/project-alignment") + 1
		if rank != judgment.wantRank {
			t.Fatalf("%q project-alignment rank = %d, want %d; results=%#v", judgment.query, rank, judgment.wantRank, searchResults.Results)
		}

		contextPack, err := ws.Context(ctx, "/", judgment.query, factile.ContextOptions{MaxTokens: 10_000, Depth: 0})
		if err != nil {
			t.Fatal(err)
		}
		contextPaths := make([]string, 0, len(contextPack.Summaries))
		for _, summary := range contextPack.Summaries {
			contextPaths = append(contextPaths, summary.Path)
		}
		if !slices.Equal(contextPaths, searchPaths) {
			t.Fatalf("%q Context order = %#v, want Search order %#v", judgment.query, contextPaths, searchPaths)
		}
		if searchResults.Query != judgment.query || contextPack.Query != judgment.query {
			t.Fatalf("query was not preserved: search=%q context=%q want=%q", searchResults.Query, contextPack.Query, judgment.query)
		}
		repeated, err := ws.Search(ctx, "/", judgment.query, factile.SearchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(repeated, searchResults) {
			t.Fatalf("%q repeated Search changed results: first=%#v repeated=%#v", judgment.query, searchResults, repeated)
		}

		if rank == 1 {
			topOne++
		}
		if rank <= 3 {
			topThree++
		}
		reciprocalRank += 1 / float64(rank)
	}

	if topOne != 2 || topThree != 2 || reciprocalRank/float64(len(judgments)) != 1 {
		t.Fatalf("workspace relevance metrics = top-one %d/%d, top-three %d/%d, MRR %.3f", topOne, len(judgments), topThree, len(judgments), reciprocalRank/float64(len(judgments)))
	}
	t.Logf("workspace relevance: top-one 2/2, top-three 2/2, MRR 1.000")
}

func TestWorkspaceSearchAndContextUseTheSameWinningPassageOrder(t *testing.T) {
	workspace := filepath.Join("..", "..", "testdata", "bundles", "search-relevance")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{Workspace: workspace})
	ctx := context.Background()

	searchResults, err := ws.Search(ctx, "/", "lumen harbor", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(searchResults.Results) != 2 ||
		searchResults.Results[0].Concept.Path != "/guides/passage-recovery" ||
		!strings.Contains(searchResults.Results[0].Snippet, "## Recovery procedure") ||
		strings.Contains(searchResults.Results[0].Snippet, "Early observation") {
		t.Fatalf("passage-ranked Search = %#v", searchResults)
	}

	contextPack, err := ws.Context(ctx, "/", "lumen harbor", factile.ContextOptions{MaxTokens: 10_000, Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(contextPack.Summaries) != 2 ||
		contextPack.Summaries[0].Path != searchResults.Results[0].Concept.Path ||
		contextPack.Summaries[1].Path != searchResults.Results[1].Concept.Path {
		t.Fatalf("Context order = %#v, Search = %#v", contextPack.Summaries, searchResults.Results)
	}
	if len(contextPack.Concepts) != 2 ||
		!strings.Contains(contextPack.Concepts[0].Markdown, "Lumen appears alone") ||
		!strings.Contains(contextPack.Concepts[0].Markdown, "Use the lumen harbor procedure") {
		t.Fatalf("Context did not retain the complete winning concept: %#v", contextPack.Concepts)
	}
}
