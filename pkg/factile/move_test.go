package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestPortableSuccessorMoveAcrossBundleBoundary(t *testing.T) {
	ctx := context.Background()
	root, bundle := filepath.Join(t.TempDir(), "root"), filepath.Join(t.TempDir(), "bundle")
	writeRootConfig(t, root)
	writeRootConfig(t, bundle)
	mustWriteWorkspace(t, filepath.Join(bundle, "index.md"), "---\nokf_version: \"0.3\"\n---\n")
	mustWriteWorkspace(t, filepath.Join(bundle, "old.md"), "---\ntype: Guide\ntitle: Original\nresource: sheet.csv\n---\nPortable Needle [asset](sheet.csv).\n")
	mustWriteWorkspace(t, filepath.Join(bundle, "sheet.csv"), "item,count\na,1\n")
	writeOKFFile(t, filepath.Join(bundle, "inbound.md"), "Guide", "Inbound", "[Old](old.md#step)\n")
	writeOKFFile(t, filepath.Join(root, "external.md"), "Guide", "External", "[Old](/portable/old.md)\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.Mount(ctx, bundle, "/portable", factile.MountOptions{Writable: true}); err != nil {
		t.Fatal(err)
	}
	old := "/portable/old"
	replacement := "/portable/folder/new name"
	preview, err := ws.MoveWithLinks(old, replacement, "", true)
	if err != nil || !preview.KeepOld || len(preview.Changes) != 3 {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if _, err := ws.MoveWithLinks(old, replacement, preview.Plan, true); err != nil {
		t.Fatal(err)
	}
	read := func(p string) factile.Concept {
		t.Helper()
		result, err := ws.Read(ctx, p, factile.ReadOptions{IncludeReview: true})
		if err != nil {
			t.Fatal(err)
		}
		return result.Concept
	}
	if stub := read(old); stub.Successor != replacement || stub.ReviewState.Status != "deprecated" {
		t.Fatalf("stub = %#v", stub)
	}
	if moved := read(replacement); moved.Frontmatter["resource"] != "/sheet.csv" || !strings.Contains(moved.Markdown, "[asset](/sheet.csv)") {
		t.Fatalf("moved concept lost asset: %#v", moved)
	}
	if inbound := read("/portable/inbound"); !strings.Contains(inbound.Markdown, "(/folder/new%20name.md#step)") {
		t.Fatalf("in-bundle backlink = %q", inbound.Markdown)
	}
	for _, edge := range []factile.GraphEdge{
		{From: "/external", To: old, Kind: "markdown_link"},
		{From: old, To: replacement, Kind: "superseded_by"},
	} {
		graph, err := ws.Graph(ctx, "/", factile.GraphOptions{Depth: 1})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, actual := range graph.Edges {
			found = found || actual == edge
		}
		if !found {
			t.Fatalf("missing %#v in %#v", edge, graph.Edges)
		}
	}
	search, err := ws.Search(ctx, "/", "Original", factile.SearchOptions{Status: "stable"})
	if err != nil || len(search.Results) != 1 || search.Results[0].Concept.Path != replacement {
		t.Fatalf("current search = %#v, %v", search, err)
	}
	pack, err := ws.Context(ctx, "/", "Original", factile.ContextOptions{Depth: 0, CurrentOnly: true})
	if err != nil || len(pack.Concepts) != 1 || pack.Concepts[0].Path != replacement {
		t.Fatalf("current context = %#v, %v", pack, err)
	}
	pack, err = ws.Context(ctx, old, "Original", factile.ContextOptions{Depth: 0, CurrentOnly: true})
	if err != nil || len(pack.Concepts) != 0 || len(pack.Omitted) != 1 || pack.Omitted[0].Reason != "replacement_unavailable" {
		t.Fatalf("restricted context = %#v, %v", pack, err)
	}
}

func TestPortableDeprecationChain(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeRootConfig(t, root)
	mustWriteWorkspace(t, filepath.Join(root, "index.md"), "---\nokf_version: \"0.3\"\n---\n")
	for _, name := range []string{"a", "b", "c"} {
		writeOKFFile(t, filepath.Join(root, name+".md"), "Guide", name, "shared needle\n")
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	retire := func(old, replacement string) {
		t.Helper()
		read, err := ws.Read(ctx, old, factile.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := ws.Deprecate(ctx, old, factile.DeprecateOptions{ExpectedRevision: read.Concept.Revision, Reason: "Retired", Replacement: replacement})
		if err != nil || result.Concept.Successor != replacement {
			t.Fatalf("deprecation = %#v, %v", result, err)
		}
	}
	retire("/a", "/b")
	retire("/b", "/c")
	search := func() factile.SearchResults {
		t.Helper()
		results, err := ws.Search(ctx, "/", "shared needle", factile.SearchOptions{Status: "stable"})
		if err != nil {
			t.Fatal(err)
		}
		return results
	}
	if results := search(); len(results.Results) != 1 || results.Results[0].Concept.Path != "/c" {
		t.Fatalf("successor chain = %#v", results)
	}
	retire("/c", "/a")
	if results := search(); len(results.Results) != 0 || len(results.Selection.Excluded) != 3 {
		t.Fatalf("cycle = %#v", results)
	}
}
