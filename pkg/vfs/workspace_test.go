package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestParsesV2Shapes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		check   func(t *testing.T, manifest Manifest)
	}{
		{
			name: "workspace",
			content: `version = 2

[workspace]
root = "docs"
`,
			check: func(t *testing.T, manifest Manifest) {
				if manifest.Workspace == nil || manifest.Workspace.Root != "docs" || manifest.Bundle != nil {
					t.Fatalf("unexpected workspace manifest: %#v", manifest)
				}
			},
		},
		{
			name: "bundle",
			content: `version = 2

[bundle]
name = "reference"
title = "Reference"
description = "Reference material."
when_to_use = "Use for reference questions."

[defaults]
format = "okf"
`,
			check: func(t *testing.T, manifest Manifest) {
				if manifest.Bundle == nil || manifest.Bundle.Name != "reference" || manifest.Bundle.Title != "Reference" || manifest.Bundle.Description == "" || manifest.Bundle.WhenToUse == "" || manifest.Defaults == nil || manifest.Defaults.Format != "okf" {
					t.Fatalf("unexpected bundle manifest: %#v", manifest)
				}
			},
		},
		{
			name: "combined standalone",
			content: `version = 2 # standard TOML comments are accepted

[workspace]
root = "."

[bundle]
name = 'standalone'
`,
			check: func(t *testing.T, manifest Manifest) {
				if manifest.Workspace == nil || manifest.Workspace.Root != "." || manifest.Bundle == nil || manifest.Bundle.Name != "standalone" {
					t.Fatalf("unexpected combined manifest: %#v", manifest)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, ManifestPath(dir), tc.content)
			manifest, err := LoadManifest(dir)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Version != 2 {
				t.Fatalf("version = %d, want 2", manifest.Version)
			}
			tc.check(t, manifest)
		})
	}
}

func TestLoadManifestRejectsInvalidSchema(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "malformed TOML", content: "version = \"unterminated\n", want: "factile.toml is not valid TOML:"},
		{name: "missing version", content: "[workspace]\nroot = \"docs\"\n", want: "Factile manifest version must be the integer 2."},
		{name: "wrong version type", content: "version = \"2\"\n[workspace]\nroot = \"docs\"\n", want: "Factile manifest version must be the integer 2."},
		{name: "unsupported version", content: "version = 1\n[workspace]\nroot = \"docs\"\n", want: "Factile manifest version must be the integer 2."},
		{name: "unknown top-level field", content: "version = 2\nextra = true\n[workspace]\nroot = \"docs\"\n", want: `Unsupported factile.toml field "extra".`},
		{name: "unknown workspace field", content: "version = 2\n[workspace]\nroot = \"docs\"\nextra = true\n", want: `Unsupported factile.toml field "workspace.extra".`},
		{name: "unknown bundle field", content: "version = 2\n[bundle]\nname = \"docs\"\nextra = true\n", want: `Unsupported factile.toml field "bundle.extra".`},
		{name: "wrong root type", content: "version = 2\n[workspace]\nroot = 42\n", want: "Workspace root must be a relative directory string."},
		{name: "missing root", content: "version = 2\n[workspace]\n", want: "Workspace root must be a relative directory string."},
		{name: "wrong bundle type", content: "version = 2\n[bundle]\nname = []\n", want: "Bundle name must be a non-empty string."},
		{name: "blank bundle name", content: "version = 2\n[bundle]\nname = \"   \"\n", want: "Bundle name must be a non-empty string."},
		{name: "wrong bundle metadata type", content: "version = 2\n[bundle]\nname = \"docs\"\ntitle = true\n", want: "Bundle title must be a string."},
		{name: "wrong default type", content: "version = 2\n[bundle]\nname = \"docs\"\n[defaults]\nformat = 2\n", want: "Bundle default format must be a string."},
		{name: "defaults without bundle", content: "version = 2\n[workspace]\nroot = \"docs\"\n[defaults]\nformat = \"okf\"\n", want: "Bundle defaults require a bundle table."},
		{name: "combined non-standalone", content: "version = 2\n[workspace]\nroot = \"docs\"\n[bundle]\nname = \"project\"\n", want: `A combined workspace and bundle manifest requires workspace.root = ".".`},
		{name: "no role", content: "version = 2\n", want: "Factile manifest must contain a workspace or bundle table."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, ManifestPath(dir), tc.content)
			_, err := LoadManifest(dir)
			if err == nil || (err.Error() != tc.want && !strings.HasPrefix(err.Error(), tc.want)) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWorkspaceRootPathGrammar(t *testing.T) {
	for _, root := range []string{".", "docs", "knowledge/reference", "docs with spaces"} {
		if !ValidWorkspaceRoot(root) {
			t.Errorf("valid root %q rejected", root)
		}
	}
	for _, root := range []string{"", "..", "../outside", "./docs", "docs/.", "docs/../outside", "docs//reference", "/docs", `docs\reference`, "C:/docs", ".factile", ".FACTILE/cache", ".git/docs"} {
		if ValidWorkspaceRoot(root) {
			t.Errorf("invalid root %q accepted", root)
		}
	}
}

func TestResolveWorkspaceIsInvariantAcrossContainedDirectories(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	rootBundle := filepath.Join(workspace, "docs")
	secondary := filepath.Join(workspace, "bundles", "notes")
	writeWorkspaceManifest(t, workspace, "docs")
	writeBundleManifest(t, rootBundle, "project-docs")
	writeBundleManifest(t, secondary, "notes")
	mustMkdir(t, filepath.Join(rootBundle, "guides", "deep"))
	mustMkdir(t, filepath.Join(secondary, "section", "deep"))

	want := WorkspaceContext{
		WorkspaceDir:  workspace,
		RootBundleDir: rootBundle,
		StateDir:      filepath.Join(workspace, StateDirname),
	}
	for _, workDir := range []string{
		workspace,
		rootBundle,
		filepath.Join(rootBundle, "guides", "deep"),
		secondary,
		filepath.Join(secondary, "section", "deep"),
	} {
		t.Run(strings.TrimPrefix(workDir, workspace), func(t *testing.T) {
			got, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workDir})
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("context = %#v, want %#v", got, want)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(workspace, StateDirname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only resolution created state: %v", err)
	}
}

func TestResolveWorkspaceUsesNearestBoundaryAcrossGitRepositories(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "outer")
	writeWorkspaceManifest(t, outer, "docs")
	writeBundleManifest(t, filepath.Join(outer, "docs"), "outer-docs")

	repository := filepath.Join(outer, "component")
	mustWrite(t, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustMkdir(t, filepath.Join(repository, "src", "deep"))
	outerContext, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: filepath.Join(repository, "src", "deep")})
	if err != nil {
		t.Fatal(err)
	}
	if outerContext.WorkspaceDir != outer {
		t.Fatalf("Git boundary changed workspace: %#v", outerContext)
	}

	inner := filepath.Join(repository, "nested")
	writeWorkspaceManifest(t, inner, "knowledge")
	writeBundleManifest(t, filepath.Join(inner, "knowledge"), "inner-knowledge")
	mustMkdir(t, filepath.Join(inner, "knowledge", "section"))
	innerContext, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: filepath.Join(inner, "knowledge", "section")})
	if err != nil {
		t.Fatal(err)
	}
	if innerContext.WorkspaceDir != inner || innerContext.RootBundleDir != filepath.Join(inner, "knowledge") {
		t.Fatalf("nearest nested workspace not selected: %#v", innerContext)
	}
}

func TestResolveWorkspaceExplicitSelectionIsExact(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	rootBundle := filepath.Join(workspace, "docs")
	writeWorkspaceManifest(t, workspace, "docs")
	writeBundleManifest(t, rootBundle, "project-docs")
	elsewhere := filepath.Join(root, "elsewhere")
	mustMkdir(t, elsewhere)

	context, err := ResolveWorkspace(ResolveWorkspaceOptions{Workspace: workspace, WorkDir: elsewhere})
	if err != nil {
		t.Fatal(err)
	}
	if context.WorkspaceDir != workspace || context.RootBundleDir != rootBundle {
		t.Fatalf("unexpected explicit context: %#v", context)
	}

	assertResolveError(t, ResolveWorkspaceOptions{Workspace: rootBundle}, ErrInvalidWorkspace, "Selected directory is not a Factile workspace.")
	assertResolveError(t, ResolveWorkspaceOptions{Workspace: filepath.Join(workspace, "missing")}, ErrInvalidWorkspace, "Selected directory is not a Factile workspace.")

	child := filepath.Join(workspace, "child")
	mustMkdir(t, child)
	assertResolveError(t, ResolveWorkspaceOptions{Workspace: child}, ErrInvalidWorkspace, "Selected directory is not a Factile workspace.")
}

func TestResolveWorkspaceHasNoImplicitFallbacks(t *testing.T) {
	t.Run("bundle only", func(t *testing.T) {
		dir := t.TempDir()
		writeBundleManifest(t, dir, "detached")
		child := filepath.Join(dir, "section")
		mustMkdir(t, child)
		assertResolveError(t, ResolveWorkspaceOptions{WorkDir: child}, ErrNoActiveWorkspace, "No active Factile workspace.")
	})

	t.Run("nearby docs", func(t *testing.T) {
		project := t.TempDir()
		writeBundleManifest(t, filepath.Join(project, "docs"), "docs-only")
		child := filepath.Join(project, "src")
		mustMkdir(t, child)
		assertResolveError(t, ResolveWorkspaceOptions{WorkDir: child}, ErrNoActiveWorkspace, "No active Factile workspace.")
	})

	t.Run("legacy marker", func(t *testing.T) {
		project := t.TempDir()
		legacyPath := filepath.Join(project, "docs", ".factile", "config.toml")
		mustWrite(t, legacyPath, "version = 1\n")
		child := filepath.Join(project, "src")
		mustMkdir(t, child)
		err := assertResolveError(t, ResolveWorkspaceOptions{WorkDir: child}, ErrNoActiveWorkspace, "No active Factile workspace.")
		if err.Details["legacy_path"] != legacyPath {
			t.Fatalf("legacy path = %q, want %q", err.Details["legacy_path"], legacyPath)
		}
		wantMigration := "Create " + ManifestPath(project) + " and " + ManifestPath(filepath.Join(project, "docs")) + "."
		if err.Details["migration"] != wantMigration {
			t.Fatalf("migration = %q, want %q", err.Details["migration"], wantMigration)
		}
	})

	t.Run("Git root", func(t *testing.T) {
		repository := t.TempDir()
		mustWrite(t, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/main\n")
		child := filepath.Join(repository, "src")
		mustMkdir(t, child)
		assertResolveError(t, ResolveWorkspaceOptions{WorkDir: child}, ErrNoActiveWorkspace, "No active Factile workspace.")
	})
}

func TestResolveWorkspaceRejectsMalformedCandidatesDeterministically(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		message  string
	}{
		{name: "invalid TOML", manifest: "version = \"unterminated\n", message: "Workspace factile.toml is not valid TOML."},
		{name: "unknown field", manifest: "version = 2\nextra = true\n[workspace]\nroot = \"docs\"\n", message: `Unsupported factile.toml field "extra".`},
		{name: "invalid root type", manifest: "version = 2\n[workspace]\nroot = 42\n", message: "Workspace root must be a relative directory string."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			mustWrite(t, ManifestPath(workspace), tc.manifest)
			child := filepath.Join(workspace, "child")
			mustMkdir(t, child)
			err := assertResolveError(t, ResolveWorkspaceOptions{WorkDir: child}, ErrInvalidWorkspace, tc.message)
			if err.Details["manifest"] != ManifestPath(workspace) {
				t.Fatalf("manifest detail = %q, want %q", err.Details["manifest"], ManifestPath(workspace))
			}
		})
	}
}

func TestResolveWorkspaceValidatesRootBundleAndContainment(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		setup   func(t *testing.T, workspace string)
		code    string
		message string
	}{
		{name: "parent escape", root: "../outside", code: ErrInvalidWorkspace, message: "Workspace root must remain inside the workspace."},
		{name: "absolute", root: "/outside", code: ErrInvalidWorkspace, message: "Workspace root must remain inside the workspace."},
		{name: "non-normalized", root: "docs/../outside", code: ErrInvalidWorkspace, message: "Workspace root must remain inside the workspace."},
		{name: "Windows absolute", root: "C:/outside", code: ErrInvalidWorkspace, message: "Workspace root must remain inside the workspace."},
		{name: "missing directory", root: "docs", code: ErrInvalidBundle, message: "Workspace root bundle has no valid factile.toml."},
		{
			name: "missing bundle manifest",
			root: "docs",
			setup: func(t *testing.T, workspace string) {
				mustMkdir(t, filepath.Join(workspace, "docs"))
			},
			code: ErrInvalidBundle, message: "Workspace root bundle has no valid factile.toml.",
		},
		{
			name: "invalid bundle manifest",
			root: "docs",
			setup: func(t *testing.T, workspace string) {
				mustWrite(t, ManifestPath(filepath.Join(workspace, "docs")), "version = 2\n[bundle]\nname = []\n")
			},
			code: ErrInvalidBundle, message: "Bundle name must be a non-empty string.",
		},
		{
			name: "malformed bundle TOML",
			root: "docs",
			setup: func(t *testing.T, workspace string) {
				mustWrite(t, ManifestPath(filepath.Join(workspace, "docs")), "version = \"unterminated\n")
			},
			code: ErrInvalidBundle, message: "Root bundle factile.toml is not valid TOML.",
		},
		{
			name: "unknown bundle field",
			root: "docs",
			setup: func(t *testing.T, workspace string) {
				mustWrite(t, ManifestPath(filepath.Join(workspace, "docs")), "version = 2\n[bundle]\nname = \"docs\"\nextra = true\n")
			},
			code: ErrInvalidBundle, message: `Unsupported factile.toml field "bundle.extra".`,
		},
		{
			name: "root is another workspace only",
			root: "docs",
			setup: func(t *testing.T, workspace string) {
				writeWorkspaceManifest(t, filepath.Join(workspace, "docs"), "knowledge")
			},
			code: ErrInvalidBundle, message: "Workspace root bundle has no valid factile.toml.",
		},
		{
			name: "root crosses nested workspace",
			root: "nested/docs",
			setup: func(t *testing.T, workspace string) {
				writeWorkspaceManifest(t, filepath.Join(workspace, "nested"), "docs")
				writeBundleManifest(t, filepath.Join(workspace, "nested", "docs"), "nested-docs")
			},
			code: ErrInvalidWorkspace, message: "Workspace root must not cross another Factile workspace.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceManifest(t, workspace, tc.root)
			if tc.setup != nil {
				tc.setup(t, workspace)
			}
			assertResolveError(t, ResolveWorkspaceOptions{WorkDir: workspace}, tc.code, tc.message)
		})
	}

	t.Run("symlink escape", func(t *testing.T) {
		root := t.TempDir()
		workspace := filepath.Join(root, "workspace")
		outside := filepath.Join(root, "outside")
		writeWorkspaceManifest(t, workspace, "docs")
		writeBundleManifest(t, outside, "outside")
		if err := os.Symlink(outside, filepath.Join(workspace, "docs")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		assertResolveError(t, ResolveWorkspaceOptions{WorkDir: workspace}, ErrInvalidWorkspace, "Workspace root must remain inside the workspace.")
	})

	t.Run("contained symlink is canonicalized", func(t *testing.T) {
		workspace := t.TempDir()
		realBundle := filepath.Join(workspace, "bundles", "docs")
		writeWorkspaceManifest(t, workspace, "docs")
		writeBundleManifest(t, realBundle, "docs")
		if err := os.Symlink(realBundle, filepath.Join(workspace, "docs")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if context.RootBundleDir != realBundle {
			t.Fatalf("root bundle = %q, want canonical %q", context.RootBundleDir, realBundle)
		}
	})

	t.Run("contained symlink cannot enter state", func(t *testing.T) {
		workspace := t.TempDir()
		privateBundle := filepath.Join(workspace, StateDirname, "docs")
		writeWorkspaceManifest(t, workspace, "docs")
		writeBundleManifest(t, privateBundle, "private")
		if err := os.Symlink(privateBundle, filepath.Join(workspace, "docs")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		assertResolveError(t, ResolveWorkspaceOptions{WorkDir: workspace}, ErrInvalidWorkspace, "Workspace root must not use private .factile or .git directories.")
	})
}

func TestResolveWorkspaceCombinedStandaloneAndPhysicalWorkDir(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, ManifestPath(workspace), `version = 2

[workspace]
root = "."

[bundle]
name = "standalone"
`)
	realChild := filepath.Join(workspace, "section")
	mustMkdir(t, realChild)
	linkParent := t.TempDir()
	linkedChild := filepath.Join(linkParent, "linked")
	if err := os.Symlink(realChild, linkedChild); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	context, err := ResolveWorkspace(ResolveWorkspaceOptions{WorkDir: linkedChild})
	if err != nil {
		t.Fatal(err)
	}
	if context.WorkspaceDir != workspace || context.RootBundleDir != workspace || context.StateDir != filepath.Join(workspace, StateDirname) {
		t.Fatalf("unexpected standalone context: %#v", context)
	}
}

func TestResolveWorkspaceRejectsMissingWorkDirAndManifestSymlink(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	assertResolveError(t, ResolveWorkspaceOptions{WorkDir: missing}, ErrInvalidWorkspace, "Working directory is not available.")

	root := t.TempDir()
	manifestTarget := filepath.Join(root, "manifest-target.toml")
	mustWrite(t, manifestTarget, "version = 2\n[workspace]\nroot = \"docs\"\n")
	workspace := filepath.Join(root, "workspace")
	mustMkdir(t, workspace)
	if err := os.Symlink(manifestTarget, ManifestPath(workspace)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	assertResolveError(t, ResolveWorkspaceOptions{WorkDir: workspace}, ErrInvalidWorkspace, "Workspace factile.toml must be a regular file.")
}

func assertResolveError(t *testing.T, opts ResolveWorkspaceOptions, code string, message string) *Error {
	t.Helper()
	_, err := ResolveWorkspace(opts)
	var layoutErr *Error
	if !errors.As(err, &layoutErr) {
		t.Fatalf("error = %v, want vfs Error", err)
	}
	if layoutErr.Code != code || layoutErr.Message != message {
		t.Fatalf("error = %#v, want code=%q message=%q", layoutErr, code, message)
	}
	return layoutErr
}

func writeWorkspaceManifest(t *testing.T, dir string, root string) {
	t.Helper()
	mustWrite(t, ManifestPath(dir), "version = 2\n\n[workspace]\nroot = \""+root+"\"\n")
}

func writeBundleManifest(t *testing.T, dir string, name string) {
	t.Helper()
	mustWrite(t, ManifestPath(dir), "version = 2\n\n[bundle]\nname = \""+name+"\"\n")
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
