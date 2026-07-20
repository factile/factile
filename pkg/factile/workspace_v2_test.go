package factile_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/vfs"
)

func TestWorkspaceV2CompositionAndWritesAreCWDInvariant(t *testing.T) {
	workDirs := []string{
		".",
		"docs",
		"docs/guides/deep",
		"bundles/reference",
		"bundles/notes/section",
	}
	for _, relativeWorkDir := range workDirs {
		t.Run(relativeWorkDir, func(t *testing.T) {
			workspace := newWorkspaceV2Fixture(t)
			workDir := filepath.Join(workspace, filepath.FromSlash(relativeWorkDir))
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				t.Fatal(err)
			}
			ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workDir})
			ctx := context.Background()

			root, err := ws.List(ctx, "/", factile.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasDocumentPath(root.Documents, "/overview") || !hasFolderPath(root.Folders, "/reference") {
				t.Fatalf("unexpected root listing from %s: %#v", relativeWorkDir, root)
			}
			if hasFolderPath(root.Folders, "/bundles") || hasDocumentPath(root.Documents, "/hidden") {
				t.Fatalf("physical workspace containment leaked into logical root: %#v", root)
			}

			overview, err := ws.Read(ctx, "/overview", factile.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if overview.Concept.Frontmatter["title"] != "Workspace Overview" {
				t.Fatalf("read wrong root-bundle concept: %#v", overview.Concept)
			}
			api, err := ws.Read(ctx, "/reference/guides/api", factile.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if api.Concept.Frontmatter["title"] != "Reference API" {
				t.Fatalf("read wrong mounted concept: %#v", api.Concept)
			}
			if _, err := ws.Read(ctx, "/notes/hidden", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrConceptNotFound {
				t.Fatalf("unmounted bundle read error = %v", err)
			}

			created, err := ws.Create(ctx, "/created", factile.CreateConceptInput{
				Type:     "Reference",
				Title:    "Created From Any CWD",
				Markdown: "# Created\n",
			})
			if err != nil {
				t.Fatal(err)
			}
			if created.Concept.Path != "/created" {
				t.Fatalf("unexpected created concept: %#v", created.Concept)
			}
			if _, err := os.Stat(filepath.Join(workspace, "docs", "created.md")); err != nil {
				t.Fatalf("write did not target root bundle: %v", err)
			}
			if relativeWorkDir != "docs" {
				if _, err := os.Stat(filepath.Join(workDir, "created.md")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("write targeted caller CWD: %v", err)
				}
			}
			if _, err := ws.Write(ctx, "/reference/guides/api", factile.WriteConceptInput{
				ExpectedRevision: api.Concept.Revision,
				Markdown:         api.Concept.Markdown,
			}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrSourceReadOnly {
				t.Fatalf("read-only mounted write error = %v", err)
			}
			if _, err := ws.Create(ctx, "/writable/from-cwd", factile.CreateConceptInput{
				Type:     "Reference",
				Title:    "Mounted Write",
				Markdown: "# Mounted Write\n",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(workspace, "bundles", "writable", "from-cwd.md")); err != nil {
				t.Fatalf("write did not target writable mounted bundle: %v", err)
			}
		})
	}
}

func TestWorkspaceV2NearestNestedWorkspaceWins(t *testing.T) {
	outer := newWorkspaceV2Fixture(t)
	inner := filepath.Join(outer, "component")
	mustWriteV2(t, filepath.Join(inner, "factile.toml"), `version = 2

[workspace]
root = "."

[bundle]
name = "component"
title = "Component Knowledge"
`)
	writeOKFV2(t, filepath.Join(inner, "overview.md"), "Reference", "Component Overview", "# Component\n")
	workDir := filepath.Join(inner, "src", "deep")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workDir})
	result, err := ws.Read(context.Background(), "/overview", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Concept.Frontmatter["title"] != "Component Overview" {
		t.Fatalf("outer workspace won over nearer nested workspace: %#v", result.Concept)
	}
}

func TestWorkspaceV2RootCardDefaultNamesTheBundle(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	mustWriteV2(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspace})
	result, err := ws.Stat(context.Background(), "/", factile.StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Card.Title != "Factile Root Bundle" {
		t.Fatalf("root card title = %q", result.Card.Title)
	}
}

func TestWorkspaceV2ExplicitSelectionUsesWorkspaceOptionOnly(t *testing.T) {
	workspace := newWorkspaceV2Fixture(t)
	ctx := context.Background()
	explicit := factile.NewWorkspace(factile.WorkspaceOptions{
		Workspace: workspace,
		WorkDir:   t.TempDir(),
	})
	if _, err := explicit.Read(ctx, "/overview", factile.ReadOptions{}); err != nil {
		t.Fatal(err)
	}

	legacyRoot := factile.NewWorkspace(factile.WorkspaceOptions{Root: workspace})
	if _, err := legacyRoot.Read(ctx, "/overview", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrInvalidWorkspace {
		t.Fatalf("legacy root unexpectedly selected a workspace: %v", err)
	}
}

func TestWorkspaceV2RejectsPrivatePathsAndMissingContext(t *testing.T) {
	workspace := newWorkspaceV2Fixture(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: filepath.Join(workspace, "bundles", "notes")})
	if _, err := ws.Read(context.Background(), "/.factile/cache/secret", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrInvalidPath {
		t.Fatalf("private path error = %v", err)
	}

	detached := t.TempDir()
	mustWriteV2(t, filepath.Join(detached, "factile.toml"), "version = 2\n\n[bundle]\nname = \"detached\"\n")
	detachedWorkspace := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: detached})
	if _, err := detachedWorkspace.List(context.Background(), "/", factile.ListOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrNoActiveWorkspace {
		t.Fatalf("bundle-only context error = %v", err)
	}

	privateBundle := filepath.Join(workspace, ".factile", "private-bundle")
	mustWriteV2(t, filepath.Join(privateBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"private\"\n")
	if _, err := ws.Mount(context.Background(), privateBundle, "/private", factile.MountOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrInvalidBundle {
		t.Fatalf("private-state mount error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "docs", "private.mount.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected private-state mount wrote a descriptor: %v", err)
	}
}

func TestWorkspaceV2RejectsSymlinkDirectoryEscape(t *testing.T) {
	workspace := newWorkspaceV2Fixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	writeOKFV2(t, filepath.Join(outside, "secret.md"), "Reference", "Secret", "# Secret\n")
	if err := os.Symlink(outside, filepath.Join(workspace, "docs", "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspace})
	if _, err := ws.Read(context.Background(), "/escape/secret", factile.ReadOptions{}); factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrUnsafeSourcePath {
		t.Fatalf("symlink directory escape error = %v", err)
	}
}

func TestWorkspaceV2ViewsAreWorkspaceScopedAndCWDInvariant(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	secondary := filepath.Join(workspace, "bundles", "reference")
	mustWriteV2(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	mustWriteV2(t, filepath.Join(secondary, "factile.toml"), "version = 2\n\n[bundle]\nname = \"reference\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "reference.mount.toml"), "source = \"../bundles/reference\"\nwritable = false\ntitle = \"Reference\"\n")
	writeOKFV2(t, filepath.Join(rootBundle, "guides", "setup.md"), "Guide", "Setup", "# Setup\n")
	writeOKFV2(t, filepath.Join(secondary, "api.md"), "Reference", "API", "# API\n")
	mustWriteV2(t, filepath.Join(workspace, "factile.views.toml"), `[[views]]
id = "support"
title = "Support"
paths = ["/guides", "/reference"]
`)
	legacyViews := filepath.Join(rootBundle, ".factile", "views.toml")
	mustWriteV2(t, legacyViews, `[[views]]
id = "legacy"
paths = ["/guides"]
`)

	for _, workDir := range []string{workspace, rootBundle, secondary} {
		ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workDir})
		views, err := ws.ListViews(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(views.Views) != 1 || views.Views[0].ID != "support" {
			t.Fatalf("views from %s = %#v", workDir, views.Views)
		}
		listed, err := ws.List(context.Background(), "/", factile.ListOptions{View: "support", Brief: true})
		if err != nil {
			t.Fatal(err)
		}
		if !hasCardPathV2(listed.Cards, "/guides") || !hasCardPathV2(listed.Cards, "/reference") {
			t.Fatalf("workspace view from %s = %#v", workDir, listed.Cards)
		}
	}

	secondaryWorkspace := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: secondary})
	if _, err := secondaryWorkspace.SetView(context.Background(), "operations", factile.ViewInput{Paths: []string{"/guides"}}); err != nil {
		t.Fatal(err)
	}
	workspaceViews, err := os.ReadFile(filepath.Join(workspace, "factile.views.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workspaceViews), `id = "operations"`) {
		t.Fatalf("secondary-CWD mutation missed workspace view file:\n%s", workspaceViews)
	}
	legacyData, err := os.ReadFile(legacyViews)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacyData), "operations") {
		t.Fatalf("legacy root-bundle view file was mutated:\n%s", legacyData)
	}
}

func TestWorkspaceV2ViewReadsAreStateFreeAndMutationsUsePrivateState(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	mustWriteV2(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	mustWriteV2(t, filepath.Join(workspace, "factile.views.toml"), "[[views]]\nid = \"guides\"\npaths = [\"/guides\"]\n")
	writeOKFV2(t, filepath.Join(rootBundle, "guides", "setup.md"), "Guide", "Setup", "# Setup\n")

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: rootBundle})
	if _, err := ws.ListViews(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.List(context.Background(), "/", factile.ListOptions{View: "guides"}); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(workspace, ".factile")
	if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only view operations created state: %v", err)
	}

	if _, err := ws.SetView(context.Background(), "operations", factile.ViewInput{Paths: []string{"/guides"}}); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{stateDir, filepath.Join(stateDir, "locks")} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %o, want 700", directory, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "factile.views.toml.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("view lock appeared beside tracked config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootBundle, ".factile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state appeared inside root bundle: %v", err)
	}
}

func TestWorkspaceV2ViewMutationRejectsStateSymlink(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	mustWriteV2(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, ".factile")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: rootBundle})
	if _, err := ws.SetView(context.Background(), "unsafe", factile.ViewInput{Paths: []string{"/guides"}}); factile.ErrorCode(factile.NormalizeError(err)) != vfs.ErrInvalidWorkspace {
		t.Fatalf("state symlink error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "factile.views.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected mutation wrote workspace views: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("state mutation escaped through symlink: %#v", entries)
	}
}

func TestWorkspaceV2GitCacheIsReusedFromSecondaryBundleCWD(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	secondary := filepath.Join(workspace, "bundles", "notes")
	mustWriteV2(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	mustWriteV2(t, filepath.Join(secondary, "factile.toml"), "version = 2\n\n[bundle]\nname = \"notes\"\n")
	remote, _, _ := gitWorkspaceRemote(t)

	fromSecondary := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: secondary})
	if _, err := fromSecondary.Mount(context.Background(), remote, "/git", factile.MountOptions{}); err != nil {
		t.Fatal(err)
	}
	secondaryRead, err := fromSecondary.Read(context.Background(), "/git/overview", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fromRoot := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: rootBundle})
	rootRead, err := fromRoot.Read(context.Background(), "/git/overview", factile.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rootRead.Concept.Revision != secondaryRead.Concept.Revision {
		t.Fatalf("CWD selected a different Git snapshot: secondary=%s root=%s", secondaryRead.Concept.Revision, rootRead.Concept.Revision)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ".factile", "cache", "git"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("workspace Git cache entries = %#v, want one", entries)
	}
	for _, bundle := range []string{rootBundle, secondary} {
		if _, err := os.Stat(filepath.Join(bundle, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Git cache appeared inside bundle %s: %v", bundle, err)
		}
	}
}

func hasCardPathV2(cards []factile.CardSummary, path string) bool {
	for _, card := range cards {
		if card.Path == path {
			return true
		}
	}
	return false
}

func newWorkspaceV2Fixture(t *testing.T) string {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	reference := filepath.Join(workspace, "bundles", "reference")
	notes := filepath.Join(workspace, "bundles", "notes")
	writable := filepath.Join(workspace, "bundles", "writable")

	mustWriteV2(t, filepath.Join(workspace, "factile.toml"), `version = 2

[workspace]
root = "docs"
`)
	mustWriteV2(t, filepath.Join(rootBundle, "factile.toml"), `version = 2

[bundle]
name = "workspace-docs"
title = "Workspace Documentation"
`)
	mustWriteV2(t, filepath.Join(reference, "factile.toml"), "version = 2\n\n[bundle]\nname = \"reference\"\n")
	mustWriteV2(t, filepath.Join(notes, "factile.toml"), "version = 2\n\n[bundle]\nname = \"notes\"\n")
	mustWriteV2(t, filepath.Join(writable, "factile.toml"), "version = 2\n\n[bundle]\nname = \"writable\"\n")
	mustWriteV2(t, filepath.Join(rootBundle, "reference.mount.toml"), `source = "../bundles/reference"
writable = false
title = "Reference"
description = "Read-only reference bundle."
`)
	mustWriteV2(t, filepath.Join(rootBundle, "writable.mount.toml"), `source = "../bundles/writable"
writable = true
title = "Writable"
`)
	// This descriptor belongs to an unmounted secondary bundle and must not be
	// imported into the root bundle's composition.
	mustWriteV2(t, filepath.Join(notes, "hidden.mount.toml"), `source = "../reference"
writable = false
`)
	writeOKFV2(t, filepath.Join(rootBundle, "overview.md"), "Architecture", "Workspace Overview", "# Workspace\n")
	writeOKFV2(t, filepath.Join(reference, "guides", "api.md"), "Reference", "Reference API", "# API\n")
	writeOKFV2(t, filepath.Join(notes, "hidden.md"), "Reference", "Hidden Notes", "# Hidden\n")
	mustWriteV2(t, filepath.Join(workspace, ".factile", "private.md"), "# Private state\n")
	mustWriteV2(t, filepath.Join(rootBundle, ".git", "private.md"), "# Private Git data\n")
	return workspace
}

func mustWriteV2(t *testing.T, filename string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeOKFV2(t *testing.T, filename string, conceptType string, title string, body string) {
	t.Helper()
	mustWriteV2(t, filename, "---\ntype: "+conceptType+"\ntitle: "+title+"\n---\n\n"+body)
}

func hasFolderPath(folders []factile.FolderSummary, path string) bool {
	for _, folder := range folders {
		if folder.Path == path {
			return true
		}
	}
	return false
}
