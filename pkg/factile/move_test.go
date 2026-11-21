package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestMoveRejectsOccupiedPathsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "original.md"), "Guide", "Original", "Original text.\n")
	writeOKFFile(t, filepath.Join(root, "folder", "guide.md"), "Guide", "Nested", "Nested text.\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	assertCollision := func(destination, plan string) {
		t.Helper()
		if _, err := ws.MoveWithLinks("/original", destination, plan); factile.ErrorCode(err) != factile.ErrPathAlreadyExists {
			t.Fatalf("move to %s returned %v", destination, err)
		}
		if _, err := ws.Read(context.Background(), "/original", factile.ReadOptions{}); err != nil {
			t.Fatalf("source was lost: %v", err)
		}
	}
	assertCollision("/folder", "")
	assertCollision("/original/child", "")
	preview, err := ws.MoveWithLinks("/original", "/new", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertCollision("/new", preview.Plan)
}

func TestMovePreservesLinkSyntaxWithWhitespaceAndFragments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "original.md"), "Guide", "Original", "## Steps\n")
	writeOKFFile(t, filepath.Join(root, "guide.md"), "Guide", "Link",
		"[Inline](original.md#steps \"Title\") and [Reference][steps].\n\n[steps]: <original.md#steps>\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	preview, err := ws.MoveWithLinks("/original", "/new name", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.MoveWithLinks("/original", "/new name", preview.Plan); err != nil {
		t.Fatal(err)
	}
	read, err := ws.Read(ctx, "/guide", factile.ReadOptions{})
	if want := "\n[Inline](<new name.md#steps> \"Title\") and [Reference][steps].\n\n[steps]: <new name.md#steps>\n"; err != nil || read.Concept.Markdown != want {
		t.Fatalf("rewritten Markdown: %q, %v", read.Concept.Markdown, err)
	}
	graph, err := ws.Graph(ctx, "/guide", factile.GraphOptions{Depth: 1})
	if err != nil || len(graph.Edges) != 2 || graph.Edges[0].To != "/new name" || len(graph.Issues) != 0 {
		t.Fatalf("rewritten links did not resolve: %#v, %v", graph, err)
	}
}
