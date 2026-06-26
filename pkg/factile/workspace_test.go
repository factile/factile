package factile_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestReaderCuratorPerspectiveGoldens(t *testing.T) {
	workspaceDir := filepath.Join("..", "..", "testdata", "catalog-workspace")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	readerCases := []struct {
		name   string
		path   string
		golden string
	}{
		{name: "root", path: "/", golden: "reader-root.json"},
		{name: "knowledge base", path: "/engineering", golden: "reader-kb.json"},
		{name: "bundle root", path: "/engineering/django", golden: "reader-bundle-root.json"},
		{name: "okf folder", path: "/engineering/django/runbooks", golden: "reader-okf-folder.json"},
		{name: "legacy folder", path: "/legacy/notes", golden: "reader-legacy-folder.json"},
	}
	for _, tc := range readerCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ws.List(ctx, tc.path, factile.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			actual := assertGoldenJSON(t, result, tc.golden)
			for _, hidden := range []string{`"source"`, `"kind"`, `"writable"`, `"mount_path"`, `"knowledge_base"`, `"bundle"`} {
				if strings.Contains(actual, hidden) {
					t.Fatalf("reader output leaked curator metadata %s:\n%s", hidden, actual)
				}
			}
		})
	}

	for _, scopePath := range []string{"/engineering/common/guides", "/engineering/playbook/guides"} {
		result, err := ws.List(ctx, scopePath, factile.ListOptions{})
		if err != nil {
			t.Fatalf("shared source scope %s: %v", scopePath, err)
		}
		if len(result.Documents) != 1 || !strings.HasSuffix(result.Documents[0].Path, "/guides/setup") {
			t.Fatalf("unexpected shared source documents for %s: %#v", scopePath, result.Documents)
		}
	}

	inspected, err := ws.InspectKnowledgeBase(ctx, "/engineering")
	if err != nil {
		t.Fatal(err)
	}
	actual := assertGoldenJSON(t, inspected, "curator-kb-inspect.json")
	for _, exposed := range []string{`"source"`, `"kind"`, `"writable"`, `"trust"`, `"views"`} {
		if !strings.Contains(actual, exposed) {
			t.Fatalf("curator output did not expose %s:\n%s", exposed, actual)
		}
	}
	if strings.Count(actual, `"source": "../../../bundles/shared-guides"`) != 2 {
		t.Fatalf("expected shared source linked at two paths:\n%s", actual)
	}
}

func TestWorkspaceViewSelectionScopesReaderOperations(t *testing.T) {
	workspaceDir := filepath.Join("..", "..", "testdata", "catalog-workspace")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	defaultList, err := ws.List(ctx, "/engineering", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultList.Folders) != 3 {
		t.Fatalf("default view should include all bundles: %#v", defaultList.Folders)
	}

	readerList, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	wantFolders := []string{"/engineering/django", "/engineering/common"}
	if got := folderPaths(readerList.Folders); strings.Join(got, ",") != strings.Join(wantFolders, ",") {
		t.Fatalf("reader view folders = %#v, want %#v", got, wantFolders)
	}

	defaultSearch, err := ws.Search(ctx, "/engineering", "setup", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasPath(defaultSearch, "/engineering/common/guides/setup") || !searchHasPath(defaultSearch, "/engineering/playbook/guides/setup") {
		t.Fatalf("default search should include both shared bundle paths: %#v", defaultSearch.Results)
	}
	readerSearch, err := ws.Search(ctx, "/engineering", "setup", factile.SearchOptions{View: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasPath(readerSearch, "/engineering/common/guides/setup") || searchHasPath(readerSearch, "/engineering/playbook/guides/setup") {
		t.Fatalf("reader search should include common and exclude playbook: %#v", readerSearch.Results)
	}

	pack, err := ws.Context(ctx, "/engineering", "setup", factile.ContextOptions{MaxTokens: 4000, Depth: 1, View: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	if contextHasPath(pack, "/engineering/playbook/guides/setup") {
		t.Fatalf("reader context leaked excluded playbook bundle: %#v", pack.Summaries)
	}

	graph, err := ws.Graph(ctx, "/engineering", factile.GraphOptions{Depth: 1, View: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	if graphHasPath(graph, "/engineering/playbook/guides/setup") {
		t.Fatalf("reader graph leaked excluded playbook bundle: %#v", graph.Nodes)
	}

	defaultView, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultView.Folders) != 3 {
		t.Fatalf("implicit default view should include all bundles: %#v", defaultView.Folders)
	}

	if _, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "missing"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected validation_failed for unknown view, got %v", err)
	}
	if _, err := ws.Search(ctx, "/legacy", "setup", factile.SearchOptions{View: "reader"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected validation_failed for view outside KB, got %v", err)
	}
}

func TestWorkspaceSetReplaceAndDeleteKnowledgeBaseView(t *testing.T) {
	workspaceDir := catalogWorkspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	created, err := ws.SetKnowledgeBaseView(ctx, "/engineering", "reviewer", factile.ViewInput{
		Title:     "Reviewer",
		Bundles:   []string{"engineering-django", "/engineering/common"},
		WhenToUse: "Use for review work.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Action != "created" || strings.Join(created.View.Bundles, ",") != "engineering-django,engineering-common" {
		t.Fatalf("unexpected created view: %#v", created)
	}

	scoped, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if got := folderPaths(scoped.Folders); strings.Join(got, ",") != "/engineering/django,/engineering/common" {
		t.Fatalf("reviewer view folders = %#v", got)
	}

	updated, err := ws.SetKnowledgeBaseView(ctx, "/engineering", "reviewer", factile.ViewInput{
		Title:   "Reviewer",
		Bundles: []string{"engineering-playbook"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Action != "updated" || strings.Join(updated.View.Bundles, ",") != "engineering-playbook" {
		t.Fatalf("unexpected updated view: %#v", updated)
	}
	inspected, err := ws.InspectKnowledgeBase(ctx, "/engineering")
	if err != nil {
		t.Fatal(err)
	}
	if countViews(inspected.KnowledgeBase.Views, "reviewer") != 1 {
		t.Fatalf("set should replace the existing view, got %#v", inspected.KnowledgeBase.Views)
	}
	reviewer, ok := findView(inspected.KnowledgeBase.Views, "reviewer")
	if !ok || strings.Join(reviewer.Bundles, ",") != "engineering-playbook" {
		t.Fatalf("inspect did not show updated reviewer view: %#v", inspected.KnowledgeBase.Views)
	}

	defaultList, err := ws.List(ctx, "/engineering", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultList.Folders) != 3 {
		t.Fatalf("view mutation should not change default reader paths: %#v", defaultList.Folders)
	}
	if _, err := ws.List(ctx, "/engineering/reviewer", factile.ListOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrMountNotFound {
		t.Fatalf("view id should not become a reader path, got %v", err)
	}

	deleted, err := ws.DeleteKnowledgeBaseView(ctx, "/engineering", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Deleted || deleted.ViewID != "reviewer" {
		t.Fatalf("unexpected delete result: %#v", deleted)
	}
	inspected, err = ws.InspectKnowledgeBase(ctx, "/engineering")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findView(inspected.KnowledgeBase.Views, "reviewer"); ok {
		t.Fatalf("delete should remove reviewer view: %#v", inspected.KnowledgeBase.Views)
	}
	if _, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "reviewer"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected validation_failed for deleted view, got %v", err)
	}
}

func TestWorkspaceKnowledgeBaseViewValidation(t *testing.T) {
	workspaceDir := catalogWorkspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	cases := []struct {
		name string
		run  func() error
	}{
		{
			name: "invalid view id",
			run: func() error {
				_, err := ws.SetKnowledgeBaseView(ctx, "/engineering", "bad/view", factile.ViewInput{Bundles: []string{"engineering-django"}})
				return err
			},
		},
		{
			name: "missing bundle reference",
			run: func() error {
				_, err := ws.SetKnowledgeBaseView(ctx, "/engineering", "missing-bundle", factile.ViewInput{Bundles: []string{"missing"}})
				return err
			},
		},
		{
			name: "duplicate bundle reference",
			run: func() error {
				_, err := ws.SetKnowledgeBaseView(ctx, "/engineering", "duplicate-bundle", factile.ViewInput{Bundles: []string{"engineering-django", "/engineering/django"}})
				return err
			},
		},
		{
			name: "delete missing view",
			run: func() error {
				_, err := ws.DeleteKnowledgeBaseView(ctx, "/engineering", "missing")
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
				t.Fatalf("expected validation_failed, got %v", err)
			}
		})
	}
}

func TestBriefListAndStatCards(t *testing.T) {
	workspaceDir := filepath.Join("..", "..", "testdata", "catalog-workspace")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	root, err := ws.List(ctx, "/", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Cards) != 2 || len(root.Folders) != 0 || len(root.Documents) != 0 {
		t.Fatalf("unexpected root brief list: %#v", root)
	}
	if root.Cards[0].Path != "/engineering" || root.Cards[0].Title != "Engineering Knowledge Base" {
		t.Fatalf("expected catalog-backed KB card: %#v", root.Cards[0])
	}
	if root.Cards[1].Path != "/legacy" || root.Cards[1].Writable == nil || !*root.Cards[1].Writable {
		t.Fatalf("expected writable legacy card: %#v", root.Cards[1])
	}

	kb, err := ws.List(ctx, "/engineering", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Cards) != 3 {
		t.Fatalf("unexpected KB cards: %#v", kb.Cards)
	}
	django := kb.Cards[1]
	if django.Path != "/engineering/django" || django.WhenToUse == "" || django.Writable == nil || !*django.Writable {
		t.Fatalf("expected writable Django bundle card with guidance: %#v", django)
	}
	common := kb.Cards[0]
	if common.Path != "/engineering/common" || common.Writable == nil || *common.Writable {
		t.Fatalf("expected read-only common bundle card: %#v", common)
	}

	rootStat, err := ws.Stat(ctx, "/", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rootStat.Card.Title != "Local Library" || rootStat.Card.Description == "" {
		t.Fatalf("unexpected root stat: %#v", rootStat.Card)
	}
	kbStat, err := ws.Stat(ctx, "/engineering", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if kbStat.Card.Title != "Engineering Knowledge Base" || kbStat.Card.Writable != nil {
		t.Fatalf("unexpected KB stat: %#v", kbStat.Card)
	}
	folderStat, err := ws.Stat(ctx, "/engineering/django/runbooks", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if folderStat.Card.Title != "Runbooks" || folderStat.Card.Writable == nil || !*folderStat.Card.Writable {
		t.Fatalf("unexpected folder stat: %#v", folderStat.Card)
	}
	docStat, err := ws.Stat(ctx, "/engineering/django/runbooks/ocr-failure", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if docStat.Card.Title != "OCR Failure Runbook" || docStat.Card.Revision == "" || len(docStat.Card.Tags) != 3 || docStat.Card.Writable == nil || !*docStat.Card.Writable {
		t.Fatalf("unexpected document stat: %#v", docStat.Card)
	}
}

func folderPaths(folders []factile.FolderSummary) []string {
	paths := make([]string, 0, len(folders))
	for _, folder := range folders {
		paths = append(paths, folder.Path)
	}
	return paths
}

func searchHasPath(results factile.SearchResults, path string) bool {
	for _, result := range results.Results {
		if result.Concept.Path == path {
			return true
		}
	}
	return false
}

func contextHasPath(pack factile.ContextPack, path string) bool {
	for _, summary := range pack.Summaries {
		if summary.Path == path {
			return true
		}
	}
	return false
}

func graphHasPath(graph factile.GraphResult, path string) bool {
	for _, node := range graph.Nodes {
		if node.Concept.Path == path {
			return true
		}
	}
	return false
}

func graphHasEdge(graph factile.GraphResult, from string, to string) bool {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}

func findView(views []factile.View, id string) (factile.View, bool) {
	for _, view := range views {
		if view.ID == id {
			return view, true
		}
	}
	return factile.View{}, false
}

func countViews(views []factile.View, id string) int {
	count := 0
	for _, view := range views {
		if view.ID == id {
			count++
		}
	}
	return count
}

func TestWorkspaceReadSearchContextGraphAndValidate(t *testing.T) {
	ws, _ := testWorkspace(t)
	ctx := context.Background()

	root, err := ws.List(ctx, "/", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 2 || root.Folders[0].Path != "/broken-docs" || root.Folders[1].Path != "/product-docs" {
		t.Fatalf("unexpected root folders: %#v", root.Folders)
	}
	list, err := ws.List(ctx, "/product-docs", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Folders) != 2 || list.Folders[0].Path != "/product-docs/runbooks" || list.Folders[1].Path != "/product-docs/workflows" {
		t.Fatalf("unexpected bundle folders: %#v", list.Folders)
	}
	runbooks, err := ws.List(ctx, "/product-docs/runbooks", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runbooks.Documents) != 1 || runbooks.Documents[0].Path != "/product-docs/runbooks/ocr-failure" {
		t.Fatalf("unexpected runbook documents: %#v", runbooks.Documents)
	}
	concept, err := ws.Read(ctx, "/product-docs/workflows/invoice-import.md", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if concept.Concept.Revision == "" || concept.Concept.Path != "/product-docs/workflows/invoice-import" {
		t.Fatalf("unexpected concept: %#v", concept.Concept)
	}
	search, err := ws.Search(ctx, "/product-docs", "invoice", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Results) == 0 || search.Results[0].Concept.Path != "/product-docs/workflows/invoice-import" {
		t.Fatalf("unexpected search results: %#v", search.Results)
	}
	resourceSearch, err := ws.Search(ctx, "/product-docs", "factile:test/product-docs/workflows/invoice-import", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resourceSearch.Results) == 0 || resourceSearch.Results[0].Concept.Path != "/product-docs/workflows/invoice-import" {
		t.Fatalf("unexpected resource search results: %#v", resourceSearch.Results)
	}
	pack, err := ws.Context(ctx, "/product-docs", "invoice import workflow", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Concepts) < 2 {
		t.Fatalf("expected search hit plus linked runbook, got %d", len(pack.Concepts))
	}
	graph, err := ws.Graph(ctx, "/product-docs/workflows/invoice-import", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected outgoing link and backlink, got %#v", graph.Edges)
	}
	good, err := ws.Validate(ctx, "/product-docs", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !good.Valid {
		t.Fatalf("expected valid product docs: %#v", good.Issues)
	}
	bad, err := ws.Validate(ctx, "/broken-docs", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if bad.Valid {
		t.Fatal("expected broken docs to be invalid")
	}
}

func TestWorkspaceListVirtualFoldersFromNestedMounts(t *testing.T) {
	tmp := t.TempDir()
	docs := filepath.Join(tmp, "docs")
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), docs)
	mountFile := filepath.Join(tmp, "mounts.toml")
	data := `[mounts."/project/docs"]
source = "` + docs + `"
kind = "local"
writable = true
`
	if err := os.WriteFile(mountFile, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	ctx := context.Background()

	root, err := ws.List(ctx, "/", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 1 || root.Folders[0].Path != "/project" {
		t.Fatalf("unexpected root folders: %#v", root.Folders)
	}
	project, err := ws.List(ctx, "/project", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Folders) != 1 || project.Folders[0].Path != "/project/docs" {
		t.Fatalf("unexpected project folders: %#v", project.Folders)
	}
	docsList, err := ws.List(ctx, "/project/docs/runbooks", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(docsList.Documents) != 1 || docsList.Documents[0].Path != "/project/docs/runbooks/ocr-failure" {
		t.Fatalf("unexpected nested mount documents: %#v", docsList.Documents)
	}

	for _, scopePath := range []string{"/", "/project", "/project/docs"} {
		search, err := ws.Search(ctx, scopePath, "invoice", factile.SearchOptions{})
		if err != nil {
			t.Fatalf("search %s: %v", scopePath, err)
		}
		if len(search.Results) == 0 || search.Results[0].Concept.Path != "/project/docs/workflows/invoice-import" {
			t.Fatalf("unexpected search results for %s: %#v", scopePath, search.Results)
		}
		pack, err := ws.Context(ctx, scopePath, "invoice import workflow", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
		if err != nil {
			t.Fatalf("context %s: %v", scopePath, err)
		}
		if len(pack.Concepts) < 2 {
			t.Fatalf("expected search hit plus linked runbook for %s, got %d", scopePath, len(pack.Concepts))
		}
		graph, err := ws.Graph(ctx, scopePath, factile.GraphOptions{Depth: 1})
		if err != nil {
			t.Fatalf("graph %s: %v", scopePath, err)
		}
		if len(graph.Edges) != 2 {
			t.Fatalf("expected scoped graph edges for %s, got %#v", scopePath, graph.Edges)
		}
		validated, err := ws.Validate(ctx, scopePath, factile.ValidateOptions{})
		if err != nil {
			t.Fatalf("validate %s: %v", scopePath, err)
		}
		if !validated.Valid {
			t.Fatalf("expected valid scope %s: %#v", scopePath, validated.Issues)
		}
	}

	runbookSearch, err := ws.Search(ctx, "/project/docs/runbooks", "ocr", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runbookSearch.Results) != 1 || runbookSearch.Results[0].Concept.Path != "/project/docs/runbooks/ocr-failure" {
		t.Fatalf("unexpected runbook search results: %#v", runbookSearch.Results)
	}
	runbookContext, err := ws.Context(ctx, "/project/docs/runbooks", "ocr failure", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(runbookContext.Concepts) == 0 {
		t.Fatal("expected runbook context")
	}
	runbookGraph, err := ws.Graph(ctx, "/project/docs/runbooks", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(runbookGraph.Nodes) != 1 {
		t.Fatalf("expected one runbook graph node, got %#v", runbookGraph.Nodes)
	}
	runbookValidation, err := ws.Validate(ctx, "/project/docs/runbooks", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !runbookValidation.Valid {
		t.Fatalf("expected valid runbook scope: %#v", runbookValidation.Issues)
	}
}

func TestWorkspaceContextAndGraphDepthSemantics(t *testing.T) {
	ws, _ := testWorkspace(t)
	ctx := context.Background()
	invoicePath := "/product-docs/workflows/invoice-import"
	runbookPath := "/product-docs/runbooks/ocr-failure"

	depthZeroContext, err := ws.Context(ctx, "/product-docs", "processed", factile.ContextOptions{MaxTokens: 4000, Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasPath(depthZeroContext, invoicePath) {
		t.Fatalf("depth 0 context missing direct search hit: %#v", depthZeroContext.Summaries)
	}
	if contextHasPath(depthZeroContext, runbookPath) {
		t.Fatalf("depth 0 context included linked runbook: %#v", depthZeroContext.Summaries)
	}

	depthOneContext, err := ws.Context(ctx, "/product-docs", "processed", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasPath(depthOneContext, runbookPath) {
		t.Fatalf("depth 1 context missing linked runbook: %#v", depthOneContext.Summaries)
	}

	if _, err := ws.Context(ctx, "/product-docs", "processed", factile.ContextOptions{MaxTokens: 4000, Depth: 2}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("expected depth 2 context to fail with invalid_path, got %v", err)
	}

	depthZeroGraph, err := ws.Graph(ctx, invoicePath, factile.GraphOptions{Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(depthZeroGraph, invoicePath) || graphHasPath(depthZeroGraph, runbookPath) || len(depthZeroGraph.Edges) != 0 {
		t.Fatalf("unexpected depth 0 graph: %#v", depthZeroGraph)
	}

	depthOneGraph, err := ws.Graph(ctx, invoicePath, factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(depthOneGraph, runbookPath) || !graphHasEdge(depthOneGraph, invoicePath, runbookPath) {
		t.Fatalf("unexpected depth 1 graph: %#v", depthOneGraph)
	}

	if _, err := ws.Graph(ctx, invoicePath, factile.GraphOptions{Depth: 2}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("expected depth 2 graph to fail with invalid_path, got %v", err)
	}
}

func TestWorkspaceWritePatchRenameDeleteDeprecate(t *testing.T) {
	ws, bundleRoot := testWorkspace(t)
	ctx := context.Background()

	created, err := ws.Create(ctx, "/product-docs/workflows/payment-import", factile.CreateConceptInput{
		Type:     "Workflow",
		Title:    "Payment Import Workflow",
		Markdown: "# Payment Import Workflow\n\nPayments are loaded.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Create(ctx, "/product-docs/workflows/payment-import", factile.CreateConceptInput{Type: "Workflow", Markdown: "# Duplicate\n"}); err == nil {
		t.Fatal("expected create existing concept to fail")
	}
	if _, err := ws.Write(ctx, created.Concept.Path, factile.WriteConceptInput{Markdown: "# Missing revision\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionRequired {
		t.Fatalf("expected revision_required, got %v", err)
	}
	if _, err := ws.Write(ctx, created.Concept.Path, factile.WriteConceptInput{ExpectedRevision: "sha256:wrong", Markdown: "# Wrong\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionMismatch {
		t.Fatalf("expected revision_mismatch, got %v", err)
	}
	unchanged, err := os.ReadFile(filepath.Join(bundleRoot, "workflows", "payment-import.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unchanged), "# Wrong") {
		t.Fatal("wrong-revision write changed the file")
	}
	written, err := ws.Write(ctx, created.Concept.Path, factile.WriteConceptInput{
		ExpectedRevision: created.Concept.Revision,
		Markdown:         "# Payment Import Workflow\n\nPayments are reconciled.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	patched, err := ws.Patch(ctx, written.Concept.Path, factile.PatchConceptInput{
		ExpectedRevision: written.Concept.Revision,
		Set:              map[string]any{"status": "draft"},
		ReplaceSections:  map[string]string{"Payment Import Workflow": "Payments are settled."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Concept.Frontmatter["status"] != "draft" || !strings.Contains(patched.Concept.Markdown, "Payments are settled.") {
		t.Fatalf("patch did not apply: %#v", patched.Concept)
	}
	deprecated, err := ws.Deprecate(ctx, patched.Concept.Path, factile.DeprecateOptions{ExpectedRevision: patched.Concept.Revision, Reason: "superseded"})
	if err != nil {
		t.Fatal(err)
	}
	if deprecated.Concept.Frontmatter["deprecated"] != true {
		t.Fatalf("deprecate did not set frontmatter: %#v", deprecated.Concept.Frontmatter)
	}
	renamed, err := ws.Rename(ctx, deprecated.Concept.Path, "/product-docs/workflows/payment-import-v2", factile.RenameOptions{ExpectedRevision: deprecated.Concept.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Concept.Path != "/product-docs/workflows/payment-import-v2" {
		t.Fatalf("unexpected renamed path: %s", renamed.Concept.Path)
	}
	existing, err := ws.Create(ctx, "/product-docs/workflows/existing-destination", factile.CreateConceptInput{
		Type:     "Workflow",
		Title:    "Existing",
		Markdown: "# Existing\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Rename(ctx, renamed.Concept.Path, existing.Concept.Path, factile.RenameOptions{ExpectedRevision: renamed.Concept.Revision}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrConceptAlreadyExist {
		t.Fatalf("expected concept_already_exists, got %v", err)
	}
	deleted, err := ws.Delete(ctx, renamed.Concept.Path, factile.DeleteOptions{ExpectedRevision: renamed.Concept.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Deleted {
		t.Fatal("delete result was false")
	}
}

func TestReadOnlyAndUnsupportedSource(t *testing.T) {
	ws, _ := testWorkspace(t)
	ctx := context.Background()
	read, err := ws.Read(ctx, "/product-docs/workflows/invoice-import", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	readOnly := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFileForWorkspace(t, false), ReadOnly: true})
	if _, err := readOnly.Write(ctx, read.Concept.Path, factile.WriteConceptInput{ExpectedRevision: read.Concept.Revision, Markdown: "# No\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("expected source_read_only, got %v", err)
	}
	remoteMount := filepath.Join(t.TempDir(), "remote.toml")
	if err := os.WriteFile(remoteMount, []byte(`[mounts."/remote"]
source = "factile://bundle"
kind = "remote"
writable = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: remoteMount})
	if _, err := remote.List(ctx, "/remote", factile.ListOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected unsupported_source, got %v", err)
	}
	if _, err := remote.Search(ctx, "/", "anything", factile.SearchOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected root search unsupported_source, got %v", err)
	}
	if _, err := remote.Validate(ctx, "/", factile.ValidateOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected root validate unsupported_source, got %v", err)
	}
}

func TestWorkspaceMountAndUnmountRegistry(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mounts.toml")
	source := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: registry})
	mounted, err := ws.Mount(ctx, source, "/docs", factile.MountOptions{Writable: true, Kind: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if mounted.Mount.MountPath != "/docs" {
		t.Fatalf("unexpected mount: %#v", mounted.Mount)
	}
	list, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Mounts) != 1 || list.Mounts[0].MountPath != "/docs" {
		t.Fatalf("unexpected mounts: %#v", list.Mounts)
	}
	unmounted, err := ws.Unmount(ctx, "/docs", factile.UnmountOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !unmounted.Removed {
		t.Fatal("unmount did not remove mount")
	}
}

func TestWorkspaceMountDoesNotOverwriteMalformedRegistry(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mounts.toml")
	original := []byte("not toml\n")
	if err := os.WriteFile(registry, original, 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: registry})
	if _, err := ws.Mount(ctx, source, "/docs", factile.MountOptions{Writable: true, Kind: "local"}); err == nil {
		t.Fatal("expected malformed registry error")
	}
	after, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("malformed registry was overwritten:\n%s", string(after))
	}
}

func TestWorkspaceValidateReportsParseErrorsAsIssues(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "bad.md"), []byte("---\ntype Workflow\n---\n\n# Bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(tmp, "mounts.toml")
	if err := os.WriteFile(registry, []byte(`[mounts."/bad"]
source = "`+bundle+`"
kind = "local"
writable = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: registry})
	result, err := ws.Validate(ctx, "/bad", factile.ValidateOptions{})
	if err != nil {
		t.Fatalf("validate returned top-level error: %v", err)
	}
	if result.Valid || len(result.Issues) != 1 || result.Issues[0].Code != factile.ErrOKFParse {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestWorkspaceCatalogMissingSourcePath(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".factile", "knowledge-bases"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".factile", "library.toml"), []byte(`id = "local"

[[knowledge_bases]]
id = "project"
path = "/project"
catalog = "knowledge-bases/project.toml"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".factile", "knowledge-bases", "project.toml"), []byte(`id = "project"
path = "/project"

[[bundles]]
id = "missing"
path = "/project/missing"
source = "missing-source"
kind = "local"
writable = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})
	if _, err := ws.List(ctx, "/project/missing", factile.ListOptions{}); err == nil {
		t.Fatal("expected missing source path error")
	}
}

func testWorkspace(t *testing.T) (*factile.LocalWorkspace, string) {
	t.Helper()
	mountFile := mountFileForWorkspace(t, true)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	return ws, filepath.Join(filepath.Dir(mountFile), "product-docs")
}

func catalogWorkspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	copyDir(t, filepath.Join("..", "..", "testdata", "catalog-workspace"), workspace)
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles"), filepath.Join(tmp, "bundles"))
	return workspace
}

func mountFileForWorkspace(t *testing.T, writable bool) string {
	t.Helper()
	tmp := t.TempDir()
	product := filepath.Join(tmp, "product-docs")
	broken := filepath.Join(tmp, "broken-docs")
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "broken-docs"), broken)
	mountFile := filepath.Join(tmp, "mounts.toml")
	data := `[mounts."/product-docs"]
source = "` + product + `"
kind = "local"
writable = ` + boolString(writable) + `

[mounts."/broken-docs"]
source = "` + broken + `"
kind = "local"
writable = true
`
	if err := os.WriteFile(mountFile, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return mountFile
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyDir(t, from, to)
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func assertGoldenJSON(t *testing.T, value any, golden string) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual := string(data) + "\n"
	expected, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", golden))
	if err != nil {
		t.Fatal(err)
	}
	if actual != string(expected) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", golden, string(expected), actual)
	}
	return actual
}
