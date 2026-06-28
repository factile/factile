package factile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/internal/cli/render"
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
	for _, exposed := range []string{`"source"`, `"kind"`, `"writable"`, `"trust"`} {
		if !strings.Contains(actual, exposed) {
			t.Fatalf("curator output did not expose %s:\n%s", exposed, actual)
		}
	}
	if strings.Count(actual, `"source": "../../../bundles/shared-guides"`) != 2 {
		t.Fatalf("expected shared source linked at two paths:\n%s", actual)
	}
}

func TestWorkspaceKnowledgeBaseReaderOperationsUseAllBundles(t *testing.T) {
	workspaceDir := filepath.Join("..", "..", "testdata", "catalog-workspace")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	list, err := ws.List(ctx, "/engineering", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantFolders := []string{"/engineering/common", "/engineering/django", "/engineering/playbook"}
	if got := folderPaths(list.Folders); strings.Join(got, ",") != strings.Join(wantFolders, ",") {
		t.Fatalf("KB folders = %#v, want %#v", got, wantFolders)
	}

	search, err := ws.Search(ctx, "/engineering", "setup", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasPath(search, "/engineering/common/guides/setup") || !searchHasPath(search, "/engineering/playbook/guides/setup") {
		t.Fatalf("KB search should include both shared bundle paths: %#v", search.Results)
	}

	pack, err := ws.Context(ctx, "/engineering", "setup", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasPath(pack, "/engineering/common/guides/setup") || !contextHasPath(pack, "/engineering/playbook/guides/setup") {
		t.Fatalf("KB context should include both shared bundle paths: %#v", pack.Summaries)
	}

	graph, err := ws.Graph(ctx, "/engineering", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(graph, "/engineering/common/guides/setup") || !graphHasPath(graph, "/engineering/playbook/guides/setup") {
		t.Fatalf("KB graph should include both shared bundle paths: %#v", graph.Nodes)
	}
}

func TestWorkspaceLibraryViewManagement(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})

	list, err := ws.ListViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Views) != 0 {
		t.Fatalf("expected empty missing-library view list: %#v", list.Views)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".factile", "library.toml")); !os.IsNotExist(err) {
		t.Fatalf("library catalog should not exist before mutation, stat err=%v", err)
	}

	created, err := ws.SetView(ctx, "invoice-import", factile.ViewInput{
		Title:       "Invoice Import",
		Description: "Workflow and runbooks for invoice import.",
		Paths:       []string{"/project/docs/", "/support/runbooks/imports"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Action != "created" || created.View.ID != "invoice-import" || strings.Join(created.View.Paths, ",") != "/project/docs,/support/runbooks/imports" {
		t.Fatalf("unexpected created view: %#v", created)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".factile", "library.toml")); err != nil {
		t.Fatalf("expected SetView to initialize library catalog: %v", err)
	}

	if _, err := ws.SetView(ctx, "security-review", factile.ViewInput{
		Title: "Security Review",
		Paths: []string{"/standards/security"},
	}); err != nil {
		t.Fatal(err)
	}
	list, err = ws.ListViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Views) != 2 || list.Views[0].ID != "invoice-import" || list.Views[1].ID != "security-review" {
		t.Fatalf("expected deterministic view ordering: %#v", list.Views)
	}

	inspected, err := ws.InspectView(ctx, "invoice-import")
	if err != nil {
		t.Fatal(err)
	}
	if inspected.View.Title != "Invoice Import" || inspected.View.Description == "" {
		t.Fatalf("unexpected inspected view: %#v", inspected)
	}

	updated, err := ws.SetView(ctx, "invoice-import", factile.ViewInput{
		Title:  "Invoice Import Updated",
		Status: "active",
		Paths:  []string{"/project/docs/workflows/invoice-import"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Action != "updated" || updated.View.Title != "Invoice Import Updated" || updated.View.Status != "active" {
		t.Fatalf("unexpected updated view: %#v", updated)
	}

	deleted, err := ws.DeleteView(ctx, "security-review")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Deleted || deleted.ID != "security-review" {
		t.Fatalf("unexpected delete result: %#v", deleted)
	}
	if _, err := ws.InspectView(ctx, "security-review"); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrMountNotFound {
		t.Fatalf("expected missing deleted view to be not found, got %v", err)
	}
}

func TestWorkspaceLibraryViewValidation(t *testing.T) {
	ctx := context.Background()
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: t.TempDir()})

	if _, err := ws.SetView(ctx, "reader", factile.ViewInput{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected empty view paths to fail validation, got %v", err)
	}
	if _, err := ws.SetView(ctx, "reader", factile.ViewInput{Paths: []string{"relative"}}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("expected relative view path to fail invalid_path, got %v", err)
	}
	if _, err := ws.SetView(ctx, "reader", factile.ViewInput{Paths: []string{"/project/docs", "/project/docs/"}}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected duplicate view paths to fail validation, got %v", err)
	}
	if _, err := ws.InspectView(ctx, "missing"); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrMountNotFound {
		t.Fatalf("expected missing view to be not found, got %v", err)
	}
	if _, err := ws.DeleteView(ctx, "missing"); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrMountNotFound {
		t.Fatalf("expected deleting missing view to be not found, got %v", err)
	}
}

func TestWorkspaceListUsesLibraryView(t *testing.T) {
	workspaceDir := catalogWorkspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()
	if _, err := ws.SetView(ctx, "invoice", factile.ViewInput{Paths: []string{"/engineering/django/runbooks", "/legacy"}}); err != nil {
		t.Fatal(err)
	}

	root, err := ws.List(ctx, "/", factile.ListOptions{View: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if got := folderPaths(root.Folders); strings.Join(got, ",") != "/engineering,/legacy" || len(root.Documents) != 0 {
		t.Fatalf("root view list = folders %#v documents %#v", root.Folders, root.Documents)
	}

	kb, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if got := folderPaths(kb.Folders); strings.Join(got, ",") != "/engineering/django" || len(kb.Documents) != 0 {
		t.Fatalf("library-view list at KB path = folders %#v documents %#v", kb.Folders, kb.Documents)
	}

	runbooks, err := ws.List(ctx, "/engineering/django/runbooks", factile.ListOptions{View: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runbooks.Folders) != 0 || len(runbooks.Documents) != 1 || runbooks.Documents[0].Path != "/engineering/django/runbooks/ocr-failure" {
		t.Fatalf("folder view list = folders %#v documents %#v", runbooks.Folders, runbooks.Documents)
	}

	empty, err := ws.List(ctx, "/engineering/common", factile.ListOptions{View: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Folders) != 0 || len(empty.Documents) != 0 {
		t.Fatalf("expected empty intersection, got folders %#v documents %#v", empty.Folders, empty.Documents)
	}

	brief, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "invoice", Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.Cards) != 1 || brief.Cards[0].Path != "/engineering/django" || brief.Cards[0].Description == "" {
		t.Fatalf("brief view list should return one catalog-backed card: %#v", brief.Cards)
	}

	r, err := render.New(render.Options{ColorMode: render.ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := r.RenderList(&out, kb); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Folders:") || !strings.Contains(text, "/engineering/django") || strings.Contains(text, "/legacy") {
		t.Fatalf("unexpected rendered view list:\n%s", text)
	}
	out.Reset()
	if err := r.RenderList(&out, empty); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No entries.") {
		t.Fatalf("empty view list should render no entries:\n%s", out.String())
	}
}

func TestWorkspaceSearchContextGraphUseLibraryView(t *testing.T) {
	workspaceDir := catalogWorkspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()
	workflowPath := "/engineering/django/workflows/invoice-import"
	runbookPath := "/engineering/django/runbooks/ocr-failure"
	legacyPath := "/legacy/notes/legacy"
	setupPath := "/engineering/common/guides/setup"
	playbookPath := "/engineering/playbook/guides/setup"

	if _, err := ws.SetView(ctx, "mixed", factile.ViewInput{Paths: []string{
		workflowPath,
		runbookPath,
		"/legacy",
	}}); err != nil {
		t.Fatal(err)
	}

	legacySearch, err := ws.Search(ctx, "/", "legacy", factile.SearchOptions{View: "mixed"})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasPath(legacySearch, legacyPath) || searchHasPath(legacySearch, setupPath) || searchHasPath(legacySearch, playbookPath) {
		t.Fatalf("view search should include direct mount only inside view: %#v", legacySearch.Results)
	}

	setupSearch, err := ws.Search(ctx, "/", "setup", factile.SearchOptions{View: "mixed"})
	if err != nil {
		t.Fatal(err)
	}
	if searchHasPath(setupSearch, setupPath) || searchHasPath(setupSearch, playbookPath) {
		t.Fatalf("view search included out-of-view shared setup docs: %#v", setupSearch.Results)
	}

	pack, err := ws.Context(ctx, "/engineering", "posted", factile.ContextOptions{View: "mixed", MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasPath(pack, workflowPath) || !contextHasPath(pack, runbookPath) || contextHasPath(pack, setupPath) || contextHasPath(pack, legacyPath) {
		t.Fatalf("view context should stay inside the selected engineering scope: %#v", pack.Summaries)
	}

	graph, err := ws.Graph(ctx, "/engineering", factile.GraphOptions{View: "mixed", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(graph, workflowPath) || !graphHasPath(graph, runbookPath) || graphHasPath(graph, setupPath) || graphHasPath(graph, legacyPath) {
		t.Fatalf("view graph nodes should be selected-scope only: %#v", graph.Nodes)
	}
	if !graphHasEdge(graph, workflowPath, runbookPath) || !graphHasEdge(graph, runbookPath, workflowPath) {
		t.Fatalf("view graph should include in-view links only: %#v", graph.Edges)
	}

	if _, err := ws.SetView(ctx, "workflow-only", factile.ViewInput{Paths: []string{workflowPath}}); err != nil {
		t.Fatal(err)
	}
	narrowGraph, err := ws.Graph(ctx, "/engineering", factile.GraphOptions{View: "workflow-only", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(narrowGraph, workflowPath) || graphHasPath(narrowGraph, runbookPath) || len(narrowGraph.Edges) != 0 || len(narrowGraph.Issues) != 0 {
		t.Fatalf("single-concept view graph leaked out-of-view links: %#v", narrowGraph)
	}

	if _, err := ws.SetView(ctx, "overlap", factile.ViewInput{Paths: []string{"/engineering/django", "/engineering/django/runbooks"}}); err != nil {
		t.Fatal(err)
	}
	overlapSearch, err := ws.Search(ctx, "/", "ocr", factile.SearchOptions{View: "overlap"})
	if err != nil {
		t.Fatal(err)
	}
	if countSearchPath(overlapSearch, runbookPath) != 1 {
		t.Fatalf("overlapping view paths should deduplicate documents: %#v", overlapSearch.Results)
	}
}

func TestWorkspaceValidateUsesLibraryView(t *testing.T) {
	workspaceDir := catalogWorkspace(t)
	bundlesDir := filepath.Join(filepath.Dir(workspaceDir), "bundles")
	workflowPath := "/engineering/django/workflows/invoice-import"
	runbookPath := "/engineering/django/runbooks/ocr-failure"

	workflowFile := filepath.Join(bundlesDir, "product-docs", "workflows", "invoice-import.md")
	data, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n- [Missing Runbook](../runbooks/missing.md)\n")...)
	if err := os.WriteFile(workflowFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	badYAMLFile := filepath.Join(bundlesDir, "broken-docs", "bad-yaml.md")
	if err := os.WriteFile(badYAMLFile, []byte("---\ntype: [\n---\n# Bad YAML\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mountsFile := filepath.Join(workspaceDir, ".factile", "mounts.toml")
	mounts, err := os.ReadFile(mountsFile)
	if err != nil {
		t.Fatal(err)
	}
	mounts = append(mounts, []byte(`

[mounts."/broken"]
source = "../../bundles/broken-docs"
kind = "local"
writable = true
`)...)
	if err := os.WriteFile(mountsFile, mounts, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()
	if _, err := ws.SetView(ctx, "workflow-only", factile.ViewInput{Paths: []string{workflowPath}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SetView(ctx, "legacy-only", factile.ViewInput{Paths: []string{"/legacy"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SetView(ctx, "bad-docs", factile.ViewInput{Paths: []string{"/broken/bad-yaml"}}); err != nil {
		t.Fatal(err)
	}

	validated, err := ws.Validate(ctx, "/engineering", factile.ValidateOptions{View: "workflow-only"})
	if err != nil {
		t.Fatal(err)
	}
	if validated.Path != "/engineering" || !validated.Valid {
		t.Fatalf("view validation with only warnings should stay valid: %#v", validated)
	}
	if hasValidationIssue(validated.Issues, "warning", "broken_link", workflowPath, "../runbooks/ocr-failure.md") {
		t.Fatalf("existing outside-view link should not warn: %#v", validated.Issues)
	}
	if !hasValidationIssue(validated.Issues, "warning", "broken_link", workflowPath, "../runbooks/missing.md") {
		t.Fatalf("missing selected-doc link should warn: %#v", validated.Issues)
	}

	empty, err := ws.Validate(ctx, "/engineering", factile.ValidateOptions{View: "legacy-only"})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Path != "/engineering" || !empty.Valid || len(empty.Issues) != 0 {
		t.Fatalf("empty view intersection should validate cleanly: %#v", empty)
	}

	bad, err := ws.Validate(ctx, "/", factile.ValidateOptions{View: "bad-docs"})
	if err != nil {
		t.Fatal(err)
	}
	if bad.Valid || !hasValidationIssue(bad.Issues, "error", factile.ErrOKFParse, "/broken/bad-yaml", "") || hasValidationIssue(bad.Issues, "error", factile.ErrOKFParse, runbookPath, "") {
		t.Fatalf("selected malformed concepts should surface scoped errors: %#v", bad.Issues)
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

func countSearchPath(results factile.SearchResults, path string) int {
	count := 0
	for _, result := range results.Results {
		if result.Concept.Path == path {
			count++
		}
	}
	return count
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

func hasValidationIssue(issues []factile.ValidationIssue, severity string, code string, path string, target string) bool {
	for _, issue := range issues {
		if issue.Severity == severity && issue.Code == code && issue.Path == path && (target == "" || strings.Contains(issue.Message, target)) {
			return true
		}
	}
	return false
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
