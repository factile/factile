package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkspaceMountsIsCWDInvariant(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "project")
	rootBundle := filepath.Join(workspace, "docs")
	reference := filepath.Join(workspace, "bundles", "reference")
	unmounted := filepath.Join(workspace, "bundles", "notes")

	writeWorkspaceManifest(t, workspace, "docs")
	writeBundleManifest(t, rootBundle, "project-docs")
	writeBundleManifest(t, reference, "reference")
	writeBundleManifest(t, unmounted, "notes")
	mustWrite(t, filepath.Join(rootBundle, "overview.md"), "# Overview\n")
	mustWrite(t, filepath.Join(reference, "guides", "api.md"), "# API\n")
	mustWrite(t, filepath.Join(unmounted, "private.md"), "# Private\n")
	mustWrite(t, filepath.Join(rootBundle, "reference.mount.toml"), `source = "../bundles/reference"
writable = false
title = "Reference"
`)
	// A descriptor owned by a secondary bundle must not join this workspace's
	// logical tree merely because that bundle is physically contained.
	mustWrite(t, filepath.Join(unmounted, "hidden.mount.toml"), `source = "../reference"
writable = false
`)

	workDirs := []string{
		workspace,
		rootBundle,
		filepath.Join(rootBundle, "guides"),
		reference,
		unmounted,
	}
	for _, workDir := range workDirs {
		mustMkdir(t, workDir)
		t.Run(filepath.ToSlash(workDir), func(t *testing.T) {
			context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workDir})
			if err != nil {
				t.Fatal(err)
			}
			mounts, err := LoadWorkspaceMounts(context)
			if err != nil {
				t.Fatal(err)
			}
			if len(mounts) != 2 {
				t.Fatalf("mounts = %#v, want root and reference only", mounts)
			}
			root, ok := mountByTestPath(mounts, "/")
			if !ok || root.SourcePath != rootBundle || !root.Writable {
				t.Fatalf("unexpected root mount: %#v", root)
			}
			mounted, ok := mountByTestPath(mounts, "/reference")
			if !ok || mounted.SourcePath != reference || mounted.Writable {
				t.Fatalf("unexpected reference mount: %#v", mounted)
			}
			if _, ok := mountByTestPath(mounts, "/hidden"); ok {
				t.Fatalf("secondary-bundle descriptor leaked into composition: %#v", mounts)
			}

			target, err := Resolve(mounts, "/reference/guides/api.md")
			if err != nil {
				t.Fatal(err)
			}
			if target.Kind != TargetConcept || target.Mount.MountPath != "/reference" || target.ConceptID != "guides/api" {
				t.Fatalf("unexpected mounted target: %#v", target)
			}
			unmountedTarget, err := Resolve(mounts, "/notes/private")
			if err != nil {
				t.Fatal(err)
			}
			if unmountedTarget.Exists {
				t.Fatalf("unmounted secondary bundle became visible: %#v", unmountedTarget)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(workspace, StateDirname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only composition created state: %v", err)
	}
}

func TestLoadWorkspaceMountsPreservesCompositionGuards(t *testing.T) {
	t.Run("root content collision", func(t *testing.T) {
		workspace := t.TempDir()
		rootBundle := filepath.Join(workspace, "docs")
		source := filepath.Join(workspace, "bundle")
		writeWorkspaceManifest(t, workspace, "docs")
		writeBundleManifest(t, rootBundle, "docs")
		writeBundleManifest(t, source, "bundle")
		mustWrite(t, filepath.Join(rootBundle, "reference", "overview.md"), "# Local\n")
		mustWrite(t, filepath.Join(rootBundle, "reference.mount.toml"), "source = \"../bundle\"\nwritable = false\n")

		context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := LoadWorkspaceMounts(context); err == nil {
			t.Fatal("expected root content collision")
		}
	})

	t.Run("local source must be a bundle", func(t *testing.T) {
		workspace := t.TempDir()
		rootBundle := filepath.Join(workspace, "docs")
		source := filepath.Join(workspace, "plain-directory")
		writeWorkspaceManifest(t, workspace, "docs")
		writeBundleManifest(t, rootBundle, "docs")
		mustMkdir(t, source)
		mustWrite(t, filepath.Join(rootBundle, "plain.mount.toml"), "source = \"../plain-directory\"\nwritable = false\n")

		context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		_, err = LoadWorkspaceMounts(context)
		var layoutErr *Error
		if !errors.As(err, &layoutErr) || layoutErr.Code != ErrInvalidBundle {
			t.Fatalf("error = %v, want %s", err, ErrInvalidBundle)
		}
	})

	t.Run("workspace state cannot be mounted", func(t *testing.T) {
		workspace := t.TempDir()
		rootBundle := filepath.Join(workspace, "docs")
		stateBundle := filepath.Join(workspace, StateDirname, "cache", "private")
		writeWorkspaceManifest(t, workspace, "docs")
		writeBundleManifest(t, rootBundle, "docs")
		writeBundleManifest(t, stateBundle, "private")
		mustWrite(t, filepath.Join(rootBundle, "private.mount.toml"), "source = \"../.factile/cache/private\"\nwritable = false\n")

		context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		_, err = LoadWorkspaceMounts(context)
		var layoutErr *Error
		if !errors.As(err, &layoutErr) || layoutErr.Code != ErrInvalidBundle {
			t.Fatalf("error = %v, want %s", err, ErrInvalidBundle)
		}
	})
}

func mountByTestPath(mounts []Mount, mountPath string) (Mount, bool) {
	for _, mount := range mounts {
		if mount.MountPath == mountPath {
			return mount, true
		}
	}
	return Mount{}, false
}
