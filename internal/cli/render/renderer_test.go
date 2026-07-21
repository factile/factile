package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/bootstrap"
	"github.com/factile/factile/pkg/skill"
)

func TestResolveColor(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want bool
	}{
		{
			name: "auto terminal",
			opts: Options{ColorMode: ColorAuto, StdoutIsTerminal: true},
			want: true,
		},
		{
			name: "auto not terminal",
			opts: Options{ColorMode: ColorAuto, StdoutIsTerminal: false},
			want: false,
		},
		{
			name: "always overrides non terminal",
			opts: Options{ColorMode: ColorAlways, StdoutIsTerminal: false},
			want: true,
		},
		{
			name: "never disables terminal",
			opts: Options{ColorMode: ColorNever, StdoutIsTerminal: true},
			want: false,
		},
		{
			name: "no color disables always",
			opts: Options{ColorMode: ColorAlways, StdoutIsTerminal: true, Env: map[string]string{"NO_COLOR": "1"}},
			want: false,
		},
		{
			name: "dumb terminal disables always",
			opts: Options{ColorMode: ColorAlways, StdoutIsTerminal: true, Env: map[string]string{"TERM": "dumb"}},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveColor(tc.opts); got != tc.want {
				t.Fatalf("ResolveColor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseColorMode(t *testing.T) {
	if got, err := ParseColorMode("auto"); err != nil || got != ColorAuto {
		t.Fatalf("ParseColorMode(auto) = %q, %v", got, err)
	}
	if _, err := ParseColorMode("sepia"); err == nil || !strings.Contains(err.Error(), "unsupported color mode: sepia") {
		t.Fatalf("expected unsupported color mode error, got %v", err)
	}
}

func TestInitAgentInstallStatusDistinguishesActions(t *testing.T) {
	tests := []struct {
		actions []string
		want    string
	}{
		{actions: []string{"created", "created"}, want: "installed"},
		{actions: []string{"updated", "unchanged"}, want: "upgraded"},
		{actions: []string{"removed", "unchanged"}, want: "upgraded"},
		{actions: []string{"unchanged", "unchanged"}, want: "already installed"},
	}
	for _, test := range tests {
		var files []skill.FileChange
		for _, action := range test.actions {
			files = append(files, skill.FileChange{Action: action})
		}
		if got := agentInstallStatus(bootstrap.AgentResult{Files: files}); got != test.want {
			t.Fatalf("actions %v rendered as %q, want %q", test.actions, got, test.want)
		}
	}
}

func TestRenderInitQuotesWorkspaceInEveryHandoffCommand(t *testing.T) {
	r, err := New(Options{ColorMode: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := r.RenderInit(&output, bootstrap.Result{
		WorkspacePath:  "../Team's Knowledge",
		RootBundlePath: "docs",
		AgentSelection: bootstrap.AgentNone,
		Bundle:         bootstrap.BundlePlan{Name: "docs", Title: "Docs", Description: "Knowledge."},
		Health:         bootstrap.HealthResult{Status: "healthy", OK: true},
	}, "../Team's Knowledge"); err != nil {
		t.Fatal(err)
	}
	want := "factile --workspace '../Team'\"'\"'s Knowledge'"
	if count := strings.Count(output.String(), want); count != 3 {
		t.Fatalf("workspace selection appeared in %d handoff commands, want 3:\n%s", count, output.String())
	}
}

func TestRendererNoColorIsDeterministic(t *testing.T) {
	r, err := New(Options{ColorMode: ColorNever, StdoutIsTerminal: true, Width: 72})
	if err != nil {
		t.Fatal(err)
	}
	if r.ColorEnabled() {
		t.Fatal("color should be disabled")
	}
	if got := r.Styles().Heading.Render("Guide"); got != "Guide" {
		t.Fatalf("no-color heading = %q", got)
	}
	body, err := r.RenderMarkdown("# Guide\n\n- one\n- two\n\n`code`\n\n[Docs](https://example.test)\n")
	if err != nil {
		t.Fatal(err)
	}
	if containsANSI(body) {
		t.Fatalf("no-color Markdown contained ANSI:\n%q", body)
	}
	if !strings.Contains(body, "Guide") || !strings.Contains(body, "one") || !strings.Contains(body, "code") || !strings.Contains(body, "Docs") || !strings.Contains(body, "https://example.test") {
		t.Fatalf("unexpected Markdown output:\n%s", body)
	}
}

func TestRendererColorAlwaysStylesText(t *testing.T) {
	r, err := New(Options{ColorMode: ColorAlways, StdoutIsTerminal: false})
	if err != nil {
		t.Fatal(err)
	}
	if !r.ColorEnabled() {
		t.Fatal("color should be enabled")
	}
	got := r.Styles().Heading.Render("Guide")
	if !containsANSI(got) || !strings.Contains(got, "Guide") {
		t.Fatalf("expected styled heading, got %q", got)
	}
}

func TestPathStyleIsBrightBoldWhite(t *testing.T) {
	style := NewStyles(true).Path
	if !style.GetBold() {
		t.Fatal("path style should be bold")
	}
	red, green, blue, _ := style.GetForeground().RGBA()
	if red != 0xffff || green != 0xffff || blue != 0xffff {
		t.Fatalf("path foreground should be white, got %#x %#x %#x", red, green, blue)
	}
}

func containsANSI(value string) bool {
	return strings.Contains(value, "\x1b")
}
