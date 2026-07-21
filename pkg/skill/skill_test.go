package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestCodexAssetsRenderGeneratedContent(t *testing.T) {
	if !strings.Contains(BaseSkillMarkdown, "# Factile local knowledge workflow") {
		t.Fatalf("canonical skill asset missing workflow guidance:\n%s", BaseSkillMarkdown)
	}
	if strings.Contains(BaseSkillMarkdown, "\nsummary:") {
		t.Fatalf("canonical skill frontmatter should contain only supported metadata:\n%s", BaseSkillMarkdown)
	}

	readerSkill := skillMarkdown(ModeReader, "")
	if !strings.Contains(readerSkill, "Reader mode is installed") ||
		!strings.Contains(readerSkill, skillInstallMarker(ModeReader, "")) ||
		!strings.Contains(readerSkill, "factile.toml` with `[workspace]") ||
		!strings.Contains(readerSkill, "same manifest when the workspace root is also the") ||
		!strings.Contains(readerSkill, "<name>.mount.toml") ||
		!strings.Contains(readerSkill, "factile.views.toml") ||
		!strings.Contains(readerSkill, "`.factile/` is ignored workspace-local state") ||
		!strings.Contains(readerSkill, "read-only Git repositories") ||
		!strings.Contains(readerSkill, "Repository setup and repair use `factile init`") ||
		!strings.Contains(readerSkill, "Use `--yes --json` for a") ||
		!strings.Contains(readerSkill, "do not run both full and brief root listings") ||
		strings.Contains(readerSkill, "{{") {
		t.Fatalf("reader skill rendered incorrectly:\n%s", readerSkill)
	}
	for _, stale := range []string{"Knowledge Base", "bundle link", ".factile/mounts.toml", ".factile/config.toml", ".factile/views.toml", "`--root", "no_active_root", "`factile kb"} {
		if strings.Contains(readerSkill, stale) {
			t.Fatalf("reader skill contains stale local catalog guidance %q:\n%s", stale, readerSkill)
		}
	}

	curatorAgents := agentsManagedBlock(ModeCurator, "software")
	if !strings.Contains(curatorAgents, "Mode: curator") ||
		!strings.Contains(curatorAgents, "Profile: `software`") ||
		!strings.Contains(curatorAgents, "use the installed\n`factile` skill") ||
		!strings.Contains(curatorAgents, "Mutate Factile knowledge only when the user explicitly asks") ||
		strings.Contains(curatorAgents, "factile context") ||
		strings.Contains(curatorAgents, "{{") {
		t.Fatalf("curator AGENTS block rendered incorrectly:\n%s", curatorAgents)
	}

	readerMCP := mcpConfigBlock(ModeReader)
	if !strings.Contains(readerMCP, `"--read-only"`) || strings.Contains(readerMCP, "{{") {
		t.Fatalf("reader MCP block rendered incorrectly:\n%s", readerMCP)
	}
}

func TestInspectRepoInstallUsesOnlyGeneratedMetadataAsIntent(t *testing.T) {
	workspace := t.TempDir()
	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	if intent := InspectRepoInstall(workspace); intent.Installed || intent.Trusted {
		t.Fatalf("missing install returned intent: %#v", intent)
	}

	writeSkillTestFile(t, skillPath, skillMarkdown(ModeCurator, "software"))
	intent := InspectRepoInstall(workspace)
	if !intent.Installed || !intent.Managed || !intent.Trusted || intent.Current || !intent.SkillCurrent || intent.AgentsCurrent || intent.MCPCurrent || intent.Mode != ModeCurator || intent.Profile != "software" {
		t.Fatalf("generated install intent was not recognized: %#v", intent)
	}
	writeSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"), agentsManagedBlock(ModeCurator, "software"))
	writeSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"), mcpConfigBlock(ModeCurator))
	intent = InspectRepoInstall(workspace)
	if !intent.Current || !intent.SkillCurrent || !intent.AgentsCurrent || !intent.MCPCurrent {
		t.Fatalf("current managed integration was not recognized: %#v", intent)
	}

	writeSkillTestFile(t, skillPath, strings.Replace(skillMarkdown(ModeCurator, "software"), "# Factile local knowledge workflow", "# Drifted local workflow", 1))
	intent = InspectRepoInstall(workspace)
	if !intent.Trusted || intent.Current || intent.SkillCurrent || !intent.AgentsCurrent || !intent.MCPCurrent || intent.Mode != ModeCurator || intent.Profile != "software" {
		t.Fatalf("content drift should not erase trusted install metadata: %#v", intent)
	}

	writeSkillTestFile(t, skillPath, "hand-written skill without install metadata\n")
	intent = InspectRepoInstall(workspace)
	if !intent.Installed || !intent.Managed || intent.Trusted || intent.Mode != "" || intent.Profile != "" {
		t.Fatalf("unrecognized skill was treated as trusted intent: %#v", intent)
	}
}

func TestInspectRepoInstallPreservesV040GeneratedIntent(t *testing.T) {
	workspace := t.TempDir()
	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	legacy := "---\nname: factile\n---\n\n" +
		"# Factile local knowledge workflow\n\n" +
		"Factile exposes one workspace's OKF knowledge as a virtual filesystem.\n\n" +
		"## Mode\n\nCurator mode is installed. Use Factile to manage local knowledge.\n\n" +
		"## Profile\n\nProfile: `software`.\n"
	writeSkillTestFile(t, skillPath, legacy)

	intent := InspectRepoInstall(workspace)
	if !intent.Installed || !intent.Managed || !intent.Trusted || intent.Current || intent.Mode != ModeCurator || intent.Profile != "software" {
		t.Fatalf("v0.4 generated install intent was not preserved: %#v", intent)
	}
}

func TestRepoInstallRejectsSymlinkOutputBeforeMutation(t *testing.T) {
	workspace := t.TempDir()
	writeSkillTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	writeSkillTestFile(t, filepath.Join(workspace, "docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeSkillTestFile(t, outside, "preserve\n")
	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, skillPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace}); err == nil {
		t.Fatal("repo install accepted a symlink output")
	}
	if got := readSkillTestFile(t, outside); got != "preserve\n" {
		t.Fatalf("repo install wrote through symlink: %q", got)
	}
	for _, filename := range []string{"AGENTS.md", filepath.Join(".codex", "config.toml")} {
		if _, err := os.Stat(filepath.Join(workspace, filename)); !os.IsNotExist(err) {
			t.Fatalf("rejected repo install created %s: %v", filename, err)
		}
	}
}

func TestRepoInstallTargetsContainingWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	nested := filepath.Join(workspace, "bundles", "secondary", "deep")
	writeSkillTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	writeSkillTestFile(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	writeSkillTestFile(t, filepath.Join(workspace, ".agents", "skills", "factile", "scripts", "factile-discover.sh"), "#!/bin/sh\n")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	result, err := Install(TargetCodex, InstallOptions{Scope: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scope != "repo" || !fileExists(filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")) || !fileExists(filepath.Join(workspace, "AGENTS.md")) || !fileExists(filepath.Join(workspace, ".codex", "config.toml")) {
		t.Fatalf("repo install did not target workspace root: %#v", result)
	}
	if fileExists(filepath.Join(nested, ".agents", "skills", "factile", "SKILL.md")) {
		t.Fatalf("repo install wrote inside secondary bundle: %#v", result)
	}
	if fileExists(filepath.Join(workspace, ".agents", "skills", "factile", "scripts", "factile-discover.sh")) {
		t.Fatal("repo install should not retain the retired discovery script")
	}
}

func TestUserInstallRemovesRetiredDiscoveryScript(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	legacyScript := filepath.Join(codexHome, "skills", "factile", "scripts", "factile-discover.sh")
	writeSkillTestFile(t, legacyScript, "#!/bin/sh\n")

	result, err := Install(TargetCodex, InstallOptions{Scope: "user", Mode: ModeReader})
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(codexHome, "skills", "factile", "SKILL.md")) {
		t.Fatalf("user install did not write the generated skill: %#v", result)
	}
	if fileExists(legacyScript) {
		t.Fatalf("user install retained the retired discovery script: %#v", result)
	}
	result, err = Install(TargetCodex, InstallOptions{Scope: "user", Mode: ModeReader})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Action != "unchanged" {
		t.Fatalf("repeated user install was not idempotent: %#v", result)
	}
	unrelated := filepath.Join(codexHome, "skills", "factile", "notes.txt")
	writeSkillTestFile(t, unrelated, "preserve\n")
	uninstalled, err := Uninstall(TargetCodex, InstallOptions{Scope: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(codexHome, "skills", "factile", "SKILL.md")) || readSkillTestFile(t, unrelated) != "preserve\n" {
		t.Fatalf("user uninstall removed unowned content: %#v", uninstalled)
	}
}

func TestDoctorDiagnosesLegacyLayoutAndGuidance(t *testing.T) {
	workDir := t.TempDir()
	writeSkillTestFile(t, filepath.Join(workDir, "docs", ".factile", "config.toml"), "version = 1\n")
	writeSkillTestFile(t, filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md"), "A Factile root is marked by `.factile/config.toml`.\n")
	writeSkillTestFile(t, filepath.Join(workDir, "AGENTS.md"), AgentsBlockStart+"\nViews live in `.factile/views.toml`.\n"+AgentsBlockEnd+"\n")
	writeSkillTestFile(t, filepath.Join(workDir, ".codex", "config.toml"), MCPBlockStart+"\n[mcp_servers.factile]\ncommand = \"factile\"\nargs = [\"--root\", \"docs\", \"mcp\", \"serve\", \"--stdio\"]\n"+MCPBlockEnd+"\n")
	binDir := filepath.Join(t.TempDir(), "bin")
	writeSkillTestFile(t, filepath.Join(binDir, "factile"), "#!/bin/sh\nprintf '{}\\n'\n")
	if err := os.Chmod(filepath.Join(binDir, "factile"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_HOME", t.TempDir())

	result, err := Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !doctorHasCheck(result, "workspace_layout", "fail") || !doctorHasCheck(result, "guidance_layout", "fail") || !doctorHasCheck(result, "mcp_config", "fail") {
		t.Fatalf("doctor did not diagnose legacy state: %#v", result)
	}
	joined := ""
	for _, check := range result.Checks {
		joined += check.Message + "\n"
	}
	if !strings.Contains(joined, "factile.toml") || !strings.Contains(joined, "rerun `factile skill install") {
		t.Fatalf("doctor migration action is not actionable:\n%s", joined)
	}
}

func TestDoctorRejectsGeneratedGuidanceDriftAndMCPModeMismatch(t *testing.T) {
	workDir := t.TempDir()
	writeSkillTestFile(t, filepath.Join(workDir, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	writeSkillTestFile(t, filepath.Join(workDir, "docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	t.Chdir(workDir)
	t.Setenv("CODEX_HOME", t.TempDir())

	if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", Mode: ModeReader}); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(t.TempDir(), "bin")
	writeSkillTestFile(t, filepath.Join(binDir, "factile"), "#!/bin/sh\nprintf '{}\\n'\n")
	if err := os.Chmod(filepath.Join(binDir, "factile"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("fresh reader install should pass doctor: %#v", result)
	}

	skillPath := filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md")
	skillData, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(skillData), "# Factile local knowledge workflow", "# Locally drifted workflow", 1)
	if err := os.WriteFile(skillPath, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !doctorHasCheck(result, "guidance_layout", "fail") {
		t.Fatalf("doctor should reject generated skill drift: %#v", result)
	}

	if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", Mode: ModeReader}); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	driftedAgents := strings.Replace(string(agentsData), "It owns the discovery workflow", "It duplicates the discovery workflow", 1)
	if err := os.WriteFile(agentsPath, []byte(driftedAgents), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !doctorHasCheck(result, "agents_managed_block", "fail") || !doctorHasCheck(result, "guidance_layout", "pass") {
		t.Fatalf("doctor should reject managed AGENTS.md drift: %#v", result)
	}

	if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", Mode: ModeReader}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workDir, ".codex", "config.toml")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeCapable := strings.Replace(string(configData), ", \"--read-only\"", "", 1)
	if err := os.WriteFile(configPath, []byte(writeCapable), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !doctorHasCheck(result, "mcp_config", "fail") || !doctorHasCheck(result, "guidance_layout", "pass") {
		t.Fatalf("doctor should reject reader guidance with write-capable MCP: %#v", result)
	}
}

func TestDoctorRejectsDuplicateMalformedAndOrphanRepoBlocks(t *testing.T) {
	tests := []struct {
		name      string
		repoSkill bool
		agents    string
		mcp       string
		check     string
	}{
		{name: "orphan agents with current user skill", agents: agentsManagedBlock(ModeReader, ""), check: "agents_managed_block"},
		{name: "orphan MCP with current user skill", mcp: mcpConfigBlock(ModeReader), check: "mcp_config"},
		{name: "duplicate agents", repoSkill: true, agents: agentsManagedBlock(ModeReader, "") + agentsManagedBlock(ModeReader, ""), mcp: mcpConfigBlock(ModeReader), check: "agents_managed_block"},
		{name: "duplicate MCP", repoSkill: true, agents: agentsManagedBlock(ModeReader, ""), mcp: mcpConfigBlock(ModeReader) + mcpConfigBlock(ModeReader), check: "mcp_config"},
		{name: "malformed agents", repoSkill: true, agents: AgentsBlockStart + "\nmissing end\n", mcp: mcpConfigBlock(ModeReader), check: "agents_managed_block"},
		{name: "malformed MCP", repoSkill: true, agents: agentsManagedBlock(ModeReader, ""), mcp: MCPBlockEnd + "\n" + MCPBlockStart + "\n", check: "mcp_config"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workspace := newSkillTestWorkspace(t)
			if tc.repoSkill {
				writeSkillTestFile(t, filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md"), skillMarkdown(ModeReader, ""))
			}
			if tc.agents != "" {
				writeSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"), tc.agents)
			}
			if tc.mcp != "" {
				writeSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"), tc.mcp)
			}
			codexHome := t.TempDir()
			t.Setenv("CODEX_HOME", codexHome)
			writeSkillTestFile(t, filepath.Join(codexHome, "skills", "factile", "SKILL.md"), skillMarkdown(ModeReader, ""))
			configureSkillDoctorRuntime(t)

			result, err := Doctor(context.Background(), TargetCodex, DoctorOptions{WorkDir: workspace})
			if err != nil {
				t.Fatal(err)
			}
			if result.OK || !doctorHasCheck(result, tc.check, "fail") {
				t.Fatalf("doctor accepted invalid repo integration: %#v", result)
			}
		})
	}
}

func TestRepoInstallCanonicalizesAndUninstallsDuplicateManagedBlocks(t *testing.T) {
	workspace := newSkillTestWorkspace(t)
	writeSkillTestFile(t, filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md"), skillMarkdown(ModeReader, ""))
	oldAgents := strings.TrimRight(agentsManagedBlock(ModeReader, ""), "\n")
	oldMCP := strings.TrimRight(mcpConfigBlock(ModeReader), "\n")
	writeSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"), "authored before\n"+oldAgents+"\nauthored between\n"+oldAgents+"\nauthored after\n")
	writeSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"), "# authored before\n"+oldMCP+"\n# authored between\n"+oldMCP+"\n# authored after\n")

	if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeCurator, Profile: "software"}); err != nil {
		t.Fatal(err)
	}
	agents := readSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"))
	config := readSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"))
	if strings.Count(agents, AgentsBlockStart) != 1 || strings.Count(agents, AgentsBlockEnd) != 1 || !strings.Contains(agents, "authored between") || !strings.Contains(agents, "authored after") {
		t.Fatalf("AGENTS.md duplicates were not reconciled safely:\n%s", agents)
	}
	if strings.Count(config, MCPBlockStart) != 1 || strings.Count(config, MCPBlockEnd) != 1 || strings.Count(config, "[mcp_servers.factile]") != 1 || !strings.Contains(config, "# authored between") || !strings.Contains(config, "# authored after") {
		t.Fatalf("MCP duplicates were not reconciled safely:\n%s", config)
	}
	var decoded map[string]any
	if _, err := toml.Decode(config, &decoded); err != nil {
		t.Fatalf("reconciled MCP config is invalid TOML: %v\n%s", err, config)
	}
	intent := InspectRepoInstall(workspace)
	if !intent.Current || intent.Mode != ModeCurator || intent.Profile != "software" {
		t.Fatalf("reconciled install is not current: %#v", intent)
	}

	currentAgents := strings.TrimRight(agentsManagedBlock(ModeCurator, "software"), "\n")
	currentMCP := strings.TrimRight(mcpConfigBlock(ModeCurator), "\n")
	writeSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"), "authored before\n"+currentAgents+"\nauthored between\n"+currentAgents+"\nauthored after\n")
	writeSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"), "# authored before\n"+currentMCP+"\n# authored between\n"+currentMCP+"\n# authored after\n")
	if _, err := Uninstall(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace}); err != nil {
		t.Fatal(err)
	}
	agents = readSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"))
	config = readSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"))
	if strings.Contains(agents, AgentsBlockStart) || !strings.Contains(agents, "authored between") || !strings.Contains(agents, "authored after") {
		t.Fatalf("repo uninstall did not remove every AGENTS block safely:\n%s", agents)
	}
	if strings.Contains(config, MCPBlockStart) || !strings.Contains(config, "# authored between") || !strings.Contains(config, "# authored after") {
		t.Fatalf("repo uninstall did not remove every MCP block safely:\n%s", config)
	}
}

func TestRepoInstallPublicationFailuresAreAtomicAndRestartSafe(t *testing.T) {
	paths := []string{
		filepath.Join(".agents", "skills", "factile", "SKILL.md"),
		"AGENTS.md",
		filepath.Join(".codex", "config.toml"),
	}
	freshWant := map[string]string{
		paths[0]: skillMarkdown(ModeReader, ""),
		paths[1]: agentsManagedBlock(ModeReader, ""),
		paths[2]: mcpConfigBlock(ModeReader),
	}
	for failAt := 1; failAt <= len(paths); failAt++ {
		t.Run("fresh publication "+string(rune('0'+failAt)), func(t *testing.T) {
			workspace := newSkillTestWorkspace(t)
			restore := failManagedPublication(failAt)
			_, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeReader})
			restore()
			if err == nil {
				t.Fatalf("publication %d did not fail", failAt)
			}
			assertCompleteSkillFiles(t, workspace, freshWant)
			assertNoSkillTemps(t, workspace)

			if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeReader}); err != nil {
				t.Fatalf("rerun did not converge: %v", err)
			}
			if intent := InspectRepoInstall(workspace); !intent.Current || intent.Mode != ModeReader {
				t.Fatalf("rerun did not produce current reader integration: %#v", intent)
			}
			restore = failManagedPublication(1)
			result, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeReader})
			restore()
			if err != nil {
				t.Fatalf("healthy no-op attempted publication: %v", err)
			}
			for _, change := range result.Files {
				if change.Action != "unchanged" {
					t.Fatalf("healthy reinstall changed %s: %#v", change.Path, result.Files)
				}
			}
		})
	}

	updatedWant := map[string]string{
		paths[0]: skillMarkdown(ModeCurator, "software"),
		paths[1]: agentsManagedBlock(ModeCurator, "software"),
		paths[2]: mcpConfigBlock(ModeCurator),
	}
	for failAt := 1; failAt <= len(paths); failAt++ {
		t.Run("replacement publication "+string(rune('0'+failAt)), func(t *testing.T) {
			workspace := newSkillTestWorkspace(t)
			if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeReader}); err != nil {
				t.Fatal(err)
			}
			old := make(map[string]string, len(paths))
			for _, rel := range paths {
				old[rel] = readSkillTestFile(t, filepath.Join(workspace, rel))
			}
			restore := failManagedPublication(failAt)
			_, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeCurator, Profile: "software"})
			restore()
			if err == nil {
				t.Fatalf("replacement %d did not fail", failAt)
			}
			for _, rel := range paths {
				got := readSkillTestFile(t, filepath.Join(workspace, rel))
				if got != old[rel] && got != updatedWant[rel] {
					t.Fatalf("%s is neither complete old nor complete new content:\n%s", rel, got)
				}
			}
			assertNoSkillTemps(t, workspace)
			if _, err := Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeCurator, Profile: "software"}); err != nil {
				t.Fatalf("replacement rerun did not converge: %v", err)
			}
			assertCompleteSkillFiles(t, workspace, updatedWant)
			if intent := InspectRepoInstall(workspace); !intent.Current || intent.Mode != ModeCurator || intent.Profile != "software" {
				t.Fatalf("replacement rerun lost intent: %#v", intent)
			}
		})
	}
}

func TestRepoReconciliationRejectsMalformedMarkersWithoutMutation(t *testing.T) {
	malformed := map[string]string{
		"start only": AgentsBlockStart + "\nmanaged\n",
		"end only":   AgentsBlockEnd + "\n",
		"reversed":   AgentsBlockEnd + "\n" + AgentsBlockStart + "\n",
		"nested":     AgentsBlockStart + "\n" + AgentsBlockStart + "\n" + AgentsBlockEnd + "\n" + AgentsBlockEnd + "\n",
	}
	for name, content := range malformed {
		for _, operation := range []string{"install", "uninstall"} {
			t.Run(name+" "+operation, func(t *testing.T) {
				workspace := newSkillTestWorkspace(t)
				writeSkillTestFile(t, filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md"), skillMarkdown(ModeReader, ""))
				writeSkillTestFile(t, filepath.Join(workspace, "AGENTS.md"), "authored\n"+content)
				writeSkillTestFile(t, filepath.Join(workspace, ".codex", "config.toml"), mcpConfigBlock(ModeReader))
				before := snapshotSkillTree(t, workspace)
				var firstError string
				for attempt := 0; attempt < 2; attempt++ {
					var err error
					if operation == "install" {
						_, err = Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace, Mode: ModeCurator})
					} else {
						_, err = Uninstall(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace})
					}
					if err == nil {
						t.Fatal("malformed managed markers were accepted")
					}
					if attempt == 0 {
						firstError = err.Error()
					} else if err.Error() != firstError {
						t.Fatalf("repeated failure changed: first=%q second=%q", firstError, err)
					}
					if after := snapshotSkillTree(t, workspace); after != before {
						t.Fatalf("rejected reconciliation changed the fixture:\nbefore:\n%s\nafter:\n%s", before, after)
					}
				}
			})
		}
	}
}

func TestRepoReconciliationRejectsUnrecognizedSkillWithoutMutation(t *testing.T) {
	for _, operation := range []string{"install", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			workspace := newSkillTestWorkspace(t)
			writeSkillTestFile(t, filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md"), "hand-authored skill\n")
			if intent := InspectRepoInstall(workspace); !intent.Installed || intent.Managed || intent.Trusted {
				t.Fatalf("custom skill was mistaken for generated integration: %#v", intent)
			}
			before := snapshotSkillTree(t, workspace)
			var err error
			if operation == "install" {
				_, err = Install(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace})
			} else {
				_, err = Uninstall(TargetCodex, InstallOptions{Scope: "repo", WorkDir: workspace})
			}
			if err == nil {
				t.Fatal("unrecognized canonical skill was accepted")
			}
			if after := snapshotSkillTree(t, workspace); after != before {
				t.Fatalf("skill collision changed the fixture:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestRepoAndUserScopeOperationsRejectManagedPathSymlinks(t *testing.T) {
	for _, scope := range []string{"repo", "user"} {
		for _, component := range []string{"skills", "factile", "scripts", "target"} {
			for _, operation := range []string{"install", "uninstall"} {
				t.Run(scope+" "+component+" "+operation, func(t *testing.T) {
					var anchor string
					var opts InstallOptions
					if scope == "repo" {
						anchor = newSkillTestWorkspace(t)
						opts = InstallOptions{Scope: "repo", WorkDir: anchor}
					} else {
						anchor = t.TempDir()
						t.Setenv("CODEX_HOME", anchor)
						opts = InstallOptions{Scope: "user"}
					}
					var link string
					if scope == "repo" {
						switch component {
						case "skills":
							link = filepath.Join(anchor, ".agents", "skills")
						case "factile":
							link = filepath.Join(anchor, ".agents", "skills", "factile")
						case "scripts":
							link = filepath.Join(anchor, ".agents", "skills", "factile", "scripts")
						case "target":
							link = filepath.Join(anchor, ".agents", "skills", "factile", "SKILL.md")
						}
					} else {
						switch component {
						case "skills":
							link = filepath.Join(anchor, "skills")
						case "factile":
							link = filepath.Join(anchor, "skills", "factile")
						case "scripts":
							link = filepath.Join(anchor, "skills", "factile", "scripts")
						case "target":
							link = filepath.Join(anchor, "skills", "factile", "SKILL.md")
						}
					}
					if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
						t.Fatal(err)
					}
					outside := t.TempDir()
					target := outside
					if component == "target" {
						target = filepath.Join(outside, "SKILL.md")
						writeSkillTestFile(t, target, "outside sentinel\n")
					} else {
						writeSkillTestFile(t, filepath.Join(outside, "sentinel"), "outside sentinel\n")
					}
					before := snapshotSkillTree(t, outside)
					if err := os.Symlink(target, link); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
					var err error
					if operation == "install" {
						_, err = Install(TargetCodex, opts)
					} else {
						_, err = Uninstall(TargetCodex, opts)
					}
					if err == nil {
						t.Fatal("managed-path symlink was accepted")
					}
					if after := snapshotSkillTree(t, outside); after != before {
						t.Fatalf("scope operation changed external data:\nbefore:\n%s\nafter:\n%s", before, after)
					}
				})
			}
		}
	}
}

func TestManagedBlockClassifier(t *testing.T) {
	start := "<start>"
	end := "<end>"
	tests := []struct {
		name    string
		content string
		kind    managedBlockKind
	}{
		{name: "absent", content: "authored", kind: managedBlockAbsent},
		{name: "single", content: start + "managed" + end, kind: managedBlockSingle},
		{name: "multiple", content: start + "one" + end + "between" + start + "two" + end, kind: managedBlockMultiple},
		{name: "start only", content: start, kind: managedBlockMalformed},
		{name: "end only", content: end, kind: managedBlockMalformed},
		{name: "reversed", content: end + start, kind: managedBlockMalformed},
		{name: "nested", content: start + start + end + end, kind: managedBlockMalformed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyManagedBlocks(tc.content, start, end).kind; got != tc.kind {
				t.Fatalf("kind = %v, want %v", got, tc.kind)
			}
		})
	}
}

func TestManagedBlockUpsertReplaceAndRemove(t *testing.T) {
	start := "<!-- start -->"
	end := "<!-- end -->"
	content := "# Existing\n\nKeep this.\n"
	firstBlock := start + "\nfirst\n" + end + "\n"

	withFirst, err := upsertManagedBlock(content, start, end, firstBlock)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withFirst, "Keep this.") || strings.Count(withFirst, start) != 1 || !strings.HasSuffix(withFirst, "\n") {
		t.Fatalf("unexpected first upsert:\n%s", withFirst)
	}

	secondBlock := start + "\nsecond\n" + end + "\n"
	withSecond, err := upsertManagedBlock(withFirst, start, end, secondBlock)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withSecond, "first") || !strings.Contains(withSecond, "second") || strings.Count(withSecond, start) != 1 {
		t.Fatalf("replacement should keep one managed block:\n%s", withSecond)
	}

	removed, ok, err := removeManagedBlock(withSecond, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected managed block removal")
	}
	if strings.Contains(removed, start) || strings.Contains(removed, "second") || !strings.Contains(removed, "Keep this.") {
		t.Fatalf("unexpected removal result:\n%s", removed)
	}
}

func TestFileBlockOperationsReportStableActions(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", "AGENTS.md")
	start := "<!-- start -->"
	end := "<!-- end -->"
	block := start + "\nmanaged\n" + end + "\n"

	change, err := upsertFileBlock(file, start, end, block)
	if err != nil {
		t.Fatal(err)
	}
	if change.Action != "created" {
		t.Fatalf("first upsert action = %s, want created", change.Action)
	}

	change, err = upsertFileBlock(file, start, end, block)
	if err != nil {
		t.Fatal(err)
	}
	if change.Action != "unchanged" {
		t.Fatalf("second upsert action = %s, want unchanged", change.Action)
	}

	change, err = removeFileBlock(file, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if change.Action != "updated" {
		t.Fatalf("remove action = %s, want updated", change.Action)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), start) {
		t.Fatalf("managed block was not removed:\n%s", string(data))
	}
}

func newSkillTestWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	writeSkillTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	writeSkillTestFile(t, filepath.Join(workspace, "docs", "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	return workspace
}

func configureSkillDoctorRuntime(t *testing.T) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	writeSkillTestFile(t, filepath.Join(binDir, "factile"), "#!/bin/sh\nprintf '{}\\n'\n")
	if err := os.Chmod(filepath.Join(binDir, "factile"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func snapshotSkillTree(t *testing.T, root string) string {
	t.Helper()
	var snapshot strings.Builder
	if err := filepath.Walk(root, func(filename string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		if info.IsDir() {
			snapshot.WriteString(filepath.ToSlash(rel) + "/\n")
			return nil
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		snapshot.WriteString(filepath.ToSlash(rel) + ":" + string(data) + "\n")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
}

func failManagedPublication(failAt int) func() {
	originalReplace := replaceManagedFile
	originalCreate := createManagedFile
	publication := 0
	fail := func() error {
		publication++
		if publication == failAt {
			return errors.New("injected managed publication failure")
		}
		return nil
	}
	replaceManagedFile = func(filename string, data []byte, mode os.FileMode) error {
		if err := fail(); err != nil {
			return err
		}
		return originalReplace(filename, data, mode)
	}
	createManagedFile = func(filename string, data []byte, mode os.FileMode) (bool, error) {
		if err := fail(); err != nil {
			return false, err
		}
		return originalCreate(filename, data, mode)
	}
	return func() {
		replaceManagedFile = originalReplace
		createManagedFile = originalCreate
	}
}

func assertCompleteSkillFiles(t *testing.T, workspace string, want map[string]string) {
	t.Helper()
	for rel, expected := range want {
		data, err := os.ReadFile(filepath.Join(workspace, rel))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != expected {
			t.Fatalf("visible managed output %s is incomplete:\ngot:\n%s\nwant:\n%s", rel, data, expected)
		}
	}
}

func assertNoSkillTemps(t *testing.T, workspace string) {
	t.Helper()
	if err := filepath.Walk(workspace, func(filename string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.Contains(info.Name(), ".factile-tmp-") {
			return errors.New("temporary file remains: " + filename)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func writeSkillTestFile(t *testing.T, filename, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSkillTestFile(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func doctorHasCheck(result DoctorResult, name, status string) bool {
	for _, check := range result.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
