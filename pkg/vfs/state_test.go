package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceStatePathIsLazyPrivateAndContained(t *testing.T) {
	workspace := t.TempDir()
	writeCombinedWorkspaceManifest(t, workspace)
	context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}

	locks, err := StatePath(context, "locks")
	if err != nil {
		t.Fatal(err)
	}
	if locks != filepath.Join(workspace, StateDirname, "locks") {
		t.Fatalf("state path = %q", locks)
	}
	if _, err := os.Stat(context.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("StatePath created state: %v", err)
	}

	locks, err = EnsureStateDirectory(context, "locks")
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{context.StateDir, locks} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %o, want 700", directory, info.Mode().Perm())
		}
	}

	for _, relative := range []string{".", "..", "../outside", "locks/../outside", "/outside", `locks\outside`, "C:/outside"} {
		if _, err := StatePath(context, relative); err == nil {
			t.Fatalf("StatePath accepted %q", relative)
		}
	}
}

func TestEnsureWorkspaceStateDirectoryRejectsSymlinks(t *testing.T) {
	t.Run("state root", func(t *testing.T) {
		workspace := t.TempDir()
		writeCombinedWorkspaceManifest(t, workspace)
		context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, context.StateDir); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := EnsureStateDirectory(context, "locks"); err == nil {
			t.Fatal("expected state-root symlink rejection")
		}
		if _, err := os.Stat(filepath.Join(outside, "locks")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state write escaped through symlink: %v", err)
		}
	})

	t.Run("state child", func(t *testing.T) {
		workspace := t.TempDir()
		writeCombinedWorkspaceManifest(t, workspace)
		context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := EnsureStateDirectory(context, ""); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(context.StateDir, "locks")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := EnsureStateDirectory(context, "locks"); err == nil {
			t.Fatal("expected state-child symlink rejection")
		}
	})
}

func writeCombinedWorkspaceManifest(t *testing.T, workspace string) {
	t.Helper()
	mustWrite(t, filepath.Join(workspace, ManifestFilename), `version = 2

[workspace]
root = "."

[bundle]
name = "workspace"
`)
}
