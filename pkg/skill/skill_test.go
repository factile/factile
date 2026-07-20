package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexAssetsRenderGeneratedContent(t *testing.T) {
	if !strings.Contains(BaseSkillMarkdown, "# Factile local knowledge workflow") {
		t.Fatalf("canonical skill asset missing workflow guidance:\n%s", BaseSkillMarkdown)
	}
	if !strings.Contains(DiscoverScript, "factile list / --json") {
		t.Fatalf("canonical discover script should prefer --json:\n%s", DiscoverScript)
	}
	if !strings.Contains(DiscoverScript, "factile status --json") {
		t.Fatalf("canonical discover script should confirm the workspace first:\n%s", DiscoverScript)
	}

	readerSkill := skillMarkdown(ModeReader, "")
	if !strings.Contains(readerSkill, "Reader mode is installed") ||
		!strings.Contains(readerSkill, "factile.toml` with `[workspace]") ||
		!strings.Contains(readerSkill, "factile.toml` with `[bundle]") ||
		!strings.Contains(readerSkill, "<name>.mount.toml") ||
		!strings.Contains(readerSkill, "factile.views.toml") ||
		!strings.Contains(readerSkill, ".factile/` directory is ignored local state") ||
		!strings.Contains(readerSkill, "read-only Git repositories") ||
		!strings.Contains(readerSkill, "factile refresh <mount-path>") ||
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
		!strings.Contains(curatorAgents, "factile mount") ||
		!strings.Contains(curatorAgents, "factile refresh") ||
		!strings.Contains(curatorAgents, "factile.views.toml") ||
		!strings.Contains(curatorAgents, "never place authored knowledge") ||
		strings.Contains(curatorAgents, "{{") {
		t.Fatalf("curator AGENTS block rendered incorrectly:\n%s", curatorAgents)
	}

	readerMCP := mcpConfigBlock(ModeReader)
	if !strings.Contains(readerMCP, `"--read-only"`) || strings.Contains(readerMCP, "{{") {
		t.Fatalf("reader MCP block rendered incorrectly:\n%s", readerMCP)
	}
}

func TestRepoInstallTargetsContainingWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	nested := filepath.Join(workspace, "bundles", "secondary", "deep")
	writeSkillTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	writeSkillTestFile(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
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

func TestManagedBlockUpsertReplaceAndRemove(t *testing.T) {
	start := "<!-- start -->"
	end := "<!-- end -->"
	content := "# Existing\n\nKeep this.\n"
	firstBlock := start + "\nfirst\n" + end + "\n"

	withFirst := upsertManagedBlock(content, start, end, firstBlock)
	if !strings.Contains(withFirst, "Keep this.") || strings.Count(withFirst, start) != 1 || !strings.HasSuffix(withFirst, "\n") {
		t.Fatalf("unexpected first upsert:\n%s", withFirst)
	}

	secondBlock := start + "\nsecond\n" + end + "\n"
	withSecond := upsertManagedBlock(withFirst, start, end, secondBlock)
	if strings.Contains(withSecond, "first") || !strings.Contains(withSecond, "second") || strings.Count(withSecond, start) != 1 {
		t.Fatalf("replacement should keep one managed block:\n%s", withSecond)
	}

	removed, ok := removeManagedBlock(withSecond, start, end)
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

func writeSkillTestFile(t *testing.T, filename, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func doctorHasCheck(result DoctorResult, name, status string) bool {
	for _, check := range result.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
