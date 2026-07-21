package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/skill"
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
	if !result.Health.OK || len(result.Health.Checks) != 5 {
		t.Fatalf("fresh init health failed: %#v", result.Health)
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
	resolved, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: filepath.Join(workspace, "docs")})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkspaceDir != workspace || resolved.RootBundleDir != filepath.Join(workspace, "docs") {
		t.Fatalf("unexpected resolved workspace: %#v", resolved)
	}
}

func TestPrepareBuildsCompletePlanWithoutMutation(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Human Project")
	if err := os.MkdirAll(filepath.Join(workspace, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, workspace)

	plan, err := Prepare(Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.NewWorkspace || plan.RootChanged || plan.WorkspacePath != "." || plan.RootBundlePath != "docs" {
		t.Fatalf("unexpected plan boundary: %#v", plan)
	}
	if plan.Bundle.Name != "human-project" || plan.Bundle.Title != "Human Project" || plan.Bundle.Description != "Documentation and knowledge for Human Project." {
		t.Fatalf("unexpected planned metadata: %#v", plan.Bundle)
	}
	if plan.Agent == nil || plan.Agent.Agent != AgentCodex || !plan.Agent.Detected || plan.Agent.Mode != skill.ModeReader {
		t.Fatalf("unexpected planned agent action: %#v", plan.Agent)
	}
	if after := snapshotTree(t, workspace); after != before {
		t.Fatalf("planning changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	result, err := Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if initAction(result.Files, "factile.toml") != "created" || len(result.Agents) != 1 {
		t.Fatalf("planned init was not applied: %#v", result)
	}
}

func TestPrepareRejectsUnsafeAgentOutputsWithoutMutation(t *testing.T) {
	t.Run("skill output is a directory", func(t *testing.T) {
		workspace := t.TempDir()
		skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}

		_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentCodex})
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
	})

	t.Run("skill output is a symlink", func(t *testing.T) {
		workspace := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.md")
		writeBootstrapTestFile(t, outside, "preserve\n")
		skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, skillPath); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentCodex})
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
		if got := readBootstrapTestFile(t, outside); got != "preserve\n" {
			t.Fatalf("init wrote through agent symlink: %q", got)
		}
	})

	t.Run("agent parent is a symlink", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(workspace, ".agents")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentCodex})
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
		if _, err := os.Stat(filepath.Join(outside, "skills")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("init wrote through agent parent symlink: %v", err)
		}
	})
}

func TestApplyRechecksAgentOutputsBeforeMutation(t *testing.T) {
	workspace := t.TempDir()
	plan, err := Prepare(Options{WorkDir: workspace, Agent: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = Apply(context.Background(), plan)
	assertInvalidWorkspace(t, err)
	assertInitDidNotStart(t, workspace)
}

func TestApplyRevalidatesLayoutBeforeMutation(t *testing.T) {
	t.Run("missing root replaced by external symlink", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		sentinel := filepath.Join(outside, "sentinel")
		writeBootstrapTestFile(t, sentinel, "preserve\n")
		plan, err := Prepare(Options{WorkDir: workspace, Agent: AgentNone})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(workspace, "docs")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err = Apply(context.Background(), plan)
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
		if got := readBootstrapTestFile(t, sentinel); got != "preserve\n" {
			t.Fatalf("outside sentinel changed: %q", got)
		}
	})

	t.Run("existing root replaced by external symlink", func(t *testing.T) {
		workspace := t.TempDir()
		if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
			t.Fatal(err)
		}
		plan, err := Prepare(Options{WorkDir: workspace, Agent: AgentNone, Title: "Updated", TitleExplicit: true})
		if err != nil {
			t.Fatal(err)
		}
		originalRoot := filepath.Join(workspace, "original-docs")
		if err := os.Rename(filepath.Join(workspace, "docs"), originalRoot); err != nil {
			t.Fatal(err)
		}
		originalSnapshot := snapshotTree(t, originalRoot)
		workspaceManifest := readBootstrapTestFile(t, filepath.Join(workspace, "factile.toml"))
		gitignore := readBootstrapTestFile(t, filepath.Join(workspace, ".gitignore"))
		outside := t.TempDir()
		sentinel := filepath.Join(outside, "sentinel")
		writeBootstrapTestFile(t, sentinel, "preserve\n")
		if err := os.Symlink(outside, filepath.Join(workspace, "docs")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err = Apply(context.Background(), plan)
		assertInvalidWorkspace(t, err)
		if got := snapshotTree(t, originalRoot); got != originalSnapshot {
			t.Fatalf("original root changed:\nbefore:\n%s\nafter:\n%s", originalSnapshot, got)
		}
		if got := readBootstrapTestFile(t, sentinel); got != "preserve\n" {
			t.Fatalf("outside sentinel changed: %q", got)
		}
		if got := readBootstrapTestFile(t, filepath.Join(workspace, "factile.toml")); got != workspaceManifest {
			t.Fatalf("workspace manifest changed: %q", got)
		}
		if got := readBootstrapTestFile(t, filepath.Join(workspace, ".gitignore")); got != gitignore {
			t.Fatalf(".gitignore changed: %q", got)
		}
	})

	for _, kind := range []string{"symlink", "directory", "fifo"} {
		t.Run("output replaced by "+kind, func(t *testing.T) {
			workspace := t.TempDir()
			plan, err := Prepare(Options{WorkDir: workspace, Agent: AgentNone})
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(workspace, ".gitignore")
			var sentinel string
			switch kind {
			case "symlink":
				sentinel = filepath.Join(t.TempDir(), "sentinel")
				writeBootstrapTestFile(t, sentinel, "preserve\n")
				if err := os.Symlink(sentinel, output); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "directory":
				if err := os.Mkdir(output, 0o755); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(output, 0o600); err != nil {
					t.Skipf("FIFOs unavailable: %v", err)
				}
			}

			_, err = Apply(context.Background(), plan)
			assertInvalidWorkspace(t, err)
			if _, err := os.Stat(filepath.Join(workspace, "factile.toml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected apply created a workspace manifest: %v", err)
			}
			if sentinel != "" && readBootstrapTestFile(t, sentinel) != "preserve\n" {
				t.Fatal("rejected apply changed the symlink target")
			}
		})
	}

	for _, target := range []string{"workspace manifest", "root manifest"} {
		t.Run(target+" changed", func(t *testing.T) {
			workspace := t.TempDir()
			if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
				t.Fatal(err)
			}
			plan, err := Prepare(Options{WorkDir: workspace, Agent: AgentNone})
			if err != nil {
				t.Fatal(err)
			}
			if target == "workspace manifest" {
				writeBootstrapTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"other\"\n")
			} else {
				manifest := readBootstrapTestFile(t, filepath.Join(workspace, "docs", "factile.toml"))
				writeBootstrapTestFile(t, filepath.Join(workspace, "docs", "factile.toml"), manifest+"when_to_use = \"Changed after planning.\"\n")
			}
			before := snapshotTree(t, workspace)

			_, err = Apply(context.Background(), plan)
			assertInvalidWorkspace(t, err)
			if after := snapshotTree(t, workspace); after != before {
				t.Fatalf("rejected apply changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}

	t.Run("nested workspace introduced", func(t *testing.T) {
		workspace := t.TempDir()
		plan, err := Prepare(Options{WorkDir: workspace, Root: "knowledge/docs", RootExplicit: true, Agent: AgentNone})
		if err != nil {
			t.Fatal(err)
		}
		writeBootstrapTestFile(t, filepath.Join(workspace, "knowledge", "factile.toml"), "version = 2\n\n[workspace]\nroot = \".\"\n\n[bundle]\nname = \"nested\"\n")
		before := snapshotTree(t, workspace)

		_, err = Apply(context.Background(), plan)
		assertInvalidWorkspace(t, err)
		if after := snapshotTree(t, workspace); after != before {
			t.Fatalf("rejected apply changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})
}

func TestInitPublicationFailuresAreRestartSafe(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	bundle := vfs.BundleConfig{Name: "project", Title: "Project", Description: "Documentation and knowledge for Project."}
	want := map[string]string{
		".gitignore":        "/.factile/\n",
		"factile.toml":      string(formatManifest(vfs.Manifest{Version: 2, Workspace: &vfs.WorkspaceConfig{Root: "docs"}})),
		"docs/factile.toml": string(formatManifest(vfs.Manifest{Version: 2, Bundle: &bundle})),
		"docs/index.md":     indexMarkdown(bundle),
		"docs/overview.md":  overviewMarkdown(bundle, now),
	}
	for failAt := 1; failAt <= len(want); failAt++ {
		t.Run(fmt.Sprintf("fresh publication %d", failAt), func(t *testing.T) {
			workspace := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(workspace, 0o755); err != nil {
				t.Fatal(err)
			}
			restore := failInitPublication(failAt)
			_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone, Now: now})
			restore()
			if err == nil {
				t.Fatalf("publication %d did not fail", failAt)
			}
			assertCompleteBootstrapFiles(t, workspace, want)
			assertNoBootstrapTemps(t, workspace)

			result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone, Now: now})
			if err != nil || !result.Health.OK {
				t.Fatalf("rerun did not converge: result=%#v error=%v", result, err)
			}
			assertCompleteBootstrapFiles(t, workspace, want)
			result, err = Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone, Now: now})
			if err != nil {
				t.Fatal(err)
			}
			for _, change := range result.Files {
				if change.Action != "unchanged" {
					t.Fatalf("healthy rerun changed %s: %#v", change.Path, result.Files)
				}
			}
		})
	}
}

func TestInitAtomicReplacementPreservesExistingFiles(t *testing.T) {
	t.Run("gitignore", func(t *testing.T) {
		workspace := t.TempDir()
		filename := filepath.Join(workspace, ".gitignore")
		writeBootstrapTestFile(t, filename, "vendor/\n")
		if err := os.Chmod(filename, 0o600); err != nil {
			t.Fatal(err)
		}
		restore := failInitPublication(1)
		_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
		restore()
		if err == nil {
			t.Fatal(".gitignore replacement did not fail")
		}
		if got := readBootstrapTestFile(t, filename); got != "vendor/\n" {
			t.Fatalf("failed replacement changed .gitignore: %q", got)
		}
		info, err := os.Stat(filename)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("failed replacement changed .gitignore mode: %v, %v", info, err)
		}
		assertNoBootstrapTemps(t, workspace)
		result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
		if err != nil || !result.Health.OK {
			t.Fatalf(".gitignore rerun did not converge: %#v, %v", result, err)
		}
		info, err = os.Stat(filename)
		if err != nil || info.Mode().Perm() != 0o600 || readBootstrapTestFile(t, filename) != "vendor/\n/.factile/\n" {
			t.Fatalf("successful replacement lost content or mode: %v, %v", info, err)
		}
	})

	t.Run("bundle manifest and authored markdown", func(t *testing.T) {
		workspace := t.TempDir()
		if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
			t.Fatal(err)
		}
		manifest := filepath.Join(workspace, "docs", "factile.toml")
		beforeManifest := readBootstrapTestFile(t, manifest)
		authored := filepath.Join(workspace, "docs", "overview.md")
		authoredContent := "---\ntype: Reference\ntitle: Authored Overview\ndescription: Preserved authored content.\n---\n\n# Authored\n"
		writeBootstrapTestFile(t, authored, authoredContent)
		restore := failInitPublication(1)
		_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone, Title: "Updated", TitleExplicit: true})
		restore()
		if err == nil {
			t.Fatal("bundle manifest replacement did not fail")
		}
		if got := readBootstrapTestFile(t, manifest); got != beforeManifest {
			t.Fatalf("failed replacement changed bundle manifest: %q", got)
		}
		if got := readBootstrapTestFile(t, authored); got != authoredContent {
			t.Fatalf("failed replacement changed authored Markdown: %q", got)
		}
		assertNoBootstrapTemps(t, workspace)
		result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone, Title: "Updated", TitleExplicit: true})
		if err != nil || !result.Health.OK || result.Bundle.Title != "Updated" {
			t.Fatalf("bundle rerun did not converge: %#v, %v", result, err)
		}
		if got := readBootstrapTestFile(t, authored); got != authoredContent {
			t.Fatalf("successful rerun changed authored Markdown: %q", got)
		}
	})

	t.Run("root change", func(t *testing.T) {
		workspace := t.TempDir()
		if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
			t.Fatal(err)
		}
		oldRoot := filepath.Join(workspace, "docs")
		oldRootBefore := snapshotTree(t, oldRoot)
		workspaceManifest := readBootstrapTestFile(t, filepath.Join(workspace, "factile.toml"))
		restore := failInitPublication(1)
		_, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/new", RootExplicit: true, Agent: AgentNone})
		restore()
		if err == nil {
			t.Fatal("workspace manifest replacement did not fail")
		}
		if got := readBootstrapTestFile(t, filepath.Join(workspace, "factile.toml")); got != workspaceManifest {
			t.Fatalf("failed root change replaced workspace manifest: %q", got)
		}
		if got := snapshotTree(t, oldRoot); got != oldRootBefore {
			t.Fatalf("failed root change modified old root:\nbefore:\n%s\nafter:\n%s", oldRootBefore, got)
		}
		result, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/new", RootExplicit: true, Agent: AgentNone})
		if err != nil || !result.Health.OK {
			t.Fatalf("root-change rerun did not converge: %#v, %v", result, err)
		}
		if got := snapshotTree(t, oldRoot); got != oldRootBefore {
			t.Fatalf("successful root change modified old root:\nbefore:\n%s\nafter:\n%s", oldRootBefore, got)
		}
	})
}

func TestInitPreflightsAllAgentInputsBeforeBundleMutation(t *testing.T) {
	for _, input := range []string{"AGENTS.md", filepath.Join(".codex", "config.toml")} {
		t.Run("unreadable "+filepath.ToSlash(input), func(t *testing.T) {
			workspace := t.TempDir()
			filename := filepath.Join(workspace, input)
			writeBootstrapTestFile(t, filename, "unreadable\n")
			if err := os.Chmod(filename, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filename, 0o600) })
			_, err := Init(context.Background(), Options{WorkDir: workspace})
			assertInvalidWorkspace(t, err)
			assertInitDidNotStart(t, workspace)
		})
	}

	t.Run("non-removable retired helper", func(t *testing.T) {
		workspace := t.TempDir()
		scripts := filepath.Join(workspace, ".agents", "skills", "factile", "scripts")
		legacy := filepath.Join(scripts, "factile-discover.sh")
		writeBootstrapTestFile(t, legacy, "#!/bin/sh\n")
		if err := os.Chmod(scripts, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) })
		_, err := Init(context.Background(), Options{WorkDir: workspace})
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
		if got := readBootstrapTestFile(t, legacy); got != "#!/bin/sh\n" {
			t.Fatalf("preflight changed retired helper: %q", got)
		}
	})

	t.Run("agent content changed after Prepare", func(t *testing.T) {
		workspace := t.TempDir()
		plan, err := Prepare(Options{WorkDir: workspace, Agent: AgentCodex})
		if err != nil {
			t.Fatal(err)
		}
		writeBootstrapTestFile(t, filepath.Join(workspace, "AGENTS.md"), "changed after planning\n")
		before := snapshotTree(t, workspace)
		_, err = Apply(context.Background(), plan)
		assertInvalidWorkspace(t, err)
		if after := snapshotTree(t, workspace); after != before {
			t.Fatalf("stale agent plan changed workspace:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})
}

func TestInitDerivesUsefulMetadataAndStarterDocuments(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "My Project__CLI")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Now: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	bundle, err := vfs.LoadManifest(filepath.Join(workspace, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	wantDescription := "Documentation and knowledge for My Project Cli."
	if bundle.Bundle == nil || bundle.Bundle.Name != "my-project-cli" || bundle.Bundle.Title != "My Project Cli" || bundle.Bundle.Description != wantDescription {
		t.Fatalf("unexpected derived metadata: %#v", bundle.Bundle)
	}
	index := readBootstrapTestFile(t, filepath.Join(workspace, "docs", "index.md"))
	overview := readBootstrapTestFile(t, filepath.Join(workspace, "docs", "overview.md"))
	for _, want := range []string{"title: \"My Project Cli Knowledge\"", "description: \"" + wantDescription + "\"", "# My Project Cli Knowledge"} {
		if !strings.Contains(index, want) {
			t.Fatalf("derived index missing %q:\n%s", want, index)
		}
	}
	for _, want := range []string{"title: \"My Project Cli Overview\"", "description: \"" + wantDescription + "\"", "# My Project Cli Overview", wantDescription} {
		if !strings.Contains(overview, want) {
			t.Fatalf("derived overview missing %q:\n%s", want, overview)
		}
	}
}

func TestInitAppliesExplicitMetadataToSplitAndCombinedBundles(t *testing.T) {
	for _, root := range []string{"docs", "."} {
		t.Run(root, func(t *testing.T) {
			workspace := t.TempDir()
			result, err := Init(context.Background(), Options{
				WorkDir:             workspace,
				Root:                root,
				RootExplicit:        true,
				Name:                "platform-handbook",
				NameExplicit:        true,
				Title:               `Platform "Handbook"`,
				TitleExplicit:       true,
				Description:         `Trusted platform documentation with a \\ path.`,
				DescriptionExplicit: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.RootBundlePath != root {
				t.Fatalf("unexpected root: %#v", result)
			}
			bundleDir := workspace
			if root != "." {
				bundleDir = filepath.Join(workspace, root)
			}
			manifest, err := vfs.LoadManifest(bundleDir)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Bundle == nil || manifest.Bundle.Name != "platform-handbook" || manifest.Bundle.Title != `Platform "Handbook"` || manifest.Bundle.Description != `Trusted platform documentation with a \\ path.` {
				t.Fatalf("explicit metadata was not preserved: %#v", manifest.Bundle)
			}
			if _, err := okf.ParseConcept("", []byte(readBootstrapTestFile(t, filepath.Join(bundleDir, "index.md")))); err != nil {
				t.Fatalf("explicit metadata produced invalid starter Markdown: %v", err)
			}
		})
	}
}

func TestInitFillsAndUpdatesMetadataWhilePreservingBundleConfiguration(t *testing.T) {
	workspace := t.TempDir()
	writeBootstrapTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"knowledge\"\n")
	writeBootstrapTestFile(t, filepath.Join(workspace, "knowledge", "factile.toml"), `version = 2

[bundle]
name = "project-guide"
when_to_use = "Use for project decisions."

[defaults]
format = "okf"
`)
	first, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if initAction(first.Files, "knowledge/factile.toml") != "updated" {
		t.Fatalf("missing metadata was not filled: %#v", first.Files)
	}
	manifest, err := vfs.LoadManifest(filepath.Join(workspace, "knowledge"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Bundle == nil || manifest.Bundle.Name != "project-guide" || manifest.Bundle.Title != "Project Guide" || manifest.Bundle.Description != "Documentation and knowledge for Project Guide." || manifest.Bundle.WhenToUse != "Use for project decisions." || manifest.Defaults == nil || manifest.Defaults.Format != "okf" {
		t.Fatalf("metadata fill lost bundle configuration: %#v", manifest)
	}

	indexPath := filepath.Join(workspace, "knowledge", "index.md")
	overviewPath := filepath.Join(workspace, "knowledge", "overview.md")
	indexBefore := readBootstrapTestFile(t, indexPath)
	overviewBefore := readBootstrapTestFile(t, overviewPath)
	second, err := Init(context.Background(), Options{
		WorkDir:             workspace,
		Title:               "Project Authority",
		TitleExplicit:       true,
		Description:         "Authoritative project decisions and operating guidance.",
		DescriptionExplicit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if initAction(second.Files, "knowledge/factile.toml") != "updated" {
		t.Fatalf("explicit metadata update was not reported: %#v", second.Files)
	}
	updated, err := vfs.LoadManifest(filepath.Join(workspace, "knowledge"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Bundle == nil || updated.Bundle.Name != "project-guide" || updated.Bundle.Title != "Project Authority" || updated.Bundle.Description != "Authoritative project decisions and operating guidance." || updated.Bundle.WhenToUse != manifest.Bundle.WhenToUse || updated.Defaults == nil || updated.Defaults.Format != "okf" {
		t.Fatalf("metadata update lost preserved fields: %#v", updated)
	}
	if got := readBootstrapTestFile(t, indexPath); got != indexBefore {
		t.Fatalf("metadata update rewrote authored index:\nbefore:\n%s\nafter:\n%s", indexBefore, got)
	}
	if got := readBootstrapTestFile(t, overviewPath); got != overviewBefore {
		t.Fatalf("metadata update rewrote authored overview:\nbefore:\n%s\nafter:\n%s", overviewBefore, got)
	}

	third, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range third.Files {
		if change.Action != "unchanged" {
			t.Fatalf("repeated metadata reconciliation changed %s: %#v", change.Path, third.Files)
		}
	}
}

func TestInitRejectsConflictingBundleNameBeforeMutation(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Name: "stable-name", NameExplicit: true}); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, workspace)
	_, err := Init(context.Background(), Options{WorkDir: workspace, Name: "different-name", NameExplicit: true, Title: "Should Not Apply", TitleExplicit: true})
	assertInvalidWorkspace(t, err)
	if after := snapshotTree(t, workspace); after != before {
		t.Fatalf("conflicting name changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInitRejectsInvalidExplicitMetadataBeforeMutation(t *testing.T) {
	tests := []Options{
		{Name: "   ", NameExplicit: true},
		{Title: "", TitleExplicit: true},
		{Description: "two\nlines", DescriptionExplicit: true},
	}
	for _, opts := range tests {
		workspace := t.TempDir()
		opts.WorkDir = workspace
		_, err := Init(context.Background(), opts)
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
	}
}

func TestInitRepairsMissingStarterWithoutOverwritingAuthoredMarkdown(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Title: "Repository Guide", TitleExplicit: true, Description: "Repository-specific guidance.", DescriptionExplicit: true}); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(workspace, "docs", "index.md")
	overviewPath := filepath.Join(workspace, "docs", "overview.md")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	writeBootstrapTestFile(t, overviewPath, "# Authored overview\n\nKeep this exactly.\n")

	result, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if initAction(result.Files, "docs/index.md") != "created" || initAction(result.Files, "docs/overview.md") != "unchanged" {
		t.Fatalf("starter repair actions are wrong: %#v", result.Files)
	}
	if index := readBootstrapTestFile(t, indexPath); !strings.Contains(index, "# Repository Guide Knowledge") {
		t.Fatalf("repaired index did not use manifest metadata:\n%s", index)
	}
	if overview := readBootstrapTestFile(t, overviewPath); overview != "# Authored overview\n\nKeep this exactly.\n" {
		t.Fatalf("authored overview was overwritten:\n%s", overview)
	}
}

func TestInitRootChangeLeavesOldRootByteForByteUntouched(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Name: "original", NameExplicit: true, Title: "Original", TitleExplicit: true, Description: "Original knowledge.", DescriptionExplicit: true}); err != nil {
		t.Fatal(err)
	}
	writeBootstrapTestFile(t, filepath.Join(workspace, "docs", "authored.md"), "# Authored\n")
	oldRootBefore := snapshotTree(t, filepath.Join(workspace, "docs"))

	result, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/new", RootExplicit: true, Name: "replacement", NameExplicit: true, Title: "Replacement", TitleExplicit: true, Description: "Replacement knowledge.", DescriptionExplicit: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.RootBundlePath != "knowledge/new" || initAction(result.Files, "factile.toml") != "updated" || initAction(result.Files, "knowledge/new/factile.toml") != "created" {
		t.Fatalf("root change result is wrong: %#v", result)
	}
	workspaceManifest, err := vfs.LoadManifest(workspace)
	if err != nil || workspaceManifest.Workspace == nil || workspaceManifest.Workspace.Root != "knowledge/new" {
		t.Fatalf("workspace selector was not updated: %#v, %v", workspaceManifest, err)
	}
	if oldRootAfter := snapshotTree(t, filepath.Join(workspace, "docs")); oldRootAfter != oldRootBefore {
		t.Fatalf("root change modified the old root:\nbefore:\n%s\nafter:\n%s", oldRootBefore, oldRootAfter)
	}
}

func TestInitFailedRootChangePreflightLeavesWorkspaceUntouched(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace}); err != nil {
		t.Fatal(err)
	}
	writeBootstrapTestFile(t, filepath.Join(workspace, "broken", "factile.toml"), "not toml = [\n")
	before := snapshotTree(t, workspace)
	_, err := Init(context.Background(), Options{WorkDir: workspace, Root: "broken", RootExplicit: true})
	assertInvalidWorkspace(t, err)
	if after := snapshotTree(t, workspace); after != before {
		t.Fatalf("failed root-change preflight modified the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInitAgentAutoRepairsManagedFilesAndPreservesIntent(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Install(skill.TargetCodex, skill.InstallOptions{Scope: "repo", WorkDir: workspace, Mode: skill.ModeCurator, Profile: "software"}); err != nil {
		t.Fatal(err)
	}
	docsBefore := snapshotTree(t, filepath.Join(workspace, "docs"))

	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	agentsPath := filepath.Join(workspace, "AGENTS.md")
	configPath := filepath.Join(workspace, ".codex", "config.toml")
	writeBootstrapTestFile(t, skillPath, strings.Replace(readBootstrapTestFile(t, skillPath), "# Factile local knowledge workflow", "# Drifted workflow", 1))
	writeBootstrapTestFile(t, agentsPath, strings.Replace(readBootstrapTestFile(t, agentsPath), "Mode: curator", "Mode: drifted", 1))
	writeBootstrapTestFile(t, configPath, strings.Replace(readBootstrapTestFile(t, configPath), `"mcp", "serve"`, `"mcp", "drifted"`, 1))
	legacyScript := filepath.Join(workspace, ".agents", "skills", "factile", "scripts", "factile-discover.sh")
	writeBootstrapTestFile(t, legacyScript, "#!/bin/sh\n")

	userHome := t.TempDir()
	t.Setenv("CODEX_HOME", userHome)
	userSkill := filepath.Join(userHome, "skills", "factile", "SKILL.md")
	writeBootstrapTestFile(t, userSkill, "user-scope sentinel\n")

	result, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Agents) != 1 || !result.Agents[0].Detected || result.Agents[0].Mode != skill.ModeCurator || result.Agents[0].Profile != "software" {
		t.Fatalf("init did not preserve managed agent intent: %#v", result.Agents)
	}
	if !hasSkillChange(result.Agents[0].Files, ".agents/skills/factile/SKILL.md", "updated") || !hasSkillChange(result.Agents[0].Files, "AGENTS.md", "updated") || !hasSkillChange(result.Agents[0].Files, ".codex/config.toml", "updated") || !hasSkillChange(result.Agents[0].Files, ".agents/skills/factile/scripts/factile-discover.sh", "removed") {
		t.Fatalf("init did not repair every managed agent surface: %#v", result.Agents[0].Files)
	}
	intent := skill.InspectRepoInstall(workspace)
	if !intent.Trusted || intent.Mode != skill.ModeCurator || intent.Profile != "software" {
		t.Fatalf("repaired install lost intent: %#v", intent)
	}
	if strings.Contains(readBootstrapTestFile(t, configPath), `"--read-only"`) {
		t.Fatalf("curator MCP was reset to reader mode:\n%s", readBootstrapTestFile(t, configPath))
	}
	if got := readBootstrapTestFile(t, userSkill); got != "user-scope sentinel\n" {
		t.Fatalf("repo reconciliation changed user scope: %q", got)
	}
	if docsAfter := snapshotTree(t, filepath.Join(workspace, "docs")); docsAfter != docsBefore {
		t.Fatalf("agent repair changed bundle content:\nbefore:\n%s\nafter:\n%s", docsBefore, docsAfter)
	}

	second, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range second.Files {
		if change.Action != "unchanged" {
			t.Fatalf("current install changed bundle file %s: %#v", change.Path, second.Files)
		}
	}
	if len(second.Agents) != 1 || second.Agents[0].Mode != skill.ModeCurator || second.Agents[0].Profile != "software" {
		t.Fatalf("current install lost intent: %#v", second.Agents)
	}
	for _, change := range second.Agents[0].Files {
		if change.Action != "unchanged" {
			t.Fatalf("current agent install changed %s: %#v", change.Path, second.Agents[0].Files)
		}
	}
}

func TestInitAgentNoneLeavesAllAgentScopesUntouched(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
		t.Fatal(err)
	}
	repoFiles := []string{
		filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md"),
		filepath.Join(workspace, ".agents", "skills", "factile", "scripts", "factile-discover.sh"),
		filepath.Join(workspace, "AGENTS.md"),
		filepath.Join(workspace, ".codex", "config.toml"),
	}
	for i, filename := range repoFiles {
		writeBootstrapTestFile(t, filename, fmt.Sprintf("repo sentinel %d\n", i))
	}
	userHome := t.TempDir()
	t.Setenv("CODEX_HOME", userHome)
	userSkill := filepath.Join(userHome, "skills", "factile", "SKILL.md")
	writeBootstrapTestFile(t, userSkill, "user sentinel\n")
	before := make(map[string]string, len(repoFiles)+1)
	for _, filename := range append(repoFiles, userSkill) {
		before[filename] = readBootstrapTestFile(t, filename)
	}

	result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone, Title: "Updated Title", TitleExplicit: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Agents) != 0 {
		t.Fatalf("agent none returned installs: %#v", result.Agents)
	}
	if !result.Health.OK || result.Bundle.Title != "Updated Title" {
		t.Fatalf("agent none did not reconcile the bundle cleanly: %#v", result)
	}
	for filename, want := range before {
		if got := readBootstrapTestFile(t, filename); got != want {
			t.Fatalf("agent none changed %s: got %q want %q", filename, got, want)
		}
	}
}

func TestInitRejectsUnrecognizedCanonicalSkillWithoutMutation(t *testing.T) {
	for _, agent := range []string{"", AgentCodex} {
		name := "auto"
		if agent != "" {
			name = agent
		}
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			writeBootstrapTestFile(t, filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md"), "hand-authored project skill\n")
			before := snapshotTree(t, workspace)
			_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: agent, Title: "Must Not Apply", TitleExplicit: true})
			assertInvalidWorkspace(t, err)
			if after := snapshotTree(t, workspace); after != before {
				t.Fatalf("skill collision changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestInitRejectsUnknownAgentBeforeChangingBundle(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, workspace)
	_, err := Init(context.Background(), Options{WorkDir: workspace, Agent: "unsupported", Title: "Must Not Apply", TitleExplicit: true})
	if factile.ErrorCode(err) != factile.ErrInvalidPath {
		t.Fatalf("unknown agent error = %v, want invalid_path", err)
	}
	if after := snapshotTree(t, workspace); after != before {
		t.Fatalf("unknown agent changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInitReportsInvalidAuthoredKnowledgeWithoutOverwritingIt(t *testing.T) {
	for _, filename := range []string{"index.md", "overview.md"} {
		t.Run(filename, func(t *testing.T) {
			workspace := t.TempDir()
			if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "docs", filename)
			invalid := "# Authored but invalid " + filename + "\n"
			writeBootstrapTestFile(t, path, invalid)

			result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
			if err != nil {
				t.Fatal(err)
			}
			if result.Health.OK || result.Health.Status != "failed" {
				t.Fatalf("invalid root knowledge was reported healthy: %#v", result.Health)
			}
			for _, name := range []string{"required_documents", "local_root_validation"} {
				check := initHealthCheck(result.Health.Checks, name)
				if check == nil || check.Status != CheckFail || !strings.Contains(check.Message, strings.TrimSuffix(filename, ".md")) {
					t.Fatalf("missing actionable %s failure: %#v", name, check)
				}
			}
			if got := readBootstrapTestFile(t, path); got != invalid {
				t.Fatalf("verification rewrote authored knowledge: %q", got)
			}
		})
	}
}

func TestInitRootValidationRecognizesExistingReservedLinks(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(workspace, "docs", "index.md")
	writeBootstrapTestFile(t, indexPath, readBootstrapTestFile(t, indexPath)+"\n- [Log](log.md)\n")
	logPath := filepath.Join(workspace, "docs", "log.md")
	writeBootstrapTestFile(t, logPath, "# Project log\n")

	result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Health.OK || result.Health.Status != "healthy" {
		t.Fatalf("existing reserved link was reported unhealthy: %#v", result.Health)
	}

	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	result, err = Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
	if err != nil {
		t.Fatal(err)
	}
	check := initHealthCheck(result.Health.Checks, "local_root_validation")
	if !result.Health.OK || result.Health.Status != "warning" || check == nil || check.Status != CheckWarning || !strings.Contains(check.Message, "log.md") {
		t.Fatalf("missing reserved link was not reported as a warning: %#v", result.Health)
	}
}

func TestInitReportsSkippedDriftedAgentIntegration(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentCodex}); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(workspace, "AGENTS.md")
	drifted := "drifted managed guidance\n"
	writeBootstrapTestFile(t, agentsPath, drifted)

	result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
	if err != nil {
		t.Fatal(err)
	}
	check := initHealthCheck(result.Health.Checks, "agent_integration")
	if result.Health.OK || check == nil || check.Status != CheckFail || !strings.Contains(check.Message, "drifted") {
		t.Fatalf("drifted managed integration was not failed: result=%#v check=%#v", result.Health, check)
	}
	if len(result.Agents) != 0 || readBootstrapTestFile(t, agentsPath) != drifted {
		t.Fatalf("--agent none repaired or rewrote managed guidance: %#v", result.Agents)
	}
}

func TestInitVerificationDoesNotInvokeGitOrCreateRemoteState(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone}); err != nil {
		t.Fatal(err)
	}
	writeBootstrapTestFile(t, filepath.Join(workspace, "docs", "remote.mount.toml"), `source = "https://example.test/remote.git"
writable = false
ref = "main"
`)
	writeBootstrapTestFile(t, filepath.Join(workspace, "docs", "hosted.mount.toml"), `source = "factile://public/example"
writable = false
`)
	fakeBin := t.TempDir()
	gitCalled := filepath.Join(t.TempDir(), "git-called")
	fakeGit := filepath.Join(fakeBin, "git")
	writeBootstrapTestFile(t, fakeGit, "#!/bin/sh\ntouch \""+gitCalled+"\"\nexit 99\n")
	if err := os.Chmod(fakeGit, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)
	userHome := t.TempDir()
	t.Setenv("CODEX_HOME", userHome)
	userSentinel := filepath.Join(userHome, "skills", "factile", "SKILL.md")
	writeBootstrapTestFile(t, userSentinel, "user scope sentinel\n")

	result, err := Init(context.Background(), Options{WorkDir: workspace, Agent: AgentNone})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Health.OK {
		t.Fatalf("local verification failed with an untouched Git mount: %#v", result.Health)
	}
	if _, err := os.Stat(gitCalled); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init verification invoked Git: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, vfs.StateDirname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init verification created remote cache state: %v", err)
	}
	if got := readBootstrapTestFile(t, userSentinel); got != "user scope sentinel\n" {
		t.Fatalf("init verification changed user scope: %q", got)
	}
}

func TestInitRootDotCreatesCombinedWorkspace(t *testing.T) {
	workspace := t.TempDir()
	result, err := Init(context.Background(), Options{WorkDir: workspace, Root: ".", RootExplicit: true})
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
		t.Fatalf("init --root . created docs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, vfs.StateDirname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init --root . created local state: %v", err)
	}
}

func TestInitReusesContainingWorkspaceFromNestedBundle(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: workspace}); err != nil {
		t.Fatal(err)
	}
	secondary := filepath.Join(workspace, "bundles", "secondary")
	writeBootstrapTestFile(t, filepath.Join(secondary, "factile.toml"), "version = 2\n\n[bundle]\nname = \"secondary\"\n")
	deep := filepath.Join(secondary, "src", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := Init(context.Background(), Options{WorkDir: deep})
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.Rel(deep, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspacePath != filepath.ToSlash(wantWorkspace) || result.RootBundlePath != "docs" {
		t.Fatalf("nested init selected the wrong workspace: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(deep, "factile.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nested init created a competing workspace: %v", err)
	}
	secondaryManifest, err := vfs.LoadManifest(secondary)
	if err != nil || secondaryManifest.Bundle == nil || secondaryManifest.Workspace != nil {
		t.Fatalf("secondary bundle changed: %#v, %v", secondaryManifest, err)
	}
}

func TestInitExplicitWorkspaceOverridesContainingWorkspace(t *testing.T) {
	parent := t.TempDir()
	if _, err := Init(context.Background(), Options{WorkDir: parent}); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(parent, "src", "nested")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := Init(context.Background(), Options{WorkDir: child, Workspace: "."})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspacePath != "." || result.RootBundlePath != "docs" {
		t.Fatalf("explicit workspace did not select the child: %#v", result)
	}
	resolved, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{Workspace: child})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkspaceDir != child {
		t.Fatalf("explicit child workspace was not created: %#v", resolved)
	}
}

func TestInitCreatesAndReusesNestedCustomRoot(t *testing.T) {
	workspace := t.TempDir()
	first, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/project", RootExplicit: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.RootBundlePath != "knowledge/project" || initAction(first.Files, "knowledge/project/factile.toml") != "created" {
		t.Fatalf("unexpected custom-root result: %#v", first)
	}
	second, err := Init(context.Background(), Options{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if second.RootBundlePath != "knowledge/project" {
		t.Fatalf("existing workspace root was not authoritative: %#v", second)
	}
	for _, change := range second.Files {
		if change.Action != "unchanged" {
			t.Fatalf("repeated custom-root init changed %s: %#v", change.Path, second.Files)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("custom-root init created default docs: %v", err)
	}
	resolved, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: filepath.Join(workspace, "knowledge", "project")})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkspaceDir != workspace || resolved.RootBundleDir != filepath.Join(workspace, "knowledge", "project") {
		t.Fatalf("custom root resolves a different workspace: %#v", resolved)
	}
}

func TestInitConvertsBundleOnlyTargetToCombinedWorkspace(t *testing.T) {
	target := t.TempDir()
	writeBootstrapTestFile(t, filepath.Join(target, "factile.toml"), `version = 2

[bundle]
name = "handbook"
title = "Handbook"
description = "Existing authored bundle."
when_to_use = "Use for project guidance."

[defaults]
format = "okf"
`)

	result, err := Init(context.Background(), Options{WorkDir: target})
	if err != nil {
		t.Fatal(err)
	}
	if result.RootBundlePath != "." || initAction(result.Files, "factile.toml") != "updated" {
		t.Fatalf("bundle-only target was not converted: %#v", result)
	}
	manifest, err := vfs.LoadManifest(target)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Workspace == nil || manifest.Workspace.Root != "." || manifest.Bundle == nil || manifest.Bundle.Name != "handbook" || manifest.Bundle.Description != "Existing authored bundle." || manifest.Bundle.WhenToUse != "Use for project guidance." || manifest.Defaults == nil || manifest.Defaults.Format != "okf" {
		t.Fatalf("bundle metadata was not preserved: %#v", manifest)
	}
}

func TestInitRepairsCompatiblePartialLayouts(t *testing.T) {
	t.Run("missing root bundle", func(t *testing.T) {
		workspace := t.TempDir()
		writeBootstrapTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
		result, err := Init(context.Background(), Options{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if initAction(result.Files, "factile.toml") != "unchanged" || initAction(result.Files, "docs/factile.toml") != "created" {
			t.Fatalf("workspace-only layout was not repaired: %#v", result.Files)
		}
	})

	t.Run("missing workspace manifest", func(t *testing.T) {
		workspace := t.TempDir()
		writeBootstrapTestFile(t, filepath.Join(workspace, "docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"existing\"\n")
		result, err := Init(context.Background(), Options{WorkDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if initAction(result.Files, "factile.toml") != "created" || initAction(result.Files, "docs/factile.toml") != "updated" {
			t.Fatalf("bundle-only partial layout was not repaired: %#v", result.Files)
		}
	})
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

func TestInitRejectsInvalidRootsWithoutMutation(t *testing.T) {
	for _, root := range []string{"", "../docs", "docs/../other", "/tmp/docs", "C:/docs", ".factile/docs", ".git/docs", `docs\nested`, "docs/"} {
		t.Run(strings.ReplaceAll(root, "/", "_"), func(t *testing.T) {
			workspace := t.TempDir()
			_, err := Init(context.Background(), Options{WorkDir: workspace, Root: root, RootExplicit: true})
			assertInvalidWorkspace(t, err)
			assertInitDidNotStart(t, workspace)
		})
	}
}

func TestInitRejectsUnsafeRootFilesystemPathsWithoutMutation(t *testing.T) {
	t.Run("file component", func(t *testing.T) {
		workspace := t.TempDir()
		writeBootstrapTestFile(t, filepath.Join(workspace, "knowledge"), "not a directory\n")
		_, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/docs", RootExplicit: true})
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
	})

	t.Run("symlink component", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(workspace, "knowledge")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		_, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/docs", RootExplicit: true})
		assertInvalidWorkspace(t, err)
		assertInitDidNotStart(t, workspace)
		if _, err := os.Stat(filepath.Join(outside, "docs")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rejected init wrote through root symlink: %v", err)
		}
	})

	t.Run("nested workspace component", func(t *testing.T) {
		workspace := t.TempDir()
		writeBootstrapTestFile(t, filepath.Join(workspace, "knowledge", "factile.toml"), "version = 2\n\n[workspace]\nroot = \".\"\n\n[bundle]\nname = \"nested\"\n")
		before := snapshotTree(t, workspace)
		_, err := Init(context.Background(), Options{WorkDir: workspace, Root: "knowledge/docs", RootExplicit: true, Agent: AgentNone})
		assertInvalidWorkspace(t, err)
		if after := snapshotTree(t, workspace); after != before {
			t.Fatalf("rejected init changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})
}

func TestInitRejectsMalformedLegacyAndConflictingLayoutsWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, string)
		options func(string) Options
	}{
		{
			name: "legacy",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, ".factile", "config.toml"), "version = 1\n")
			},
		},
		{
			name: "malformed workspace",
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
		{
			name: "nested workspace selected as root",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "docs", "factile.toml"), "version = 2\n\n[workspace]\nroot = \".\"\n\n[bundle]\nname = \"nested\"\n")
			},
		},
		{
			name: "bundle target with separate root",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "factile.toml"), "version = 2\n\n[bundle]\nname = \"bundle\"\n")
			},
			options: func(dir string) Options {
				return Options{WorkDir: dir, Root: "docs", RootExplicit: true}
			},
		},
		{
			name: "combined workspace with separate root",
			setup: func(t *testing.T, dir string) {
				writeBootstrapTestFile(t, filepath.Join(dir, "factile.toml"), "version = 2\n\n[workspace]\nroot = \".\"\n\n[bundle]\nname = \"combined\"\n")
			},
			options: func(dir string) Options {
				return Options{WorkDir: dir, Root: "docs", RootExplicit: true}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			tc.setup(t, workspace)
			opts := Options{WorkDir: workspace}
			if tc.options != nil {
				opts = tc.options(workspace)
			}
			before := snapshotTree(t, workspace)
			_, err := Init(context.Background(), opts)
			assertInvalidWorkspace(t, err)
			after := snapshotTree(t, workspace)
			if before != after {
				t.Fatalf("rejected init changed the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
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

func assertInvalidWorkspace(t *testing.T, err error) {
	t.Helper()
	var layoutErr *vfs.Error
	if !errors.As(err, &layoutErr) || layoutErr.Code != vfs.ErrInvalidWorkspace {
		t.Fatalf("error = %T %v, want invalid_workspace", err, err)
	}
}

func assertInitDidNotStart(t *testing.T, workspace string) {
	t.Helper()
	for _, filename := range []string{filepath.Join(workspace, ".gitignore"), filepath.Join(workspace, "factile.toml")} {
		if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rejected init created %s: %v", filename, err)
		}
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var snapshot strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			snapshot.WriteString(filepath.ToSlash(rel) + "/\n")
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot.WriteString(filepath.ToSlash(rel) + ":" + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
}

func failInitPublication(failAt int) func() {
	originalReplace := replaceInitFile
	originalCreate := createInitFile
	publication := 0
	fail := func() error {
		publication++
		if publication == failAt {
			return errors.New("injected init publication failure")
		}
		return nil
	}
	replaceInitFile = func(filename string, data []byte, mode os.FileMode) error {
		if err := fail(); err != nil {
			return err
		}
		return originalReplace(filename, data, mode)
	}
	createInitFile = func(filename string, data []byte, mode os.FileMode) (bool, error) {
		if err := fail(); err != nil {
			return false, err
		}
		return originalCreate(filename, data, mode)
	}
	return func() {
		replaceInitFile = originalReplace
		createInitFile = originalCreate
	}
}

func assertCompleteBootstrapFiles(t *testing.T, workspace string, want map[string]string) {
	t.Helper()
	for rel, expected := range want {
		filename := filepath.Join(workspace, filepath.FromSlash(rel))
		data, err := os.ReadFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != expected {
			t.Fatalf("visible output %s is incomplete:\ngot:\n%s\nwant:\n%s", rel, data, expected)
		}
	}
}

func assertNoBootstrapTemps(t *testing.T, workspace string) {
	t.Helper()
	if err := filepath.Walk(workspace, func(filename string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.Contains(info.Name(), ".factile-tmp-") {
			return fmt.Errorf("temporary file remains: %s", filename)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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

func initHealthCheck(checks []HealthCheck, name string) *HealthCheck {
	for _, check := range checks {
		if check.Name == name {
			return &check
		}
	}
	return nil
}

func hasSkillChange(files []skill.FileChange, path, action string) bool {
	for _, file := range files {
		if file.Path == path && file.Action == action {
			return true
		}
	}
	return false
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

func readBootstrapTestFile(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
