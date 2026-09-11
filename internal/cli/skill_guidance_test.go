package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/skill"
	"github.com/factile/factile/pkg/version"
)

func oldCLISkillWorkspace(t *testing.T) string {
	t.Helper()
	previous := version.Version
	t.Cleanup(func() { version.Version = previous })
	version.Version = "v1.0.0"
	workspace := t.TempDir()
	t.Chdir(workspace)
	t.Setenv("CODEX_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"init", "--agent", "codex", "--json"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("init: %d %s", code, errOut.String())
	}
	version.Version = "v2.0.0"
	return workspace
}

func TestCLISkillWarningsPreserveOutputAndFiles(t *testing.T) {
	workspace := oldCLISkillWorkspace(t)
	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	before, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		args    []string
		warning bool
	}{
		{"summary", nil, true},
		{"text read", []string{"read", "/overview"}, true},
		{"JSON read", []string{"read", "/overview", "--json"}, false},
		{"quiet read", []string{"read", "/overview", "--quiet"}, false},
		{"version", []string{"version"}, false},
		{"version flag", []string{"--version"}, false},
		{"help", []string{"read", "--help"}, false},
		{"skill inspect", []string{"skill", "inspect", "codex"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := Run(context.Background(), test.args, nil, &out, &errOut)
			if code != 0 {
				t.Fatalf("command: %d %s", code, errOut.String())
			}
			if got := strings.Contains(errOut.String(), "factile init"); got != test.warning {
				t.Fatalf("warning = %s; want %v", errOut.String(), test.warning)
			}
			if strings.Contains(out.String(), "Warning:") {
				t.Fatal("warning polluted stdout")
			}
			if test.name == "JSON read" && !json.Valid(out.Bytes()) {
				t.Fatal("invalid JSON")
			}
		})
	}
	// A JSON read stays byte-identical even when installed guidance becomes stale.
	var oldOut, newOut, errOut bytes.Buffer
	version.Version = "v1.0.0"
	Run(context.Background(), []string{"read", "/overview", "--json"}, nil, &oldOut, &errOut)
	version.Version = "v2.0.0"
	Run(context.Background(), []string{"read", "/overview", "--json"}, nil, &newOut, &errOut)
	if !bytes.Equal(oldOut.Bytes(), newOut.Bytes()) || errOut.Len() != 0 {
		t.Fatal("JSON read contract changed")
	}
	after, err := os.ReadFile(skillPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("ordinary commands updated the skill")
	}
}

func TestCLISkillWarningsDoNotPolluteMCP(t *testing.T) {
	oldCLISkillWorkspace(t)
	var out, errOut bytes.Buffer
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n"
	code := Run(context.Background(), []string{"mcp", "serve", "--stdio", "--read-only"}, strings.NewReader(request), &out, &errOut)
	if code != 0 || errOut.Len() != 0 || !json.Valid(out.Bytes()) || strings.Contains(out.String(), "Warning:") {
		t.Fatalf("MCP stream changed: %d %s %s", code, out.String(), errOut.String())
	}
}

func TestCLIInitPreservesEditedSkillAndShowsRepairReason(t *testing.T) {
	workspace := oldCLISkillWorkspace(t)
	skillPath := filepath.Join(workspace, ".agents", "skills", "factile", "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("\nProject-specific instructions.\n")...)
	writeCLITestFile(t, skillPath, string(content))
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"init"}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "edited skill") || !strings.Contains(errOut.String(), "Preserve your edits") {
		t.Fatalf("repair did not explain rejection: %d %s", code, errOut.String())
	}
	after, err := os.ReadFile(skillPath)
	if err != nil || !bytes.Equal(content, after) {
		t.Fatal("init overwrote edits")
	}
}

func TestCLISkillDoctorInstallationJSONGolden(t *testing.T) {
	want, err := os.ReadFile("../../testdata/golden/skill-installations.json")
	if err != nil {
		t.Fatal(err)
	}
	workspace := oldCLISkillWorkspace(t)
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"skill", "doctor", "codex", "--json"}, nil, &out, &errOut)
	if code == 0 || errOut.Len() != 0 {
		t.Fatalf("doctor: %d %s", code, errOut.String())
	}
	var report struct {
		Version       string               `json:"version"`
		Installations []skill.Installation `json:"installations"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for i := range report.Installations {
		report.Installations[i].Path = filepath.ToSlash(strings.TrimPrefix(report.Installations[i].Path, workspace+string(filepath.Separator)))
	}
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("doctor installation contract:\n%s\nwant:\n%s", got, want)
	}
}
