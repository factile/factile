package factile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteViewsFilePreservesPublishedFileOnFailure(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "factile.views.toml")
	original := []byte("[[views]]\nid = \"original\"\npaths = [\"/guides\"]\n")
	if err := os.WriteFile(filename, original, 0o644); err != nil {
		t.Fatal(err)
	}
	publishErr := errors.New("publish failed")
	err := atomicWriteViewsFile(filename, []byte("replacement\n"), func(string, string) error {
		return publishErr
	})
	if !errors.Is(err, publishErr) {
		t.Fatalf("error = %v, want publish failure", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("published file changed after failed replace:\n%s", data)
	}
	entries, err := os.ReadDir(filepath.Dir(filename))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(filename) {
		t.Fatalf("temporary view file leaked after failure: %#v", entries)
	}
}

func TestWorkspaceLocksUsePrivateState(t *testing.T) {
	workspaceDir := viewTestWorkspace(t)
	ws := NewWorkspace(WorkspaceOptions{WorkDir: workspaceDir})
	target := filepath.Join(workspaceDir, "overview.md")
	stateDir := filepath.Join(workspaceDir, ".factile")
	locksDir := filepath.Join(stateDir, "locks")

	err := ws.withWorkspaceLocks([]string{target}, func() error {
		if _, err := os.Stat(target + ".lock"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("lock appeared beside authored content: %v", err)
		}
		for _, directory := range []string{stateDir, locksDir} {
			info, err := os.Stat(directory)
			if err != nil {
				return err
			}
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("%s mode = %o, want 700", directory, info.Mode().Perm())
			}
		}
		entries, err := os.ReadDir(locksDir)
		if err != nil {
			return err
		}
		if len(entries) != 1 {
			t.Fatalf("lock entries = %#v, want one", entries)
		}
		info, err := entries[0].Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("lock mode = %o, want 600", info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(locksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("released lock remains in state: %#v", entries)
	}
}

func TestScopeForViewIntersectsReaderScopes(t *testing.T) {
	workspaceDir := viewTestWorkspace(t)
	ws := NewWorkspace(WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()

	cases := []struct {
		name    string
		command string
		paths   []string
		want    []string
		notWant []string
	}{
		{
			name:    "mounted group root",
			command: "/",
			paths:   []string{"/engineering"},
			want: []string{
				"/engineering/django/workflows/invoice-import",
				"/engineering/common/guides/setup",
				"/engineering/playbook/guides/setup",
			},
		},
		{
			name:    "mounted source path",
			command: "/",
			paths:   []string{"/engineering/django"},
			want: []string{
				"/engineering/django/workflows/invoice-import",
				"/engineering/django/runbooks/ocr-failure",
			},
			notWant: []string{"/engineering/common/guides/setup"},
		},
		{
			name:    "folder path",
			command: "/",
			paths:   []string{"/engineering/django/runbooks"},
			want:    []string{"/engineering/django/runbooks/ocr-failure"},
			notWant: []string{"/engineering/django/workflows/invoice-import"},
		},
		{
			name:    "direct mount path",
			command: "/",
			paths:   []string{"/legacy"},
			want:    []string{"/legacy/notes/legacy"},
			notWant: []string{"/engineering/django/workflows/invoice-import"},
		},
		{
			name:    "command narrower than view",
			command: "/engineering/django/runbooks",
			paths:   []string{"/engineering/django"},
			want:    []string{"/engineering/django/runbooks/ocr-failure"},
			notWant: []string{"/engineering/django/workflows/invoice-import"},
		},
		{
			name:    "view narrower than command",
			command: "/engineering",
			paths:   []string{"/engineering/django/runbooks"},
			want:    []string{"/engineering/django/runbooks/ocr-failure"},
			notWant: []string{"/engineering/django/workflows/invoice-import"},
		},
		{
			name:    "unrelated view path",
			command: "/engineering",
			paths:   []string{"/legacy"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viewID := strings.ReplaceAll(tc.name, " ", "-")
			if _, err := ws.SetView(ctx, viewID, ViewInput{Paths: tc.paths}); err != nil {
				t.Fatal(err)
			}
			scope, err := ws.scopeForView(context.Background(), tc.command, viewID)
			if err != nil {
				t.Fatal(err)
			}
			if scope.Path != tc.command && !(tc.command == "" && scope.Path == "/") {
				t.Fatalf("scope path = %s, want %s", scope.Path, tc.command)
			}
			got := scopedConceptPaths(scope)
			for _, want := range tc.want {
				if !hasString(got, want) {
					t.Fatalf("scope missing %s in %#v", want, got)
				}
			}
			for _, notWant := range tc.notWant {
				if hasString(got, notWant) {
					t.Fatalf("scope included %s in %#v", notWant, got)
				}
			}
			if len(tc.want) == 0 && len(got) != 0 {
				t.Fatalf("expected empty intersection, got %#v", got)
			}
		})
	}
}

func TestScopeForViewDeduplicatesOverlappingPaths(t *testing.T) {
	workspaceDir := viewTestWorkspace(t)
	ws := NewWorkspace(WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()
	if _, err := ws.SetView(ctx, "overlap", ViewInput{Paths: []string{"/engineering/django", "/engineering/django/runbooks"}}); err != nil {
		t.Fatal(err)
	}
	scope, err := ws.scopeForView(context.Background(), "/", "overlap")
	if err != nil {
		t.Fatal(err)
	}
	paths := scopedConceptPaths(scope)
	if countString(paths, "/engineering/django/runbooks/ocr-failure") != 1 {
		t.Fatalf("expected overlapping paths to deduplicate concepts, got %#v", paths)
	}
	if !hasString(paths, "/engineering/django/workflows/invoice-import") {
		t.Fatalf("expected non-overlapping concept to remain, got %#v", paths)
	}
}

func TestScopeForViewPreservesViewPathOrder(t *testing.T) {
	workspaceDir := viewTestWorkspace(t)
	ws := NewWorkspace(WorkspaceOptions{WorkDir: workspaceDir})
	ctx := context.Background()
	if _, err := ws.SetView(ctx, "ordered", ViewInput{Paths: []string{"/legacy", "/engineering/django/runbooks"}}); err != nil {
		t.Fatal(err)
	}
	scope, err := ws.scopeForView(context.Background(), "/", "ordered")
	if err != nil {
		t.Fatal(err)
	}
	paths := scopedConceptPaths(scope)
	want := []string{"/legacy/notes/legacy", "/engineering/django/runbooks/ocr-failure"}
	if len(paths) < len(want) || strings.Join(paths[:len(want)], ",") != strings.Join(want, ",") {
		t.Fatalf("expected view path order %v at start, got %#v", want, paths)
	}
}

func TestScopeForViewRejectsUnknownView(t *testing.T) {
	workspaceDir := viewTestWorkspace(t)
	ws := NewWorkspace(WorkspaceOptions{WorkDir: workspaceDir})
	if _, err := ws.scopeForView(context.Background(), "/", "missing"); ErrorCode(NormalizeError(err)) != ErrMountNotFound {
		t.Fatalf("expected missing view to be not found, got %v", err)
	}
}

func viewTestWorkspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	mustWriteViewTestFile(t, filepath.Join(workspace, "factile.toml"), `version = 2

[workspace]
root = "."

[bundle]
name = "test"
title = "Test"

[defaults]
format = "okf"
`)
	copyViewTestDir(t, filepath.Join("..", "..", "testdata", "bundles"), filepath.Join(tmp, "bundles"))
	mustWriteViewTestFile(t, filepath.Join(tmp, "bundles", "shared-guides", "factile.toml"), "version = 2\n\n[bundle]\nname = \"shared-guides\"\n")
	mustWriteViewTestFile(t, filepath.Join(tmp, "bundles", "product-docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"product-docs\"\n")
	mustWriteViewTestFile(t, filepath.Join(workspace, "engineering", "common.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Common Engineering Guides"
`)
	mustWriteViewTestFile(t, filepath.Join(workspace, "engineering", "django.mount.toml"), `source = "../../bundles/product-docs"
writable = true
title = "Django Product Docs"
`)
	mustWriteViewTestFile(t, filepath.Join(workspace, "engineering", "playbook.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Engineering Playbook"
`)
	copyViewTestDir(t, filepath.Join("..", "..", "testdata", "bundles", "legacy-notes"), filepath.Join(workspace, "legacy"))
	return workspace
}

func mustWriteViewTestFile(t *testing.T, filename string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyViewTestDir(t *testing.T, src string, dst string) {
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
			copyViewTestDir(t, from, to)
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

func scopedConceptPaths(scope scopedSet) []string {
	paths := make([]string, 0, len(scope.Concepts))
	for _, item := range scope.Concepts {
		paths = append(paths, item.Concept.Path)
	}
	return paths
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
