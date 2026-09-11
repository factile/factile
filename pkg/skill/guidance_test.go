package skill

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/version"
)

func useSkillVersion(t *testing.T, value string) {
	t.Helper()
	previous := version.Version
	t.Cleanup(func() { version.Version = previous })
	version.Version = value
}

func TestSkillReleaseMetadataAndChecksum(t *testing.T) {
	useSkillVersion(t, "v2.3.4-rc.1")
	for _, mode := range []string{ModeReader, ModeCurator} {
		for _, profile := range []string{"", "software"} {
			content := skillMarkdown(mode, profile)
			doc, err := okf.ParseConcept("SKILL", []byte(content))
			if err != nil {
				t.Fatal(err)
			}
			metadata := doc.Frontmatter["metadata"].(map[string]any)
			if metadata["version"] != "v2.3.4-rc.1" {
				t.Fatalf("version metadata: %#v", metadata)
			}
			checksum := metadata["factile-content-sha256"].(string)
			unsigned := strings.Replace(content, checksum, "", 1)
			if checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(unsigned))) {
				t.Fatal("checksum does not cover the complete generated file")
			}
			filename := filepath.Join(t.TempDir(), "SKILL.md")
			writeSkillTestFile(t, filename, content)
			state := inspectInstalledSkill(filename)
			if !state.Current || state.Modified || state.Mode != mode || state.Profile != profile {
				t.Fatalf("generated skill: %#v", state)
			}
		}
	}
	inspected, err := Inspect(TargetCodex)
	if err != nil || inspected.Version != version.Current().Version {
		t.Fatalf("inspection: %#v %v", inspected, err)
	}
}

func TestSkillDistinguishesReleaseDriftFromLocalEdits(t *testing.T) {
	useSkillVersion(t, "v1.0.0")
	old := skillMarkdown(ModeCurator, "software")
	version.Version = "v2.0.0"
	current := skillMarkdown(ModeCurator, "software")
	legacy := strings.Replace(current, current[strings.Index(current, "metadata:\n"):strings.Index(current, "\n---\n")], "", 1)
	missingMetadata := legacy
	legacy = strings.Replace(legacy, skillInstallMarker(ModeCurator, "software"), legacySkillInstallMarker(ModeCurator, "software"), 1)
	for _, test := range []struct{ name, content, status string }{
		{"current", current, "current"},
		{"previous release", old, "outdated"},
		{"body edit", old + "\nMy custom instructions.\n", "modified"},
		{"mode edit", strings.Replace(old, "mode=curator", "mode=reader", 1), "modified"},
		{"version edit", strings.Replace(old, `"v1.0.0"`, `"v2.0.0"`, 1), "modified"},
		{"missing checksum", strings.Replace(old, "factile-content-sha256:", "removed-checksum:", 1), "invalid"},
		{"missing metadata", missingMetadata, "invalid"},
		{"legacy", legacy, "unversioned"},
		{"hand written", "---\nname: factile\n---\nMy custom skill.\n", "unrecognized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "SKILL.md")
			writeSkillTestFile(t, filename, test.content)
			if state := inspectInstalledSkill(filename); guidanceStatus(state) != test.status {
				t.Fatalf("state = %#v; want %s", state, test.status)
			}
		})
	}
	// Two builds can share a release label but embed different guidance.
	version.Version = "v1.0.0"
	previousBase := BaseSkillMarkdown
	t.Cleanup(func() { BaseSkillMarkdown = previousBase })
	BaseSkillMarkdown += "\nNew built-in guidance.\n"
	filename := filepath.Join(t.TempDir(), "SKILL.md")
	writeSkillTestFile(t, filename, old)
	if state := inspectInstalledSkill(filename); guidanceStatus(state) != "outdated" || state.Modified {
		t.Fatalf("same-version build drift: %#v", state)
	}
}

func TestSkillRepairPreservesEditedFiles(t *testing.T) {
	for _, scope := range []string{"repo", "user"} {
		t.Run(scope, func(t *testing.T) {
			workspace := newSkillTestWorkspace(t)
			userHome := t.TempDir()
			t.Setenv("CODEX_HOME", userHome)
			opts := InstallOptions{Scope: scope, WorkDir: workspace, Mode: ModeCurator, Profile: "software"}
			if _, err := Install(TargetCodex, opts); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
			if scope == "user" {
				filename = filepath.Join(userHome, "skills", "factile", "SKILL.md")
			}
			writeSkillTestFile(t, filename, readSkillTestFile(t, filename)+"\nMy edits.\n")
			before := snapshotSkillTree(t, workspace) + snapshotSkillTree(t, userHome)
			if _, err := Install(TargetCodex, opts); err == nil || !strings.Contains(err.Error(), "edited skill") {
				t.Fatalf("repair accepted an edited skill: %v", err)
			}
			if _, err := Uninstall(TargetCodex, opts); err == nil {
				t.Fatal("uninstall deleted an edited skill")
			}
			if after := snapshotSkillTree(t, workspace) + snapshotSkillTree(t, userHome); after != before {
				t.Fatal("rejected repair changed files")
			}
		})
	}
}

func TestSkillApplyRejectsEditsMadeAfterPlanning(t *testing.T) {
	workspace := newSkillTestWorkspace(t)
	opts := InstallOptions{Scope: "repo", WorkDir: workspace}
	if _, err := Install(TargetCodex, opts); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareRepoInstall(opts)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	writeSkillTestFile(t, filename, readSkillTestFile(t, filename)+"\nAn edit made after planning.\n")
	before := snapshotSkillTree(t, workspace)
	if _, err := ApplyRepoInstall(plan); err == nil {
		t.Fatal("apply overwrote edits")
	}
	if after := snapshotSkillTree(t, workspace); after != before {
		t.Fatal("apply changed files")
	}
}

func TestSkillWarningsAndDoctorAreReadOnlyAndPreserveScope(t *testing.T) {
	useSkillVersion(t, "v1.0.0")
	workspace := newSkillTestWorkspace(t)
	userHome := t.TempDir()
	t.Setenv("CODEX_HOME", userHome)
	configureSkillDoctorRuntime(t)
	for _, scope := range []string{"repo", "user"} {
		if _, err := Install(TargetCodex, InstallOptions{Scope: scope, WorkDir: workspace, Mode: ModeCurator, Profile: "software"}); err != nil {
			t.Fatal(err)
		}
	}
	version.Version = "v2.0.0"
	before := snapshotSkillTree(t, workspace) + snapshotSkillTree(t, userHome)
	warnings := GuidanceWarnings(workspace)
	if len(warnings) != 2 || !strings.Contains(warnings[0], "factile init") ||
		!strings.Contains(warnings[1], "--scope user --mode curator --profile software") {
		t.Fatalf("repair guidance lost scope or intent: %#v", warnings)
	}
	result, err := Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workspace})
	if err != nil || result.OK || result.Version != "v2.0.0" || len(result.Installations) != 2 {
		t.Fatalf("doctor = %#v, %v", result, err)
	}
	for _, installation := range result.Installations {
		if installation.Status != "outdated" || installation.Version != "v1.0.0" || installation.Mode != ModeCurator {
			t.Fatalf("installation = %#v", installation)
		}
	}
	if after := snapshotSkillTree(t, workspace) + snapshotSkillTree(t, userHome); after != before {
		t.Fatal("diagnostics changed files")
	}
}

func TestSkillLegacyRepairAndIdempotence(t *testing.T) {
	workspace := newSkillTestWorkspace(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	filename := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	legacy := "---\nname: factile\n---\n# Factile local knowledge workflow\n\n" + legacySkillInstallMarker(ModeCurator, "software") + "\n"
	writeSkillTestFile(t, filename, legacy)
	warnings := GuidanceWarnings(workspace)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "review local edits") {
		t.Fatalf("legacy warning: %#v", warnings)
	}
	opts := InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeCurator, Profile: "software"}
	if _, err := Install(TargetCodex, opts); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(TargetCodex, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range second.Files {
		if change.Action != "unchanged" {
			t.Fatalf("repeat changed %s", change.Path)
		}
	}
	after, err := os.Stat(filename)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("repeat rewrote the file")
	}
	if warnings := GuidanceWarnings(workspace); len(warnings) != 0 {
		t.Fatalf("current warning: %#v", warnings)
	}
}
