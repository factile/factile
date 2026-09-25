package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestSourcesStayInMountedBundleAndRespectViews(t *testing.T) {
	root, ws := editingWorkspace(t)
	ctx := context.Background()
	source := t.TempDir()
	files := map[string]string{
		"factile.toml":       "version = 2\n[bundle]\nname = \"source-evidence\"\n",
		"refs/policy.md":     "---\ntype: Reference\n---\n# Policy\n",
		"guides/decision.md": "---\ntype: Note\nsources:\n  - {id: policy, resource: /refs/policy.md, author: 'Policy team', custom: {keep: true}}\n  - {id: relative, resource: ../refs/policy.md}\n  - {id: external, resource: 'factile://elsewhere/private'}\n---\n# Decision\n\nDecision evidence.[^policy]\n",
	}
	for name, body := range files {
		p := filepath.Join(source, name)
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, "refs"), 0755)
	os.WriteFile(filepath.Join(root, "refs/policy.md"), []byte("---\ntype: Note\n---\n# Root decoy\n"), 0644)
	if _, err := ws.Mount(ctx, source, "/mounted", factile.MountOptions{Writable: true}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "root-reference.md"), []byte("---\ntype: Note\nsources: [{resource: /mounted/refs/policy.md}]\n---\n# UniqueRootReference\n"), 0644)
	rootPack, err := ws.Context(ctx, "/", "UniqueRootReference", factile.ContextOptions{Depth: 1, MaxTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rootPack.Concepts) != 1 {
		t.Fatal("source traversal crossed its physical bundle", rootPack)
	}
	read, err := ws.Read(ctx, "/mounted/guides/decision", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range read.Concept.SourceReferences[:2] {
		if ref.Target != "/mounted/refs/policy" {
			t.Fatal("wrong owning bundle", ref)
		}
	}
	if read.Concept.SourceReferences[2].Target != "" {
		t.Fatal("external source dereferenced")
	}
	if len(read.Concept.ClaimReferences) != 1 || !read.Concept.ClaimReferences[0].Resolved {
		t.Fatal(read.Concept.ClaimReferences)
	}
	graph, err := ws.Graph(ctx, "/mounted/refs/policy", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, edge := range graph.Edges {
		if edge.From == read.Concept.Path && edge.To == "/mounted/refs/policy" && edge.Kind == "source_reference" {
			found = true
		}
		if edge.To == "/refs/policy" {
			t.Fatal("crossed source boundary")
		}
	}
	if !found {
		t.Fatal("missing source backlink", graph)
	}
	if _, err := ws.SetView(ctx, "decision-only", factile.ViewInput{Paths: []string{read.Concept.Path}}); err != nil {
		t.Fatal(err)
	}
	graph, err = ws.Graph(ctx, "/", factile.GraphOptions{Depth: 1, View: "decision-only"})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 || len(graph.Edges) != 0 {
		t.Fatal("view traversal widened visibility", graph)
	}
	pack, err := ws.Context(ctx, "/", "Decision", factile.ContextOptions{Depth: 1, View: "decision-only", MaxTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Concepts) != 1 {
		t.Fatal("context crossed view", pack)
	}
	for _, ref := range pack.Concepts[0].SourceReferences {
		if ref.Target != "" {
			t.Fatal("view leaked a derived target", ref)
		}
	}
	current, err := ws.Read(ctx, "/mounted/refs/policy", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := ws.Rename(ctx, current.Concept.Path, "/mounted/refs/revised", factile.RenameOptions{ExpectedRevision: current.Concept.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(renamed.Warnings) == 0 {
		t.Fatal("source backlink not reported on rename")
	}
	result, err := ws.Validate(ctx, "/mounted", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	missing := false
	for _, issue := range result.Issues {
		if issue.Code == "broken_source" {
			missing = true
		}
	}
	if !result.Valid || !missing {
		t.Fatal("missing source must be an advisory diagnostic", result)
	}
}

func TestSourcesNeverFollowSymlinks(t *testing.T) {
	root, ws := editingWorkspace(t)
	outside := filepath.Join(t.TempDir(), "private.md")
	os.WriteFile(outside, []byte("private"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "private.md")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "probe.md"), []byte("---\ntype: Note\nsources: [{resource: private.md}]\n---\n# Probe\n"), 0644)
	result, err := ws.Read(context.Background(), "/probe", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Concept.SourceReferences[0].Target != "" || result.Concept.MetadataDiagnostics[0].Code != "broken_source" {
		t.Fatal(result)
	}
}
