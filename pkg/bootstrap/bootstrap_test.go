package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/vfs"
)

func TestInitCreatesDefaultWorkspaceWithoutLocalState(t *testing.T) {
	workspace := t.TempDir()
	result, err := Init(context.Background(), Options{
		WorkDir: workspace,
		Now:     time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspacePath != "." || result.RootBundlePath != "docs" {
		t.Fatalf("unexpected init paths: %#v", result)
	}
	for _, path := range []string{".gitignore", "factile.toml", "docs/factile.toml", "docs/index.md", "docs/overview.md"} {
		if initAction(result.Files, path) != "created" {
			t.Fatalf("%s action = %q, want created", path, initAction(result.Files, path))
		}
	}
	ignore, err := os.ReadFile(filepath.Join(workspace, ".gitignore"))
	if err != nil || string(ignore) != "/.factile/\n" {
		t.Fatalf("unexpected .gitignore: %q, %v", ignore, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, vfs.StateDirname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init created local state: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(workspace, "docs", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	parsedIndex, err := okf.ParseConcept("", index)
	if err != nil || parsedIndex.Frontmatter["type"] != "Index" {
		t.Fatalf("init created an invalid bundle index: %#v, %v", parsedIndex.Frontmatter, err)
	}
	context, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: filepath.Join(workspace, "docs")})
	if err != nil {
		t.Fatal(err)
	}
	if context.WorkspaceDir != workspace || context.RootBundleDir != filepath.Join(workspace, "docs") {
		t.Fatalf("unexpected resolved workspace: %#v", context)
	}
}

func TestInitHereCreatesCombinedWorkspace(t *testing.T) {
	workspace := t.TempDir()
	result, err := Init(context.Background(), Options{WorkDir: workspace, Here: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspacePath != "." || result.RootBundlePath != "." {
		t.Fatalf("unexpected init paths: %#v", result)
	}
	manifest, err := vfs.LoadManifest(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Workspace == nil || manifest.Workspace.Root != "." || manifest.Bundle == nil {
		t.Fatalf("unexpected combined manifest: %#v", manifest)
	}
	if _, err := os.Stat(filepath.Join(workspace, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init --here created docs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, vfs.StateDirname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init --here created local state: %v", err)
	}
}

func TestInitIsIdempotentAndPreservesExistingFiles(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("vendor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if initAction(first.Files, ".gitignore") != "updated" {
		t.Fatalf("first .gitignore action = %q", initAction(first.Files, ".gitignore"))
	}
	overview := filepath.Join(workspace, "docs", "overview.md")
	if err := os.WriteFile(overview, []byte("# Preserved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range second.Files {
		if change.Action != "unchanged" {
			t.Fatalf("repeated init changed %s: %#v", change.Path, second.Files)
		}
	}
	data, err := os.ReadFile(overview)
	if err != nil || string(data) != "# Preserved\n" {
		t.Fatalf("overview was overwritten: %q, %v", data, err)
	}
	ignore, err := os.ReadFile(filepath.Join(workspace, ".gitignore"))
	if err != nil || string(ignore) != "vendor/\n/.factile/\n" {
		t.Fatalf("existing ignore rules were not preserved: %q, %v", ignore, err)
	}
}

func TestInitRefusesLegacyPartialAndIncompatibleLayouts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		here  bool
	}{
		{
			name: "legacy",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, ".factile", "config.toml"), "version = 1\n")
			},
		},
		{
			name: "legacy docs",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "docs", ".factile", "views.toml"), "[views]\n")
			},
		},
		{
			name: "partial default",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
			},
		},
		{
			name: "partial combined",
			here: true,
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "factile.toml"), "version = 2\n\n[workspace]\nroot = \".\"\n")
			},
		},
		{
			name: "malformed combined",
			here: true,
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "factile.toml"), "not toml = [\n")
			},
		},
		{
			name: "malformed root bundle",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
				writeBootstrapTestFile(t, filepath.Join(dir, "docs", "factile.toml"), "not toml = [\n")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			tc.setup(t, workspace)
			_, err := Init(context.Background(), Options{WorkDir: workspace, Here: tc.here})
			var layoutErr *vfs.Error
			if !errors.As(err, &layoutErr) || layoutErr.Code != vfs.ErrInvalidWorkspace {
				t.Fatalf("error = %T %v, want invalid_workspace", err, err)
			}
			if _, err := os.Stat(filepath.Join(workspace, ".gitignore")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected init changed .gitignore: %v", err)
			}
		})
	}
}

func TestInitRejectsSymlinkGitignore(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("preserve\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Init(context.Background(), Options{WorkDir: workspace}); err == nil {
		t.Fatal("init accepted symlink .gitignore")
	}
	if _, err := os.Stat(filepath.Join(workspace, "factile.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected init created a workspace manifest: %v", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || strings.TrimSpace(string(data)) != "preserve" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
}

func TestInitRejectsSymlinkDocsBeforeMutation(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "docs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Init(context.Background(), Options{WorkDir: workspace}); err == nil {
		t.Fatal("init accepted symlink docs directory")
	}
	for _, filename := range []string{filepath.Join(workspace, ".gitignore"), filepath.Join(workspace, "factile.toml"), filepath.Join(outside, "factile.toml")} {
		if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rejected init created %s: %v", filename, err)
		}
	}
}

func initAction(files []FileChange, path string) string {
	for _, file := range files {
		if file.Path == path {
			return file.Action
		}
	}
	return ""
}

func writeBootstrapTestFile(t *testing.T, filename, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
