package skill

import (
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

	readerSkill := skillMarkdown(ModeReader, "")
	if !strings.Contains(readerSkill, "Reader mode is installed") ||
		!strings.Contains(readerSkill, ".factile/config.toml") ||
		!strings.Contains(readerSkill, "<name>.mount.toml") ||
		!strings.Contains(readerSkill, ".factile/views.toml") ||
		strings.Contains(readerSkill, "{{") {
		t.Fatalf("reader skill rendered incorrectly:\n%s", readerSkill)
	}
	for _, stale := range []string{"Knowledge Base", "bundle link", ".factile/mounts.toml", "`factile kb"} {
		if strings.Contains(readerSkill, stale) {
			t.Fatalf("reader skill contains stale local catalog guidance %q:\n%s", stale, readerSkill)
		}
	}

	curatorAgents := agentsManagedBlock(ModeCurator, "software")
	if !strings.Contains(curatorAgents, "Mode: curator") ||
		!strings.Contains(curatorAgents, "Profile: `software`") ||
		!strings.Contains(curatorAgents, "factile mount") ||
		!strings.Contains(curatorAgents, ".factile/views.toml") ||
		strings.Contains(curatorAgents, "{{") {
		t.Fatalf("curator AGENTS block rendered incorrectly:\n%s", curatorAgents)
	}

	readerMCP := mcpConfigBlock(ModeReader)
	if !strings.Contains(readerMCP, `"--read-only"`) || strings.Contains(readerMCP, "{{") {
		t.Fatalf("reader MCP block rendered incorrectly:\n%s", readerMCP)
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
