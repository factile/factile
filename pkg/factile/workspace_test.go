package factile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/factile/factile/internal/cli/render"
	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/gitsource"
	"github.com/factile/factile/pkg/vfs"
)

func TestReaderCuratorPerspectiveGoldens(t *testing.T) {
	workspaceDir := v2Workspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	readerCases := []struct {
		name   string
		path   string
		golden string
	}{
		{name: "root", path: "/", golden: "reader-root.json"},
		{name: "mounted group", path: "/engineering", golden: "reader-mounted-group.json"},
		{name: "mounted source root", path: "/engineering/django", golden: "reader-bundle-root.json"},
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

	mounts, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sharedCount int
	for _, mount := range mounts.Mounts {
		if mount.Source == "../../bundles/shared-guides" {
			sharedCount++
		}
	}
	if sharedCount != 2 {
		t.Fatalf("expected shared source mounted at two paths: %#v", mounts.Mounts)
	}
}

func TestWorkspaceMountedSourceReaderOperationsUseAllSources(t *testing.T) {
	workspaceDir := v2Workspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	list, err := ws.List(ctx, "/engineering", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantFolders := []string{"/engineering/common", "/engineering/django", "/engineering/playbook"}
	if got := folderPaths(list.Folders); strings.Join(got, ",") != strings.Join(wantFolders, ",") {
		t.Fatalf("mounted source folders = %#v, want %#v", got, wantFolders)
	}

	search, err := ws.Search(ctx, "/engineering", "setup", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasPath(search, "/engineering/common/guides/setup") || !searchHasPath(search, "/engineering/playbook/guides/setup") {
		t.Fatalf("search should include both shared mounted paths: %#v", search.Results)
	}

	pack, err := ws.Context(ctx, "/engineering", "setup", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasPath(pack, "/engineering/common/guides/setup") || !contextHasPath(pack, "/engineering/playbook/guides/setup") {
		t.Fatalf("context should include both shared mounted paths: %#v", pack.Summaries)
	}

	graph, err := ws.Graph(ctx, "/engineering", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(graph, "/engineering/common/guides/setup") || !graphHasPath(graph, "/engineering/playbook/guides/setup") {
		t.Fatalf("graph should include both shared mounted paths: %#v", graph.Nodes)
	}
}

func TestWorkspaceViewManagement(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	writeRootConfig(t, tmp)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})

	list, err := ws.ListViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Views) != 0 {
		t.Fatalf("expected empty missing views file list: %#v", list.Views)
	}
	if list.Views == nil {
		t.Fatal("expected missing views file to return an empty array, not null")
	}
	if _, err := os.Stat(filepath.Join(tmp, "factile.views.toml")); !os.IsNotExist(err) {
		t.Fatalf("views file should not exist before mutation, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".factile")); !os.IsNotExist(err) {
		t.Fatalf("view reads should not create state, stat err=%v", err)
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
	if _, err := os.Stat(filepath.Join(tmp, "factile.views.toml")); err != nil {
		t.Fatalf("expected SetView to initialize views file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".factile", "library.toml")); !os.IsNotExist(err) {
		t.Fatalf("SetView should not initialize old library file, stat err=%v", err)
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
	summary, err := ws.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Views) != 2 || summary.Views[0].ID != "invoice-import" {
		t.Fatalf("summary should list views from views.toml: %#v", summary.Views)
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

func TestWorkspaceRejectsInternalMetadataPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeRootConfig(t, root)
	cacheFile := filepath.Join(root, ".factile", "cache", "git", "key", "snapshots", "revision", "guides", "setup.md")
	mustWriteWorkspace(t, cacheFile, "---\ntype: Guide\n---\n\n# Original\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	path := "/.factile/cache/git/key/snapshots/revision/guides/setup"

	if _, err := ws.Read(ctx, path, factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("internal read error = %v, want %s", err, factile.ErrInvalidPath)
	}
	if _, err := ws.Search(ctx, "/.factile/cache", "setup", factile.SearchOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("internal search error = %v, want %s", err, factile.ErrInvalidPath)
	}
	if _, err := ws.Write(ctx, path, factile.WriteConceptInput{ExpectedRevision: "ignored", Markdown: "# Changed\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("internal write error = %v, want %s", err, factile.ErrInvalidPath)
	}
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Original") {
		t.Fatalf("internal cache content was mutated: %s", data)
	}
}

func TestWorkspaceViewValidation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	writeRootConfig(t, tmp)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})

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

func TestWorkspaceViewsFileValidation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	writeRootConfig(t, tmp)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})
	viewsFile := filepath.Join(tmp, "factile.views.toml")

	if err := os.WriteFile(viewsFile, []byte(`[[views]]
id = "dup"
paths = ["/a"]

[[views]]
id = "dup"
paths = ["/b"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.ListViews(ctx); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected duplicate view ids to fail validation, got %v", err)
	}

	if err := os.WriteFile(viewsFile, []byte(`[[views]]
id = "dup-path"
paths = ["/a", "/a/"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.ListViews(ctx); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("expected duplicate view paths to fail validation, got %v", err)
	}
}

func TestWorkspaceListUsesView(t *testing.T) {
	workspaceDir := v2Workspace(t)
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

	group, err := ws.List(ctx, "/engineering", factile.ListOptions{View: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if got := folderPaths(group.Folders); strings.Join(got, ",") != "/engineering/django" || len(group.Documents) != 0 {
		t.Fatalf("view list at mounted group path = folders %#v documents %#v", group.Folders, group.Documents)
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
		t.Fatalf("brief view list should return one mount-backed card: %#v", brief.Cards)
	}

	r, err := render.New(render.Options{ColorMode: render.ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := r.RenderList(&out, group); err != nil {
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

func TestWorkspaceSearchContextGraphUseView(t *testing.T) {
	workspaceDir := v2Workspace(t)
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

func TestWorkspaceValidateUsesView(t *testing.T) {
	workspaceDir := v2Workspace(t)
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

	mustWriteWorkspace(t, filepath.Join(workspaceDir, "broken.mount.toml"), `source = "../bundles/broken-docs"
writable = true
`)

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
	if _, err := ws.SetView(ctx, "django", factile.ViewInput{Paths: []string{"/engineering/django"}}); err != nil {
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
	if hasValidationIssue(validated.Issues, "warning", "broken_link", workflowPath, "../runbooks/missing.md") {
		t.Fatalf("links outside the selected view scope should not be judged: %#v", validated.Issues)
	}

	django, err := ws.Validate(ctx, "/", factile.ValidateOptions{View: "django"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasValidationIssue(django.Issues, "warning", "broken_link", workflowPath, "../runbooks/missing.md") {
		t.Fatalf("missing links inside the selected view scope should warn: %#v", django.Issues)
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

func TestWorkspaceValidateReportsInvalidGitMountsAsScopedIssues(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	good := filepath.Join(t.TempDir(), "good")
	mustWriteWorkspace(t, filepath.Join(good, "factile.toml"), "version = 2\n\n[bundle]\nname = \"good\"\n")
	writeOKFFile(t, filepath.Join(good, "overview.md"), "Guide", "Good", "# Good\n")
	mustWriteWorkspace(t, filepath.Join(root, "good.mount.toml"), "source = "+strconv.Quote(good)+"\nwritable = false\n")
	invalidMounts := []struct {
		mountPath  string
		descriptor string
		revision   string
	}{
		{mountPath: "/invalid", descriptor: `source = "https://example.test/coding.git"
writable = false
ref = ""
`},
		{mountPath: "/sha256-lower", revision: strings.Repeat("a", 64), descriptor: `source = "https://example.test/coding.git"
writable = false
revision = "` + strings.Repeat("a", 64) + `"
`},
		{mountPath: "/sha256-upper", revision: strings.Repeat("A", 64), descriptor: `source = "https://example.test/coding.git"
writable = false
revision = "` + strings.Repeat("A", 64) + `"
`},
	}
	for _, invalid := range invalidMounts {
		filename := strings.TrimPrefix(invalid.mountPath, "/") + ".mount.toml"
		mustWriteWorkspace(t, filepath.Join(root, filename), invalid.descriptor)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	listed, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]*factile.SourceStatus{}
	for _, mount := range listed.Mounts {
		statuses[mount.MountPath] = mount.SourceStatus
	}
	for _, invalid := range invalidMounts {
		status := statuses[invalid.mountPath]
		if status == nil || status.LastErrorCode != factile.ErrValidationFailed || status.IntentRevision != invalid.revision {
			t.Fatalf("invalid Git mount %s status = %#v", invalid.mountPath, status)
		}
	}

	for _, invalid := range invalidMounts {
		path := invalid.mountPath
		result, err := ws.Validate(ctx, path, factile.ValidateOptions{})
		if err != nil {
			t.Fatalf("validate %s returned top-level error: %v", path, err)
		}
		if result.Valid || !hasIssue(result.Issues, path, factile.ErrValidationFailed) {
			t.Fatalf("validate %s did not report the invalid Git mount: %#v", path, result)
		}
	}
	rootResult, err := ws.Validate(ctx, "/", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range invalidMounts {
		if rootResult.Valid || !hasIssue(rootResult.Issues, invalid.mountPath, factile.ErrValidationFailed) {
			t.Fatalf("root validation did not report %s: %#v", invalid.mountPath, rootResult)
		}
	}
	goodResult, err := ws.Validate(ctx, "/good", factile.ValidateOptions{})
	if err != nil || !goodResult.Valid {
		t.Fatalf("unrelated local mount validation = %#v, %v", goodResult, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid Git validation initialized cache: %v", err)
	}
}

func TestWorkspaceViewValidationDoesNotHydrateGitOutsideView(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "overview.md"), "Guide", "Overview", "# Overview\n\n[Outside view](missing.md)\n")
	mustWriteWorkspace(t, filepath.Join(root, "offline.mount.toml"), `source = "file:///definitely/missing/factile.git"
writable = false
`)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.SetView(ctx, "local-only", factile.ViewInput{Paths: []string{"/overview"}}); err != nil {
		t.Fatal(err)
	}

	result, err := ws.Validate(ctx, "/", factile.ValidateOptions{View: "local-only"})
	if err != nil || !result.Valid || len(result.Issues) != 0 {
		t.Fatalf("local-only view validation = %#v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("out-of-view Git mount was hydrated: %v", err)
	}
}

func TestBriefListAndStatCards(t *testing.T) {
	workspaceDir := v2Workspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	root, err := ws.List(ctx, "/", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Cards) != 2 || len(root.Folders) != 0 || len(root.Documents) != 0 {
		t.Fatalf("unexpected root brief list: %#v", root)
	}
	if root.Cards[0].Path != "/engineering" || root.Cards[0].Title != "Engineering" {
		t.Fatalf("expected directory-derived engineering card: %#v", root.Cards[0])
	}
	if root.Cards[1].Path != "/legacy" || root.Cards[1].Writable == nil || !*root.Cards[1].Writable {
		t.Fatalf("expected writable legacy card: %#v", root.Cards[1])
	}

	group, err := ws.List(ctx, "/engineering", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(group.Cards) != 3 {
		t.Fatalf("unexpected mounted source cards: %#v", group.Cards)
	}
	django := group.Cards[1]
	if django.Path != "/engineering/django" || django.WhenToUse == "" || django.Writable == nil || !*django.Writable {
		t.Fatalf("expected writable Django mount card with guidance: %#v", django)
	}
	common := group.Cards[0]
	if common.Path != "/engineering/common" || common.Writable == nil || *common.Writable {
		t.Fatalf("expected read-only common mount card: %#v", common)
	}

	rootStat, err := ws.Stat(ctx, "/", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rootStat.Card.Title != "Test" {
		t.Fatalf("unexpected root stat: %#v", rootStat.Card)
	}
	groupStat, err := ws.Stat(ctx, "/engineering", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if groupStat.Card.Title != "Engineering" || groupStat.Card.Writable == nil || !*groupStat.Card.Writable {
		t.Fatalf("unexpected mounted group stat: %#v", groupStat.Card)
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

func hasDocumentPath(documents []factile.DocumentSummary, path string) bool {
	for _, document := range documents {
		if document.Path == path {
			return true
		}
	}
	return false
}

func hasCardPath(cards []factile.CardSummary, path string) bool {
	for _, card := range cards {
		if card.Path == path {
			return true
		}
	}
	return false
}

func hasCardTitle(cards []factile.CardSummary, path string, title string) bool {
	for _, card := range cards {
		if card.Path == path && card.Title == title {
			return true
		}
	}
	return false
}

func hasIssue(issues []factile.ValidationIssue, path string, code string) bool {
	for _, issue := range issues {
		if issue.Path == path && issue.Code == code {
			return true
		}
	}
	return false
}

func countIssues(issues []factile.ValidationIssue, path string, code string) int {
	count := 0
	for _, issue := range issues {
		if issue.Path == path && issue.Code == code {
			count++
		}
	}
	return count
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

func TestWorkspaceV2RootFilesDirectoriesAndMounts(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	mounted := filepath.Join(tmp, "mounted-product-docs")
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "overview.md"), "Guide", "Root Overview", "# Root Overview\n\nInvoice knowledge starts here and links to [Setup](guides/setup.md).\n")
	writeOKFFile(t, filepath.Join(root, "guides", "setup.md"), "Guide", "Setup", "# Setup\n\nSetup links to [Invoice](/mounted/workflows/invoice-import.md).\n")
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), mounted)
	mustWriteWorkspace(t, filepath.Join(mounted, "factile.toml"), "version = 2\n\n[bundle]\nname = \"mounted-product-docs\"\n")
	if err := os.WriteFile(filepath.Join(root, "mounted.mount.toml"), []byte(`source = "`+mounted+`"
writable = true
title = "Mounted Docs"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	rootList, err := ws.List(ctx, "/", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := folderPaths(rootList.Folders); strings.Join(got, ",") != "/guides,/mounted" || !hasDocumentPath(rootList.Documents, "/overview") {
		t.Fatalf("unexpected root list: folders=%#v documents=%#v", rootList.Folders, rootList.Documents)
	}
	rootBrief, err := ws.List(ctx, "/", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCardTitle(rootBrief.Cards, "/mounted", "Mounted Docs") {
		t.Fatalf("expected descriptor metadata in mounted card: %#v", rootBrief.Cards)
	}
	guideList, err := ws.List(ctx, "/guides", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(guideList.Documents) != 1 || guideList.Documents[0].Path != "/guides/setup" {
		t.Fatalf("unexpected root directory list: %#v", guideList.Documents)
	}
	mountedList, err := ws.List(ctx, "/mounted/workflows", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDocumentPath(mountedList.Documents, "/mounted/workflows/invoice-import") {
		t.Fatalf("unexpected mounted directory list: %#v", mountedList.Documents)
	}
	overview, err := ws.Read(ctx, "/overview.md", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Concept.Path != "/overview" || overview.Concept.ConceptID != "overview" {
		t.Fatalf("unexpected root read: %#v", overview.Concept)
	}
	created, err := ws.Create(ctx, "/guides/new-note", factile.CreateConceptInput{
		Type:     "Guide",
		Title:    "New Note",
		Markdown: "# New Note\n\nRoot-local writing works.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Concept.Path != "/guides/new-note" {
		t.Fatalf("unexpected root-created concept: %#v", created.Concept)
	}
	if _, err := os.Stat(filepath.Join(root, "guides", "new-note.md")); err != nil {
		t.Fatalf("expected root-local created file: %v", err)
	}

	search, err := ws.Search(ctx, "/", "invoice", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasPath(search, "/overview") || !searchHasPath(search, "/guides/setup") || !searchHasPath(search, "/mounted/workflows/invoice-import") {
		t.Fatalf("root search missed root or mounted documents: %#v", search.Results)
	}
	pack, err := ws.Context(ctx, "/", "invoice import workflow", factile.ContextOptions{MaxTokens: 4000, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasPath(pack, "/mounted/workflows/invoice-import") {
		t.Fatalf("root context missed mounted workflow: %#v", pack.Summaries)
	}
	graph, err := ws.Graph(ctx, "/", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasPath(graph, "/overview") || !graphHasPath(graph, "/guides/setup") || !graphHasPath(graph, "/mounted/workflows/invoice-import") || !graphHasEdge(graph, "/guides/setup", "/mounted/workflows/invoice-import") {
		t.Fatalf("root graph missed root or mounted links: %#v", graph)
	}
	validated, err := ws.Validate(ctx, "/", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !validated.Valid {
		t.Fatalf("expected valid root and mounted docs: %#v", validated.Issues)
	}
}

func TestWorkspaceV2RootWritePatchRenameDeleteDeprecate(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	created, err := ws.Create(ctx, "/guides/root-note", factile.CreateConceptInput{
		Type:     "Guide",
		Title:    "Root Note",
		Markdown: "# Root Note\n\nDraft.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Write(ctx, created.Concept.Path, factile.WriteConceptInput{Markdown: "# Missing revision\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionRequired {
		t.Fatalf("expected revision_required, got %v", err)
	}
	if _, err := ws.Write(ctx, created.Concept.Path, factile.WriteConceptInput{ExpectedRevision: "sha256:wrong", Markdown: "# Wrong\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionMismatch {
		t.Fatalf("expected revision_mismatch, got %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "guides", "root-note.md")); err != nil || strings.Contains(string(data), "# Wrong") {
		t.Fatalf("wrong-revision write changed root-local file, err=%v", err)
	}

	written, err := ws.Write(ctx, created.Concept.Path, factile.WriteConceptInput{
		ExpectedRevision: created.Concept.Revision,
		Markdown:         "# Root Note\n\nUpdated.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	patched, err := ws.Patch(ctx, written.Concept.Path, factile.PatchConceptInput{
		ExpectedRevision: written.Concept.Revision,
		Set:              map[string]any{"status": "draft"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Concept.Frontmatter["status"] != "draft" {
		t.Fatalf("patch did not apply to root-local file: %#v", patched.Concept.Frontmatter)
	}
	deprecated, err := ws.Deprecate(ctx, patched.Concept.Path, factile.DeprecateOptions{ExpectedRevision: patched.Concept.Revision, Reason: "superseded"})
	if err != nil {
		t.Fatal(err)
	}
	if deprecated.Concept.Frontmatter["deprecated"] != true {
		t.Fatalf("deprecate did not apply to root-local file: %#v", deprecated.Concept.Frontmatter)
	}
	renamed, err := ws.Rename(ctx, deprecated.Concept.Path, "/guides/root-note-v2", factile.RenameOptions{ExpectedRevision: deprecated.Concept.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Concept.Path != "/guides/root-note-v2" {
		t.Fatalf("unexpected renamed root-local path: %s", renamed.Concept.Path)
	}
	if _, err := os.Stat(filepath.Join(root, "guides", "root-note-v2.md")); err != nil {
		t.Fatalf("expected renamed root-local file: %v", err)
	}
	deleted, err := ws.Delete(ctx, renamed.Concept.Path, factile.DeleteOptions{ExpectedRevision: renamed.Concept.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Deleted {
		t.Fatal("delete result was false")
	}
	if _, err := os.Stat(filepath.Join(root, "guides", "root-note-v2.md")); !os.IsNotExist(err) {
		t.Fatalf("expected root-local file to be deleted, err=%v", err)
	}
}

func TestWorkspaceMkdirCreatesDirectoryScaffolds(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	created, err := ws.Mkdir(ctx, "/guides", factile.MkdirOptions{Title: "Guides", Log: true, Overview: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.Directory.Path != "/guides" || !created.Directory.Created {
		t.Fatalf("unexpected mkdir result: %#v", created.Directory)
	}
	if strings.Join(created.Directory.Files, ",") != "/guides/index.md,/guides/log.md,/guides/overview.md" {
		t.Fatalf("unexpected created files: %#v", created.Directory.Files)
	}
	for _, name := range []string{"index.md", "log.md", "overview.md"} {
		if _, err := os.Stat(filepath.Join(root, "guides", name)); err != nil {
			t.Fatalf("expected scaffold file %s: %v", name, err)
		}
	}
	overview, err := ws.Read(ctx, "/guides/overview", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Concept.Frontmatter["title"] != "Guides Overview" {
		t.Fatalf("unexpected overview frontmatter: %#v", overview.Concept.Frontmatter)
	}

	bundle, err := ws.Mkdir(ctx, "/guides/coding", factile.MkdirOptions{Title: "Coding", Bundle: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(bundle.Directory.Files, ",") != "/guides/coding/index.md,/guides/coding/log.md,/guides/coding/overview.md" {
		t.Fatalf("unexpected bundle files: %#v", bundle.Directory.Files)
	}
	index, err := os.ReadFile(filepath.Join(root, "guides", "coding", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `okf_version: "0.1"`) {
		t.Fatalf("bundle index missing okf_version:\n%s", index)
	}
}

func TestWorkspaceMkdirRejectsCollisionsAndMissingParents(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "overview.md"), "Guide", "Overview", "# Overview\n")
	writeOKFFile(t, filepath.Join(root, "topic.md"), "Guide", "Topic", "# Topic\n")
	if err := os.MkdirAll(filepath.Join(root, "topic"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	if _, err := ws.Mkdir(ctx, "/guides", factile.MkdirOptions{}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
		code string
	}{
		{name: "root", path: "/", code: factile.ErrInvalidPath},
		{name: "existing directory", path: "/guides", code: factile.ErrPathAlreadyExists},
		{name: "existing concept", path: "/overview", code: factile.ErrConceptAlreadyExist},
		{name: "ambiguous target", path: "/topic", code: factile.ErrAmbiguousTarget},
		{name: "missing parent", path: "/missing/deep", code: factile.ErrMountNotFound},
		{name: "concept parent", path: "/overview/deep", code: factile.ErrInvalidPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ws.Mkdir(ctx, tc.path, factile.MkdirOptions{})
			if factile.ErrorCode(factile.NormalizeError(err)) != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
		})
	}
}

func TestWorkspaceV2DescriptorMountWritePolicy(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	writableSource := filepath.Join(tmp, "writable")
	readOnlySource := filepath.Join(tmp, "read-only")
	writeRootConfig(t, root)
	if err := os.MkdirAll(writableSource, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteWorkspace(t, filepath.Join(writableSource, "factile.toml"), "version = 2\n\n[bundle]\nname = \"writable\"\n")
	mustWriteWorkspace(t, filepath.Join(readOnlySource, "factile.toml"), "version = 2\n\n[bundle]\nname = \"read-only\"\n")
	writeOKFFile(t, filepath.Join(readOnlySource, "existing.md"), "Guide", "Existing", "# Existing\n")
	if err := os.WriteFile(filepath.Join(root, "writable.mount.toml"), []byte(`source = "`+writableSource+`"
writable = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readonly.mount.toml"), []byte(`source = "`+readOnlySource+`"
writable = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	created, err := ws.Create(ctx, "/writable/new", factile.CreateConceptInput{
		Type:     "Guide",
		Title:    "New",
		Markdown: "# New\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Concept.Path != "/writable/new" {
		t.Fatalf("unexpected mounted write path: %s", created.Concept.Path)
	}
	if _, err := os.Stat(filepath.Join(writableSource, "new.md")); err != nil {
		t.Fatalf("expected writable mounted source file: %v", err)
	}
	dir, err := ws.Mkdir(ctx, "/writable/guides", factile.MkdirOptions{Title: "Guides", Bundle: true})
	if err != nil {
		t.Fatal(err)
	}
	if dir.Directory.Path != "/writable/guides" {
		t.Fatalf("unexpected mounted mkdir path: %#v", dir.Directory)
	}
	if _, err := os.Stat(filepath.Join(writableSource, "guides", "index.md")); err != nil {
		t.Fatalf("expected writable mounted scaffold: %v", err)
	}

	readOnlyDoc, err := ws.Read(ctx, "/readonly/existing", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Create(ctx, "/readonly/new", factile.CreateConceptInput{Type: "Guide", Title: "New", Markdown: "# New\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("expected read-only descriptor create rejection, got %v", err)
	}
	if _, err := ws.Mkdir(ctx, "/readonly/new", factile.MkdirOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("expected read-only descriptor mkdir rejection, got %v", err)
	}
	if _, err := ws.Write(ctx, readOnlyDoc.Concept.Path, factile.WriteConceptInput{ExpectedRevision: readOnlyDoc.Concept.Revision, Markdown: "# Changed\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("expected read-only descriptor write rejection, got %v", err)
	}
}

func TestWorkspaceV2ValidateReportsRootMetadataIssues(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "overview.md"), "Guide", "Overview", "# Overview\n")
	if err := os.WriteFile(filepath.Join(root, "factile.views.toml"), []byte(`[[views]]
id = "dup"
paths = ["/overview"]

[[views]]
id = "dup"
paths = ["/overview"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.mount.toml"), []byte(`source = "./missing"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	result, err := ws.Validate(ctx, "/", factile.ValidateOptions{})
	if err != nil {
		t.Fatalf("validate returned top-level error: %v", err)
	}
	if result.Valid || countIssues(result.Issues, "/", factile.ErrValidationFailed) < 2 {
		t.Fatalf("expected views and mount descriptor validation issues: %#v", result.Issues)
	}
}

func TestWorkspaceListVirtualFoldersFromNestedMounts(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	rootBundle := filepath.Join(workspace, "knowledge")
	docs := filepath.Join(tmp, "product-docs")
	mustWriteWorkspace(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"knowledge\"\n")
	mustWriteWorkspace(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"knowledge\"\n")
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), docs)
	mustWriteWorkspace(t, filepath.Join(docs, "factile.toml"), "version = 2\n\n[bundle]\nname = \"product-docs\"\n")
	mustWriteWorkspace(t, filepath.Join(rootBundle, "project", "docs.mount.toml"), "source = "+strconv.Quote(docs)+"\nwritable = true\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspace})
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
	readOnlyWorkspace, _ := workspaceForBundles(t, false)
	readOnly := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: readOnlyWorkspace, ReadOnly: true})
	if _, err := readOnly.Write(ctx, read.Concept.Path, factile.WriteConceptInput{ExpectedRevision: read.Concept.Revision, Markdown: "# No\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("expected source_read_only, got %v", err)
	}
	remoteRoot := filepath.Join(t.TempDir(), "remote-root")
	writeRootConfig(t, remoteRoot)
	mustWriteWorkspace(t, filepath.Join(remoteRoot, "remote.mount.toml"), "source = \"factile://bundle\"\nwritable = false\n")
	remote := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: remoteRoot})
	if _, err := remote.List(ctx, "/remote", factile.ListOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected unsupported_source, got %v", err)
	}
	if _, err := remote.Search(ctx, "/", "anything", factile.SearchOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected root search unsupported_source, got %v", err)
	}
	if _, err := remote.Validate(ctx, "/", factile.ValidateOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected root validate unsupported_source, got %v", err)
	}
	if _, err := remote.Mkdir(ctx, "/remote/new", factile.MkdirOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("expected read-only remote mkdir source_read_only, got %v", err)
	}
	writableRemoteRoot := filepath.Join(t.TempDir(), "remote-writable-root")
	writeRootConfig(t, writableRemoteRoot)
	mustWriteWorkspace(t, filepath.Join(writableRemoteRoot, "remote.mount.toml"), "source = \"factile://bundle\"\nwritable = true\n")
	writableRemote := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: writableRemoteRoot})
	if _, err := writableRemote.Mkdir(ctx, "/remote/new", factile.MkdirOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("expected writable remote mkdir unsupported_source, got %v", err)
	}
}

func TestWorkspaceMountAndUnmountDescriptor(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	source := filepath.Join(tmp, "source")
	writeRootConfig(t, root)
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteWorkspace(t, filepath.Join(source, "factile.toml"), "version = 2\n\n[bundle]\nname = \"source\"\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	mounted, err := ws.Mount(ctx, source, "/engineering/django", factile.MountOptions{Title: "Django", Description: "Framework docs"})
	if err != nil {
		t.Fatal(err)
	}
	if mounted.Mount.MountPath != "/engineering/django" || mounted.Mount.Source != source || mounted.Mount.Writable {
		t.Fatalf("unexpected descriptor mount: %#v", mounted.Mount)
	}
	descriptor := filepath.Join(root, "engineering", "django.mount.toml")
	data, err := os.ReadFile(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `title = "Django"`) || !strings.Contains(string(data), "writable = false") {
		t.Fatalf("descriptor missing metadata:\n%s", string(data))
	}
	list, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Mounts) != 1 || list.Mounts[0].MountPath != "/engineering/django" || list.Mounts[0].Title != "Django" {
		t.Fatalf("unexpected descriptor mounts: %#v", list.Mounts)
	}
	unmounted, err := ws.Unmount(ctx, "/engineering/django", factile.UnmountOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !unmounted.Removed {
		t.Fatal("unmount did not remove descriptor")
	}
	if _, err := os.Stat(descriptor); !os.IsNotExist(err) {
		t.Fatalf("expected descriptor removed, err=%v", err)
	}
}

func TestWorkspaceMountDescriptorRejectsUnsupportedRemoteSources(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	tests := []struct {
		name      string
		source    string
		mountPath string
		opts      factile.MountOptions
	}{
		{name: "factile URI", source: "factile://remote/source", mountPath: "/remote", opts: factile.MountOptions{}},
		{name: "remote kind", source: "../source", mountPath: "/kind", opts: factile.MountOptions{Kind: "remote"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ws.Mount(ctx, tc.source, tc.mountPath, tc.opts); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
				t.Fatalf("expected unsupported_source, got %v", err)
			}
			descriptor := filepath.Join(root, strings.TrimPrefix(tc.mountPath, "/")+".mount.toml")
			if _, err := os.Stat(descriptor); !os.IsNotExist(err) {
				t.Fatalf("descriptor should not be written for unsupported source, err=%v", err)
			}
		})
	}
}

func TestWorkspaceGitMountAndReaderIntegration(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	remote, remotePath, sourcePath := gitWorkspaceRemote(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	for _, tc := range []struct {
		mountPath string
		opts      factile.MountOptions
	}{
		{mountPath: "/missing-ref", opts: factile.MountOptions{Ref: "missing", RefSet: true}},
		{mountPath: "/missing-revision", opts: factile.MountOptions{Revision: strings.Repeat("1", 40), RevisionSet: true}},
	} {
		if _, err := ws.Mount(ctx, remote, tc.mountPath, tc.opts); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionNotAvailable {
			t.Fatalf("unavailable selector for %s error = %v", tc.mountPath, err)
		}
		if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(tc.mountPath, "/")+".mount.toml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unavailable selector wrote a descriptor for %s: %v", tc.mountPath, err)
		}
	}

	mounted, err := ws.Mount(ctx, remote, "/git", factile.MountOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if mounted.Mount.Source != remote || mounted.Mount.Kind != "git" || mounted.Mount.Writable || mounted.Mount.Title != "Git Fixture" {
		t.Fatalf("unexpected Git mount: %#v", mounted.Mount)
	}
	if _, err := ws.Mount(ctx, sourcePath, "/local", factile.MountOptions{}); err != nil {
		t.Fatal(err)
	}
	descriptor := filepath.Join(root, "git.mount.toml")
	descriptorData, err := os.ReadFile(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(descriptorData), `source = "`+remote+`"`) || !strings.Contains(string(descriptorData), "writable = false") {
		t.Fatalf("Git descriptor did not preserve source intent:\n%s", descriptorData)
	}
	missingReplacement := gitFileRemote(t, filepath.Join(t.TempDir(), "missing.git"))
	if _, err := ws.Mount(ctx, missingReplacement, "/git", factile.MountOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRemoteSourceUnavailable {
		t.Fatalf("failed Git replacement error = %v", err)
	}
	afterFailedReplacement, err := os.ReadFile(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFailedReplacement) != string(descriptorData) {
		t.Fatalf("failed Git replacement changed the descriptor:\nbefore:\n%s\nafter:\n%s", descriptorData, afterFailedReplacement)
	}
	mustWriteWorkspace(t, filepath.Join(root, "offline.mount.toml"), `source = "file:///definitely/missing/outside-scope.git"
writable = false
title = "Outside Scope"
`)

	listed, err := ws.List(ctx, "/git", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Documents) != 1 || listed.Documents[0].Path != "/git/overview" || len(listed.Folders) != 1 || listed.Folders[0].Path != "/git/guides" {
		t.Fatalf("unexpected Git list result: %#v", listed)
	}
	brief, err := ws.List(ctx, "/git", factile.ListOptions{Brief: true})
	if err != nil || len(brief.Cards) != 2 {
		t.Fatalf("unexpected Git brief list: %#v %v", brief, err)
	}
	read, err := ws.Read(ctx, "/git/overview", factile.ReadOptions{})
	if err != nil || !strings.Contains(read.Concept.Markdown, "Git Overview") {
		t.Fatalf("unexpected Git read: %#v %v", read, err)
	}
	stat, err := ws.Stat(ctx, "/git/overview", factile.StatOptions{})
	if err != nil || stat.Card.Path != read.Concept.Path || stat.Card.Title != "Git Overview" {
		t.Fatalf("unexpected Git stat: %#v %v", stat, err)
	}
	search, err := ws.Search(ctx, "/git", "setup", factile.SearchOptions{})
	if err != nil || len(search.Results) == 0 || !strings.HasPrefix(search.Results[0].Concept.Path, "/git/") {
		t.Fatalf("unexpected Git search: %#v %v", search, err)
	}
	contextPack, err := ws.Context(ctx, "/git", "setup", factile.ContextOptions{Depth: 1})
	if err != nil || len(contextPack.Concepts) == 0 {
		t.Fatalf("unexpected Git context: %#v %v", contextPack, err)
	}
	graph, err := ws.Graph(ctx, "/git/overview", factile.GraphOptions{Depth: 1})
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].To != "/git/guides/setup" {
		t.Fatalf("unexpected Git graph: %#v %v", graph, err)
	}
	validated, err := ws.Validate(ctx, "/git", factile.ValidateOptions{})
	if err != nil || !validated.Valid {
		t.Fatalf("unexpected Git validation: %#v %v", validated, err)
	}
	localListed, err := ws.List(ctx, "/local", factile.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	localRead, err := ws.Read(ctx, "/local/overview", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	localSearch, err := ws.Search(ctx, "/local", "setup", factile.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	localContext, err := ws.Context(ctx, "/local", "setup", factile.ContextOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	localGraph, err := ws.Graph(ctx, "/local/overview", factile.GraphOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	localValidated, err := ws.Validate(ctx, "/local", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]any{
		"list":       {listed, localListed},
		"read":       {read, localRead},
		"search":     {search, localSearch},
		"context":    {contextPack, localContext},
		"graph":      {graph, localGraph},
		"validation": {validated, localValidated},
	} {
		if gitValue, localValue := normalizedMountJSON(t, pair[0], "/git"), normalizedMountJSON(t, pair[1], "/local"); gitValue != localValue {
			t.Fatalf("%s differs between equivalent Git and local mounts\nGit:   %s\nlocal: %s", name, gitValue, localValue)
		}
	}
	if _, err := ws.SetView(ctx, "git-guides", factile.ViewInput{Title: "Git Guides", Paths: []string{"/git/guides"}}); err != nil {
		t.Fatal(err)
	}
	viewSearch, err := ws.Search(ctx, "/", "setup", factile.SearchOptions{View: "git-guides"})
	if err != nil || len(viewSearch.Results) == 0 || !strings.HasPrefix(viewSearch.Results[0].Concept.Path, "/git/guides/") {
		t.Fatalf("unexpected Git view search: %#v %v", viewSearch, err)
	}

	if _, err := ws.Write(ctx, "/git/overview", factile.WriteConceptInput{ExpectedRevision: read.Concept.Revision, Markdown: "# changed\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git write error = %v", err)
	}
	if _, err := ws.Mkdir(ctx, "/git/new", factile.MkdirOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git mkdir error = %v", err)
	}
	if _, err := ws.Create(ctx, "/git/new", factile.CreateConceptInput{Type: "Guide", Title: "New", Markdown: "# New\n"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git create error = %v", err)
	}
	if _, err := ws.Patch(ctx, "/git/overview", factile.PatchConceptInput{ExpectedRevision: read.Concept.Revision, Set: map[string]any{"status": "draft"}}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git patch error = %v", err)
	}
	if _, err := ws.Rename(ctx, "/git/overview", "/git/renamed", factile.RenameOptions{ExpectedRevision: read.Concept.Revision}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git rename error = %v", err)
	}
	if _, err := ws.Delete(ctx, "/git/overview", factile.DeleteOptions{ExpectedRevision: read.Concept.Revision}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git delete error = %v", err)
	}
	if _, err := ws.Deprecate(ctx, "/git/overview", factile.DeprecateOptions{ExpectedRevision: read.Concept.Revision, Reason: "superseded"}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
		t.Fatalf("Git deprecate error = %v", err)
	}
	if status := gitWorkspaceOutput(t, sourcePath, "status", "--porcelain"); status != "" {
		t.Fatalf("source repository was mutated: %s", status)
	}
	statuses, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gitStatus *factile.SourceStatus
	for _, mount := range statuses.Mounts {
		if mount.MountPath == "/git" {
			gitStatus = mount.SourceStatus
		}
	}
	if gitStatus == nil || !gitStatus.SnapshotAvailable || gitStatus.SelectedRevision == "" || gitStatus.RefreshDue {
		t.Fatalf("unexpected generated Git status: %#v", gitStatus)
	}
	offlineRemote := remotePath + ".offline"
	if err := os.Rename(remotePath, offlineRemote); err != nil {
		t.Fatal(err)
	}
	statuses, err = ws.ListMounts(ctx)
	if err != nil {
		t.Fatalf("mount status attempted network access: %v", err)
	}
	refreshed, err := ws.Refresh(ctx, "/git")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Outcome != "stale" || !refreshed.Status.Stale || refreshed.Warning == nil {
		t.Fatalf("unexpected stale workspace refresh: %#v", refreshed)
	}
	if staleRead, err := ws.Read(ctx, "/git/overview", factile.ReadOptions{}); err != nil || staleRead.Concept.Path != "/git/overview" {
		t.Fatalf("stale snapshot was not readable: %#v %v", staleRead, err)
	}
	if _, err := ws.Refresh(ctx, "/local"); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsupportedSource {
		t.Fatalf("local refresh error = %v", err)
	}
	if _, err := ws.Refresh(ctx, "/missing"); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrMountNotFound {
		t.Fatalf("missing refresh error = %v", err)
	}
	if err := os.Rename(offlineRemote, remotePath); err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(root, ".factile", "cache")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Unmount(ctx, "/git", factile.UnmountOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("unmount removed generated cache unexpectedly: %v", err)
	}
	if _, err := os.Stat(remotePath); err != nil {
		t.Fatalf("unmount touched source repository: %v", err)
	}
}

func TestWorkspaceGitMountUsesCachedSnapshotWhenGitIsUnavailable(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	remote, _, _ := gitWorkspaceRemote(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.Mount(ctx, remote, "/git", factile.MountOptions{}); err != nil {
		t.Fatal(err)
	}
	workspace, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := gitsource.OpenCache(workspace, gitsource.NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry("/git", remote)
	if err != nil {
		t.Fatal(err)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	state.LastAttemptAt = time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	if err := cache.WriteState(entry, state); err != nil {
		t.Fatal(err)
	}

	uncachedRoot := filepath.Join(t.TempDir(), "uncached-root")
	writeRootConfig(t, uncachedRoot)
	mustWriteWorkspace(t, filepath.Join(uncachedRoot, "git.mount.toml"), "source = \""+remote+"\"\nwritable = false\n")
	uncached := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: uncachedRoot})

	t.Setenv("PATH", t.TempDir())
	read, err := ws.Read(ctx, "/git/overview", factile.ReadOptions{})
	if err != nil || read.Concept.Path != "/git/overview" {
		t.Fatalf("cached read with missing Git = %#v, %v", read, err)
	}
	mounts, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus == nil || !mounts.Mounts[0].SourceStatus.Stale || mounts.Mounts[0].SourceStatus.LastErrorCode != factile.ErrRemoteSourceUnavailable {
		t.Fatalf("missing-Git stale status = %#v", mounts)
	}
	refreshed, err := ws.Refresh(ctx, "/git")
	if err != nil || refreshed.Outcome != "stale" || refreshed.Warning == nil {
		t.Fatalf("explicit missing-Git refresh = %#v, %v", refreshed, err)
	}
	if _, err := uncached.Read(ctx, "/git/overview", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRemoteSourceUnavailable {
		t.Fatalf("uncached missing-Git read error = %v", err)
	}
}

func TestWorkspaceRetriesFailedPinnedGitAcquisitionAfterInterval(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	remote, _, sourcePath := gitWorkspaceRemote(t)
	writeOKFFile(t, filepath.Join(sourcePath, "overview.md"), "Reference", "Delayed Git Overview", "# Delayed Git Overview\n")
	gitWorkspaceRun(t, sourcePath, "add", "--", "overview.md")
	gitWorkspaceRun(t, sourcePath, "commit", "-m", "delayed pin")
	revision := gitWorkspaceOutput(t, sourcePath, "rev-parse", "HEAD")
	mustWriteWorkspace(t, filepath.Join(root, "git.mount.toml"), "source = \""+remote+"\"\nwritable = false\nrevision = \""+revision+"\"\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.Read(ctx, "/git/overview", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionNotAvailable {
		t.Fatalf("initial unavailable pin error = %v", err)
	}
	gitWorkspaceRun(t, sourcePath, "push", "--", remote, "HEAD:refs/heads/delayed")
	if _, err := ws.Read(ctx, "/git/overview", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionNotAvailable {
		t.Fatalf("pin retried before interval: %v", err)
	}

	workspace, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := gitsource.OpenCache(workspace, gitsource.NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry("/git", remote)
	if err != nil {
		t.Fatal(err)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	state.LastAttemptAt = time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	if err := cache.WriteState(entry, state); err != nil {
		t.Fatal(err)
	}
	read, err := ws.Read(ctx, "/git/overview", factile.ReadOptions{})
	if err != nil || read.Concept.Path != "/git/overview" || !strings.Contains(read.Concept.Markdown, "Delayed Git Overview") {
		t.Fatalf("eligible pinned acquisition = %#v, %v", read, err)
	}
	mounts, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus == nil || mounts.Mounts[0].SourceStatus.RefreshDue || mounts.Mounts[0].SourceStatus.SelectedRevision != revision {
		t.Fatalf("pinned workspace status = %#v", mounts)
	}
}

func TestWorkspaceRedactsInvalidGitDescriptorStatus(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	source := "https://alice:correct-horse@example.test/private.git?token=hunter2"
	mustWriteWorkspace(t, filepath.Join(root, "private.mount.toml"), "source = \""+source+"\"\nwritable = false\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	listed, err := ws.ListMounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Mounts) != 1 || listed.Mounts[0].Source != "[redacted]" || listed.Mounts[0].SourceStatus == nil || listed.Mounts[0].SourceStatus.Source != "[redacted]" || listed.Mounts[0].SourceStatus.LastErrorCode != factile.ErrValidationFailed {
		t.Fatalf("invalid Git descriptor was not safely reported: %#v", listed)
	}
	encoded := normalizedMountJSON(t, listed, "")
	for _, secret := range []string{"alice", "correct-horse", "hunter2", source} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("mount status exposed %q: %s", secret, encoded)
		}
	}
	if _, err := ws.Read(context.Background(), "/private/anything", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("invalid descriptor read error = %v", err)
	}
}

func TestWorkspaceMountStatusDoesNotInitializeCache(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	mustWriteWorkspace(t, filepath.Join(root, "offline.mount.toml"), `source = "file:///definitely/missing/factile.git"
writable = false
`)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	listed, err := ws.ListMounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Mounts) != 1 || listed.Mounts[0].SourceStatus == nil || listed.Mounts[0].SourceStatus.SnapshotAvailable {
		t.Fatalf("unexpected pristine mount status: %#v", listed)
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mount status initialized cache: %v", err)
	}
}

func TestWorkspaceGitMountFailureAndLazyRootListing(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	emptyPath := filepath.Join(t.TempDir(), "empty.git")
	gitWorkspaceRun(t, "", "init", "--bare", "--", emptyPath)
	emptyRemote := gitFileRemote(t, emptyPath)
	if _, err := ws.Mount(ctx, emptyRemote, "/failed", factile.MountOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRevisionNotAvailable {
		t.Fatalf("empty Git mount error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "failed.mount.toml")); !os.IsNotExist(err) {
		t.Fatalf("failed Git mount wrote a descriptor: %v", err)
	}

	mustWriteWorkspace(t, filepath.Join(root, "offline.mount.toml"), `source = "file:///definitely/missing/factile.git"
writable = false
title = "Offline Git"
`)
	listed, err := ws.List(ctx, "/", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatalf("root listing hydrated offline Git: %v", err)
	}
	found := false
	for _, card := range listed.Cards {
		if card.Path == "/offline" && card.Title == "Offline Git" {
			found = true
		}
	}
	if !found {
		t.Fatalf("root listing omitted descriptor-only Git card: %#v", listed.Cards)
	}
	if _, err := ws.Search(ctx, "/", "anything", factile.SearchOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrRemoteSourceUnavailable {
		t.Fatalf("broad scan did not hydrate in-scope Git mount: %v", err)
	}
}

func TestWorkspaceRejectsInvalidGitSelectorsBeforeWritingDescriptor(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	remote, _, sourcePath := gitWorkspaceRemote(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	for _, tc := range []struct {
		mountPath string
		opts      factile.MountOptions
	}{
		{mountPath: "/empty-ref", opts: factile.MountOptions{RefSet: true}},
		{mountPath: "/empty-revision", opts: factile.MountOptions{RevisionSet: true}},
		{mountPath: "/empty-version", opts: factile.MountOptions{VersionSet: true}},
		{mountPath: "/sha256-lower", opts: factile.MountOptions{Revision: strings.Repeat("a", 64), RevisionSet: true}},
		{mountPath: "/sha256-upper", opts: factile.MountOptions{Revision: strings.Repeat("A", 64), RevisionSet: true}},
	} {
		if _, err := ws.Mount(ctx, remote, tc.mountPath, tc.opts); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
			t.Fatalf("empty selector for %s error = %v", tc.mountPath, err)
		}
		descriptor := filepath.Join(root, strings.TrimPrefix(tc.mountPath, "/")+".mount.toml")
		if _, err := os.Stat(descriptor); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid selector wrote %s: %v", descriptor, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid selectors initialized Git cache: %v", err)
	}
	if _, err := ws.Mount(ctx, sourcePath, "/local-ref", factile.MountOptions{Ref: "main", RefSet: true}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
		t.Fatalf("local source accepted Git selector: %v", err)
	}
}

func TestWorkspaceRejectsGitURIQueryAndFragmentDelimitersBeforeMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	for _, tc := range []struct {
		mountPath string
		source    string
	}{
		{mountPath: "/native-empty-query", source: "https://example.test/repository.git?"},
		{mountPath: "/native-query", source: "https://example.test/repository.git?token=value"},
		{mountPath: "/native-empty-fragment", source: "https://example.test/repository.git#"},
		{mountPath: "/native-fragment", source: "https://example.test/repository.git#private"},
		{mountPath: "/git-plus-empty-query", source: "git+https://example.test/repository.git?"},
		{mountPath: "/git-plus-query", source: "git+https://example.test/repository.git?token=value"},
		{mountPath: "/git-plus-empty-fragment", source: "git+https://example.test/repository.git#"},
		{mountPath: "/git-plus-fragment", source: "git+https://example.test/repository.git#private"},
	} {
		if _, err := ws.Mount(context.Background(), tc.source, tc.mountPath, factile.MountOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrValidationFailed {
			t.Fatalf("URI delimiter source %q error = %v", tc.source, err)
		}
		descriptor := filepath.Join(root, strings.TrimPrefix(tc.mountPath, "/")+".mount.toml")
		if _, err := os.Stat(descriptor); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("URI delimiter source wrote %s: %v", descriptor, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("URI delimiter sources initialized Git cache: %v", err)
	}
}

func TestWorkspaceReportsEmptyGitURIDelimitersAsScopedInvalidDescriptors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	invalid := []struct {
		mountPath string
		source    string
	}{
		{mountPath: "/native-query", source: "https://example.test/repository.git?"},
		{mountPath: "/native-fragment", source: "https://example.test/repository.git#"},
		{mountPath: "/git-plus-query", source: "git+https://example.test/repository.git?"},
		{mountPath: "/git-plus-fragment", source: "git+https://example.test/repository.git#"},
	}
	for _, tc := range invalid {
		filename := strings.TrimPrefix(tc.mountPath, "/") + ".mount.toml"
		mustWriteWorkspace(t, filepath.Join(root, filename), "source = "+strconv.Quote(tc.source)+"\nwritable = false\n")
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	listed, err := ws.ListMounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Mounts) != len(invalid) {
		t.Fatalf("invalid descriptor count = %d, want %d: %#v", len(listed.Mounts), len(invalid), listed)
	}
	for _, mount := range listed.Mounts {
		if mount.Source != "[redacted]" || mount.SourceStatus == nil || mount.SourceStatus.Source != "[redacted]" || mount.SourceStatus.LastErrorCode != factile.ErrValidationFailed {
			t.Fatalf("invalid URI descriptor was not safely scoped: %#v", mount)
		}
	}
	validated, err := ws.Validate(context.Background(), "/", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range invalid {
		if validated.Valid || !hasIssue(validated.Issues, tc.mountPath, factile.ErrValidationFailed) {
			t.Fatalf("root validation did not report %s: %#v", tc.mountPath, validated)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid loaded URI descriptors initialized Git cache: %v", err)
	}
}

func TestWorkspaceRejectsWritableNonLocalMounts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	for _, source := range []string{
		"factile://remote/source",
		"git+https://example.test/docs.git",
		"https://github.com/senseware/coding-practice.git",
		"git@github.com:senseware/coding-practice.git",
	} {
		if _, err := ws.Mount(ctx, source, "/remote", factile.MountOptions{Writable: true}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
			t.Fatalf("writable non-local source %q should return source_read_only, got %v", source, err)
		}
		if _, err := os.Stat(filepath.Join(root, "remote.mount.toml")); !os.IsNotExist(err) {
			t.Fatalf("writable non-local source %q wrote a descriptor: %v", source, err)
		}
	}
}

func TestWorkspaceMountDescriptorRejectsRootPathConflicts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	writeRootConfig(t, root)
	writeOKFFile(t, filepath.Join(root, "docs.md"), "Guide", "Docs", "# Docs\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.Mount(ctx, "../source", "/docs", factile.MountOptions{Writable: true}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrAmbiguousTarget {
		t.Fatalf("expected ambiguous target, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs.mount.toml")); !os.IsNotExist(err) {
		t.Fatalf("descriptor should not be written on conflict, err=%v", err)
	}
}

func TestWorkspaceLegacyRegistryCannotBypassWorkspace(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mount-registry.toml")
	source := filepath.Join(tmp, "bundle")
	mustWriteWorkspace(t, filepath.Join(source, "factile.toml"), "version = 2\n\n[bundle]\nname = \"bundle\"\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: registry})
	if _, err := ws.Mount(ctx, source, "/docs", factile.MountOptions{Writable: true, Kind: "local"}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrInvalidWorkspace {
		t.Fatalf("legacy registry bypass error = %v", err)
	}
	if _, err := os.Stat(registry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy registry was mutated: %v", err)
	}
}

func TestWorkspaceMountFileRejectsGitBeforeMutation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mount-registry.toml")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: registry})

	_, err := ws.Mount(ctx, "https://example.test/coding.git", "/coding", factile.MountOptions{Ref: "main", RefSet: true})
	normalized := factile.NormalizeError(err)
	var app *factile.AppError
	if !errors.As(normalized, &app) || app.Code != vfs.ErrInvalidWorkspace || app.Message != "Legacy mount-file composition does not select a Factile workspace." {
		t.Fatalf("unexpected legacy Git mount error: %v", err)
	}
	if _, err := os.Stat(registry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected Git mount changed registry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected Git mount initialized cache: %v", err)
	}
}

func TestWorkspaceMountDoesNotOverwriteMalformedRegistry(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mount-registry.toml")
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
	root := filepath.Join(tmp, "root")
	writeRootConfig(t, root)
	bundle := filepath.Join(tmp, "bundle")
	mustWriteWorkspace(t, filepath.Join(bundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"bad\"\n")
	if err := os.WriteFile(filepath.Join(bundle, "bad.md"), []byte("---\ntype Workflow\n---\n\n# Bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustWriteWorkspace(t, filepath.Join(root, "bad.mount.toml"), "source = "+strconv.Quote(bundle)+"\nwritable = true\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	result, err := ws.Validate(ctx, "/bad", factile.ValidateOptions{})
	if err != nil {
		t.Fatalf("validate returned top-level error: %v", err)
	}
	if result.Valid || len(result.Issues) != 1 || result.Issues[0].Code != factile.ErrOKFParse {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestWorkspaceUsesActiveRootFromParentDirectory(t *testing.T) {
	ctx := context.Background()
	workspace := v2Workspace(t)
	child := filepath.Join(workspace, "docs", "nested")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: child})
	result, err := ws.List(ctx, "/", factile.ListOptions{Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCardPath(result.Cards, "/engineering") {
		t.Fatalf("expected parent root card from child workspace: %#v", result.Cards)
	}
	if _, err := os.Stat(filepath.Join(child, ".factile")); !os.IsNotExist(err) {
		t.Fatalf("child workspace should not get nested .factile, err=%v", err)
	}
}

func TestWorkspaceRequiresActiveWorkspace(t *testing.T) {
	ctx := context.Background()
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: t.TempDir()})
	if _, err := ws.List(ctx, "/", factile.ListOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrNoActiveWorkspace {
		t.Fatalf("expected no active workspace list error, got %v", err)
	}
	if _, err := ws.Mount(ctx, ".", "/docs", factile.MountOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrNoActiveWorkspace {
		t.Fatalf("expected no active workspace mount error, got %v", err)
	}
}

func testWorkspace(t *testing.T) (*factile.LocalWorkspace, string) {
	t.Helper()
	workspace, product := workspaceForBundles(t, true)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspace})
	return ws, product
}

func v2Workspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	writeRootConfig(t, workspace)
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles"), filepath.Join(tmp, "bundles"))
	mustWriteWorkspace(t, filepath.Join(tmp, "bundles", "shared-guides", "factile.toml"), "version = 2\n\n[bundle]\nname = \"shared-guides\"\n")
	mustWriteWorkspace(t, filepath.Join(tmp, "bundles", "product-docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"product-docs\"\n")
	mustWriteWorkspace(t, filepath.Join(tmp, "bundles", "broken-docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"broken-docs\"\n")
	mustWriteWorkspace(t, filepath.Join(workspace, "engineering", "common.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Common Engineering Guides"
description = "Shared setup and operating guides."
trust = "shared"
`)
	mustWriteWorkspace(t, filepath.Join(workspace, "engineering", "django.mount.toml"), `source = "../../bundles/product-docs"
writable = true
title = "Django Product Docs"
description = "Product workflow and runbook examples."
when_to_use = "Use when working on invoice import workflows or runbooks."
trust = "local"
`)
	mustWriteWorkspace(t, filepath.Join(workspace, "engineering", "playbook.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Engineering Playbook"
description = "The same shared guides mounted at another path."
trust = "shared"
`)
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "legacy-notes"), filepath.Join(workspace, "legacy"))
	return workspace
}

func mustWriteWorkspace(t *testing.T, filename string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRootConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "factile.toml"), []byte(`version = 2

[workspace]
root = "."

[bundle]
name = "test"
title = "Test"

[defaults]
format = "okf"
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeOKFFile(t *testing.T, filename string, typ string, title string, markdown string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `---
type: ` + typ + `
title: ` + title + `
---

` + markdown
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitWorkspaceRemote(t *testing.T) (string, string, string) {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), "source")
	mustWriteWorkspace(t, filepath.Join(sourcePath, "factile.toml"), `version = 2

[bundle]
name = "git-fixture"
title = "Git Fixture"
description = "Git-backed reader fixture."

[defaults]
format = "okf"
`)
	writeOKFFile(t, filepath.Join(sourcePath, "overview.md"), "Reference", "Git Overview", "# Git Overview\n\n[Setup](guides/setup.md)\n")
	writeOKFFile(t, filepath.Join(sourcePath, "guides", "setup.md"), "Guide", "Setup Guide", "# Setup\n\nConfigure the fixture.\n")
	gitWorkspaceRun(t, "", "init", "--", sourcePath)
	gitWorkspaceRun(t, sourcePath, "config", "--local", "--", "user.name", "Factile Test")
	gitWorkspaceRun(t, sourcePath, "config", "--local", "--", "user.email", "factile@example.test")
	gitWorkspaceRun(t, sourcePath, "add", "--", ".")
	gitWorkspaceRun(t, sourcePath, "commit", "-m", "fixture")
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	gitWorkspaceRun(t, "", "clone", "--bare", "--", sourcePath, remotePath)
	return gitFileRemote(t, remotePath), remotePath, sourcePath
}

func gitFileRemote(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
}

func gitWorkspaceRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitsource.NewRunner().Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func gitWorkspaceOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := gitsource.NewRunner().Run(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func normalizedMountJSON(t *testing.T, value any, mountPath string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), mountPath, "/mount")
}

func workspaceForBundles(t *testing.T, writable bool) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	product := filepath.Join(workspace, "bundles", "product-docs")
	broken := filepath.Join(workspace, "bundles", "broken-docs")
	mustWriteWorkspace(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	mustWriteWorkspace(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"test-root\"\n")
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	copyDir(t, filepath.Join("..", "..", "testdata", "bundles", "broken-docs"), broken)
	mustWriteWorkspace(t, filepath.Join(product, "factile.toml"), "version = 2\n\n[bundle]\nname = \"product-docs\"\n")
	mustWriteWorkspace(t, filepath.Join(broken, "factile.toml"), "version = 2\n\n[bundle]\nname = \"broken-docs\"\n")
	mustWriteWorkspace(t, filepath.Join(rootBundle, "product-docs.mount.toml"), "source = \"../bundles/product-docs\"\nwritable = "+boolString(writable)+"\n")
	mustWriteWorkspace(t, filepath.Join(rootBundle, "broken-docs.mount.toml"), "source = \"../bundles/broken-docs\"\nwritable = true\n")
	return workspace, product
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
