package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/skill"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/version"
)

func TestCLIHelpAndReadJSON(t *testing.T) {
	for _, args := range [][]string{{"--help"}} {
		var stdout bytes.Buffer
		code := Run(context.Background(), args, nil, &stdout, &bytes.Buffer{})
		if code != 0 {
			t.Fatalf("%v help exit code = %d", args, code)
		}
		help := stdout.String()
		if !strings.Contains(help, "Factile local OKF tool") || !strings.Contains(help, "Local OKF knowledge as paths") {
			t.Fatalf("unexpected help for %v: %s", args, help)
		}
		for _, expected := range []string{
			"Start here",
			"factile init",
			"factile status",
			"factile /",
			"factile list /",
			"factile context / \"what should I know?\"",
			"factile ui",
			"factile version",
			"Reader commands",
			"stat     <path>",
			"Curator commands",
			"Bundle admin",
			"Agents and MCP",
			"Use --json for scripts and agents",
		} {
			if !strings.Contains(help, expected) {
				t.Fatalf("help for %v missing %q:\n%s", args, expected, help)
			}
		}
		for _, expected := range []string{"--json", "--color auto|always|never", "--quiet"} {
			if !strings.Contains(help, expected) {
				t.Fatalf("help for %v missing global option %q:\n%s", args, expected, help)
			}
		}
		for _, expected := range []string{
			"mount",
			"<source> <mount-path>",
			"Create a path mount descriptor",
			"unmount",
			"Remove a path mount descriptor",
			"mounts",
			"List configured mounts",
			"view list",
			"List views",
			"view inspect",
			"Inspect a view",
			"view set",
			"Create or replace a view",
			"view delete",
			"Delete a view",
		} {
			if !strings.Contains(help, expected) {
				t.Fatalf("help for %v missing aligned curator row %q:\n%s", args, expected, help)
			}
		}
		for _, expected := range []string{"mkdir     <path>", "Create a directory scaffold"} {
			if !strings.Contains(help, expected) {
				t.Fatalf("help for %v missing mkdir row %q:\n%s", args, expected, help)
			}
		}
		for _, stale := range []string{"kb list", "Knowledge Bases", "Bundle link"} {
			if strings.Contains(help, stale) {
				t.Fatalf("help for %v still exposes stale curator row %q:\n%s", args, stale, help)
			}
		}
		if strings.Contains(help, "--verbose") {
			t.Fatalf("help for %v still mentions --verbose:\n%s", args, help)
		}
		assertNotJSONText(t, help)
		assertNoTerminalEscapes(t, help)
	}

	mountFile := cliMountFile(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("read exit code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"path": "/product-docs/workflows/invoice-import"`) || !strings.Contains(stdout.String(), `"revision": "sha256:`) {
		t.Fatalf("unexpected read JSON: %s", stdout.String())
	}
}

func TestCLIBareWorkspaceSummary(t *testing.T) {
	workspace := cliV2Workspace(t)
	t.Chdir(workspace)
	runCLIJSON[factile.ViewResult](t, "view", "set", "invoice", "--title", "Invoice", "--path", "/engineering/django/workflows/invoice-import", "--path", "/legacy", "--json")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bare summary exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"Factile Workspace:\n  " + workspace + " (", "Knowledge:", "/engineering", "Views:", "invoice  Invoice", "Sources:", "/engineering/django ->", "Health:", "Next:", "factile context / \"<task>\" --view invoice"} {
		if !strings.Contains(output, want) {
			t.Fatalf("bare summary missing %q:\n%s", want, output)
		}
	}
	for _, stale := range []string{"Factile workspace:", "Path: ", "Version:", "Knowledge Views:"} {
		if strings.Contains(output, stale) {
			t.Fatalf("bare summary still contains stale label %q:\n%s", stale, output)
		}
	}
	if strings.Contains(output, "Reader commands") || strings.Contains(output, "Usage") {
		t.Fatalf("bare summary should not print full help:\n%s", output)
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	code = Run(context.Background(), []string{"--color", "always"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("colored bare summary exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	colored := stdout.String()
	for _, want := range []string{"Factile Workspace:", workspace, "/engineering", "/legacy", "\x1b"} {
		if !strings.Contains(colored, want) {
			t.Fatalf("colored bare summary missing %q:\n%q", want, colored)
		}
	}

	summary := runCLIJSON[factile.SummaryResult](t, "status", "--json")
	if summary.Workspace.Path != workspace || len(summary.Knowledge) == 0 || len(summary.Views) != 1 || len(summary.Sources) == 0 || len(summary.NextCommands) == 0 {
		t.Fatalf("unexpected status JSON summary: %#v", summary)
	}

	empty := t.TempDir()
	t.Chdir(empty)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--color", "never"}, nil, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("empty summary exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "No active Factile root") || !strings.Contains(stderr.String(), "factile init") {
		t.Fatalf("empty summary should report no active library, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestCLIVersionOutput(t *testing.T) {
	wantText := version.Current().String()
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Run(context.Background(), args, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%v exit code = %d stderr=%s", args, code, stderr.String())
		}
		if got := strings.TrimSpace(stdout.String()); got != wantText {
			t.Fatalf("%v output = %q, want %q", args, got, wantText)
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--json", "version"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("version --json exit code = %d stderr=%s", code, stderr.String())
	}
	var got version.Info
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("version JSON did not parse: %v\n%s", err, stdout.String())
	}
	if got != version.Current() {
		t.Fatalf("version JSON = %#v, want %#v", got, version.Current())
	}
}

func TestCLIRejectsVerboseGlobalOption(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--verbose", "--json"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("--verbose unexpectedly succeeded: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("--verbose wrote stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), `"code":"unsupported_command"`) || !strings.Contains(stderr.String(), "Unsupported command: --verbose") {
		t.Fatalf("expected structured unsupported command error, code=%d stderr=%s", code, stderr.String())
	}
}

func TestCLISubcommandHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "list", args: []string{"list", "--help"}, want: "factile list [path] [--brief] [--view <id>]"},
		{name: "status", args: []string{"status", "--help"}, want: "factile status"},
		{name: "search", args: []string{"search", "--help"}, want: "factile search <path> <query> [--view <id>]"},
		{name: "mkdir", args: []string{"mkdir", "--help"}, want: "factile mkdir <path> [--title <title>] [--log] [--overview] [--bundle]"},
		{name: "create", args: []string{"create", "--help"}, want: "factile create <document-path> --type <type> --title <title> --body <file>"},
		{name: "context", args: []string{"context", "--help"}, want: "factile context <path> <query> [--max-tokens <n>] [--depth 0|1] [--view <id>]"},
		{name: "graph", args: []string{"graph", "--help"}, want: "factile graph <path> [--depth 0|1] [--view <id>]"},
		{name: "validate", args: []string{"validate", "--help"}, want: "factile validate <path> [--view <id>]"},
		{name: "ui", args: []string{"ui", "--help"}, want: "factile ui [--port <port>] [--no-open] [--dev-assets <url>] [--curator]"},
		{name: "mount", args: []string{"mount", "--help"}, want: "factile mount <source> <mount-path> [--read-only] [--title <title>] [--description <text>]"},
		{name: "unmount", args: []string{"unmount", "--help"}, want: "factile unmount <mount-path>"},
		{name: "mounts", args: []string{"mounts", "--help"}, want: "factile mounts"},
		{name: "view group", args: []string{"view", "--help"}, want: "factile view list|inspect|set|delete"},
		{name: "view leaf", args: []string{"view", "set", "--help"}, want: "factile view set <id> --title <title> --path <path> [--description <text>]"},
		{name: "bundle group", args: []string{"bundle", "--help"}, want: "factile bundle find|inspect"},
		{name: "bundle leaf", args: []string{"bundle", "inspect", "--help"}, want: "factile bundle inspect <source>"},
		{name: "skill group", args: []string{"skill", "--help"}, want: "factile skill list|inspect|install|uninstall|doctor"},
		{name: "skill leaf", args: []string{"skill", "install", "--help"}, want: "factile skill install codex --scope repo|user [--mode reader|curator] [--profile software]"},
		{name: "mcp", args: []string{"mcp", "--help"}, want: "factile mcp serve --stdio [--read-only]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(context.Background(), tc.args, nil, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("%v help exit code = %d stderr=%s", tc.args, code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("%v help wrote stderr: %s", tc.args, stderr.String())
			}
			help := strings.TrimSpace(stdout.String())
			if help != tc.want {
				t.Fatalf("%v help = %q, want %q", tc.args, help, tc.want)
			}
		})
	}
}

func TestCLIInvalidUsageStillFails(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"read"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("invalid read exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "factile read <document-path>" {
		t.Fatalf("unexpected invalid usage text: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("invalid usage wrote stderr: %s", stderr.String())
	}
}

func TestCLICreateRequiresTitle(t *testing.T) {
	mountFile := cliMountFile(t)
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("# Untitled\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "create", "/product-docs/workflows/untitled", "--type", "Workflow", "--body", body}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("create without title exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "factile create <document-path> --type <type> --title <title> --body <file>" {
		t.Fatalf("unexpected create usage: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("create without title wrote stderr: %s", stderr.String())
	}
}

func TestCLIJSONFlagMatchesFormatJSON(t *testing.T) {
	mountFile := cliMountFile(t)

	var jsonFlagOut, jsonFlagErr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--json"}, nil, &jsonFlagOut, &jsonFlagErr)
	if code != 0 {
		t.Fatalf("--json read exit code = %d stderr=%s", code, jsonFlagErr.String())
	}

	var formatOut, formatErr bytes.Buffer
	code = Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--format", "json"}, nil, &formatOut, &formatErr)
	if code != 0 {
		t.Fatalf("--format json read exit code = %d stderr=%s", code, formatErr.String())
	}

	var fromJSONFlag, fromFormat any
	if err := json.Unmarshal(jsonFlagOut.Bytes(), &fromJSONFlag); err != nil {
		t.Fatalf("--json did not return JSON: %v\n%s", err, jsonFlagOut.String())
	}
	if err := json.Unmarshal(formatOut.Bytes(), &fromFormat); err != nil {
		t.Fatalf("--format json did not return JSON: %v\n%s", err, formatOut.String())
	}
	if fmtJSON(fromJSONFlag) != fmtJSON(fromFormat) {
		t.Fatalf("--json output differed from --format json:\n--json=%s\n--format=%s", jsonFlagOut.String(), formatOut.String())
	}

	var trailingGlobalOut, trailingGlobalErr bytes.Buffer
	code = Run(context.Background(), []string{"read", "/product-docs/workflows/invoice-import", "--mount-file", mountFile, "--json"}, nil, &trailingGlobalOut, &trailingGlobalErr)
	if code != 0 {
		t.Fatalf("trailing global options read exit code = %d stderr=%s", code, trailingGlobalErr.String())
	}
	if fmtJSONBytes(t, trailingGlobalOut.Bytes()) != fmtJSON(fromJSONFlag) {
		t.Fatalf("trailing global option output differed:\ntrailing=%s\nbefore=%s", trailingGlobalOut.String(), jsonFlagOut.String())
	}

	var coloredJSONOut, coloredJSONErr bytes.Buffer
	code = Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--json", "--color", "always"}, nil, &coloredJSONOut, &coloredJSONErr)
	if code != 0 {
		t.Fatalf("--json --color always read exit code = %d stderr=%s", code, coloredJSONErr.String())
	}
	assertNoTerminalEscapes(t, coloredJSONOut.String())
}

func TestCLIPathShortcut(t *testing.T) {
	mountFile := cliMountFile(t)

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "root lists",
			args: []string{"/", "--color", "never"},
			want: []string{"/", "Folders:", "/product-docs  Product Docs"},
		},
		{
			name: "folder lists",
			args: []string{"/product-docs", "--color", "never"},
			want: []string{"/product-docs", "Folders:", "/product-docs/workflows  Workflows"},
		},
		{
			name: "document reads",
			args: []string{"/product-docs/workflows/invoice-import", "--color", "never"},
			want: []string{"/product-docs/workflows/invoice-import", "Invoice Import Workflow", "Supplier invoices are received"},
		},
		{
			name: "trailing slash document reads",
			args: []string{"/product-docs/workflows/invoice-import/", "--color", "never"},
			want: []string{"/product-docs/workflows/invoice-import", "Invoice Import Workflow", "Supplier invoices are received"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--mount-file", mountFile}, tc.args...)
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), args, nil, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("%s exit code = %d stdout=%s stderr=%s", tc.name, code, stdout.String(), stderr.String())
			}
			output := stdout.String()
			for _, want := range tc.want {
				if !strings.Contains(output, want) {
					t.Fatalf("%s missing %q:\n%s", tc.name, want, output)
				}
			}
			assertNotJSONText(t, output)
			assertNoTerminalEscapes(t, output)
		})
	}

	t.Run("folder json matches list shape", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"--mount-file", mountFile, "/product-docs", "--json"}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("path JSON list exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), `"path": "/product-docs"`) || !strings.Contains(stdout.String(), `"folders"`) || strings.Contains(stdout.String(), `"concept"`) {
			t.Fatalf("unexpected folder shortcut JSON:\n%s", stdout.String())
		}
	})

	t.Run("document json matches read shape", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"--mount-file", mountFile, "/product-docs/workflows/invoice-import", "--json"}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("path JSON read exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), `"concept"`) || !strings.Contains(stdout.String(), `"path": "/product-docs/workflows/invoice-import"`) || strings.Contains(stdout.String(), `"folders"`) {
			t.Fatalf("unexpected document shortcut JSON:\n%s", stdout.String())
		}
	})

	t.Run("missing path reports path not found", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"--mount-file", mountFile, "/product-docs/missing"}, nil, &stdout, &stderr)
		if code != 4 {
			t.Fatalf("missing path exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), "Path not found: /product-docs/missing") {
			t.Fatalf("unexpected missing path error:\n%s", stderr.String())
		}
	})
}

func TestCLIJSONAliasMatrix(t *testing.T) {
	mountFile := cliMountFile(t)
	cases := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"--mount-file", mountFile, "list", "/"}},
		{name: "read", args: []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import"}},
		{name: "search", args: []string{"--mount-file", mountFile, "search", "/product-docs", "invoice"}},
		{name: "validate", args: []string{"--mount-file", mountFile, "validate", "/product-docs"}},
		{name: "skill inspect", args: []string{"skill", "inspect", "codex"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var jsonOut, jsonErr bytes.Buffer
			code := Run(context.Background(), appendOutputArgs(tc.args, "--json"), nil, &jsonOut, &jsonErr)
			if code != 0 {
				t.Fatalf("--json exit code = %d stderr=%s", code, jsonErr.String())
			}
			var formatOut, formatErr bytes.Buffer
			code = Run(context.Background(), appendOutputArgs(tc.args, "--format", "json"), nil, &formatOut, &formatErr)
			if code != 0 {
				t.Fatalf("--format json exit code = %d stderr=%s", code, formatErr.String())
			}
			var fromJSONFlag, fromFormat any
			if err := json.Unmarshal(jsonOut.Bytes(), &fromJSONFlag); err != nil {
				t.Fatalf("--json did not return JSON: %v\n%s", err, jsonOut.String())
			}
			if err := json.Unmarshal(formatOut.Bytes(), &fromFormat); err != nil {
				t.Fatalf("--format json did not return JSON: %v\n%s", err, formatOut.String())
			}
			if fmtJSON(fromJSONFlag) != fmtJSON(fromFormat) {
				t.Fatalf("JSON aliases differed:\n--json=%s\n--format=%s", jsonOut.String(), formatOut.String())
			}
			assertNoTerminalEscapes(t, jsonOut.String())
			assertNoTerminalEscapes(t, formatOut.String())
		})
	}
}

func TestCLIJSONReaderContractShapes(t *testing.T) {
	mountFile := cliMountFile(t)

	list := runCLIJSON[factile.ListResult](t, "--mount-file", mountFile, "list", "/", "--json")
	if list.Path != "/" || !hasFolderPath(list.Folders, "/product-docs") || !hasFolderPath(list.Folders, "/broken-docs") {
		t.Fatalf("unexpected list contract: %#v", list)
	}
	if len(list.Mounts) != 0 || len(list.Concepts) != 0 || len(list.Paths) != 0 {
		t.Fatalf("list reader JSON leaked legacy fields: %#v", list)
	}

	read := runCLIJSON[factile.ConceptResult](t, "--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--json")
	if read.Concept.Path != "/product-docs/workflows/invoice-import" ||
		read.Concept.ConceptID != "workflows/invoice-import" ||
		!strings.HasPrefix(read.Concept.Revision, "sha256:") ||
		read.Concept.Frontmatter["type"] != "Workflow" ||
		read.Concept.Frontmatter["title"] != "Invoice Import Workflow" ||
		read.Concept.Frontmatter["resource"] != "factile:test/product-docs/workflows/invoice-import" ||
		!strings.Contains(read.Concept.Markdown, "Supplier invoices are received") {
		t.Fatalf("unexpected read contract: %#v", read.Concept)
	}

	search := runCLIJSON[factile.SearchResults](t, "--mount-file", mountFile, "search", "/product-docs", "invoice", "--json")
	if search.Path != "/product-docs" || search.Query != "invoice" || len(search.Results) == 0 {
		t.Fatalf("unexpected search contract: %#v", search)
	}
	first := search.Results[0]
	if first.Concept.Path != "/product-docs/workflows/invoice-import" ||
		first.Concept.Resource != "factile:test/product-docs/workflows/invoice-import" ||
		first.Score <= 0 ||
		!strings.Contains(first.Snippet, "Supplier invoices") {
		t.Fatalf("unexpected first search result: %#v", first)
	}

	contextPack := runCLIJSON[factile.ContextPack](t, "--mount-file", mountFile, "context", "/product-docs", "invoice import workflow", "--json")
	if contextPack.Path != "/product-docs" || contextPack.Query != "invoice import workflow" || !hasConceptPath(contextPack.Concepts, "/product-docs/workflows/invoice-import") {
		t.Fatalf("unexpected context contract: %#v", contextPack)
	}

	graph := runCLIJSON[factile.GraphResult](t, "--mount-file", mountFile, "graph", "/product-docs", "--json")
	if graph.Path != "/product-docs" ||
		!hasGraphNodePath(graph.Nodes, "/product-docs/workflows/invoice-import") ||
		!hasGraphEdge(graph.Edges, "/product-docs/workflows/invoice-import", "/product-docs/runbooks/ocr-failure", "markdown_link") {
		t.Fatalf("unexpected graph contract: %#v", graph)
	}

	validation := runCLIJSONWithCode[factile.ValidationResult](t, 3, "--mount-file", mountFile, "validate", "/broken-docs", "--json")
	if validation.Path != "/broken-docs" || validation.Valid || !hasValidationIssue(validation.Issues, "error", "missing_type", "/broken-docs/bad-frontmatter") {
		t.Fatalf("unexpected validation contract: %#v", validation)
	}

	workspace := cliV2Workspace(t)
	t.Chdir(workspace)
	brief := runCLIJSON[factile.ListResult](t, "list", "/", "--brief", "--json")
	if brief.Path != "/" || len(brief.Cards) == 0 || !hasCardPath(brief.Cards, "/engineering") || len(brief.Folders) != 0 || len(brief.Documents) != 0 {
		t.Fatalf("unexpected brief list contract: %#v", brief)
	}
	stat := runCLIJSON[factile.StatResult](t, "stat", "/engineering/django", "--json")
	if stat.Card.Path != "/engineering/django" || stat.Card.Title != "Django Product Docs" || stat.Card.WhenToUse != "Use when working on invoice import workflows or runbooks." || stat.Card.Writable == nil || !*stat.Card.Writable {
		t.Fatalf("unexpected stat contract: %#v", stat)
	}
}

func TestCLIJSONWriterContractShapes(t *testing.T) {
	mountFile := cliMountFile(t)
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("# Payment Import\n\nPayments are imported.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := "/product-docs/workflows/payment-import"

	mkdirPath := "/product-docs/guides"
	made := runCLIJSON[factile.DirectoryResult](t, "--mount-file", mountFile, "mkdir", mkdirPath, "--title", "Guides", "--bundle", "--json")
	if made.Directory.Path != mkdirPath || !made.Directory.Created || !hasString(made.Directory.Files, mkdirPath+"/index.md") || !hasString(made.Directory.Files, mkdirPath+"/log.md") || !hasString(made.Directory.Files, mkdirPath+"/overview.md") {
		t.Fatalf("unexpected mkdir contract: %#v", made.Directory)
	}
	overview := runCLIJSON[factile.ConceptResult](t, "--mount-file", mountFile, "read", mkdirPath+"/overview", "--json")
	if overview.Concept.Path != mkdirPath+"/overview" || overview.Concept.Frontmatter["title"] != "Guides Overview" {
		t.Fatalf("unexpected mkdir overview concept: %#v", overview.Concept)
	}

	created := runCLIJSON[factile.ConceptResult](t, "--mount-file", mountFile, "create", path, "--type", "Workflow", "--title", "Payment Import", "--body", body, "--json")
	if created.Concept.Path != path || created.Concept.Frontmatter["type"] != "Workflow" || created.Concept.Frontmatter["title"] != "Payment Import" || !strings.HasPrefix(created.Concept.Revision, "sha256:") {
		t.Fatalf("unexpected create contract: %#v", created.Concept)
	}

	if err := os.WriteFile(body, []byte("# Payment Import\n\nPayments are settled.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written := runCLIJSON[factile.ConceptResult](t, "--mount-file", mountFile, "write", path, "--rev", created.Concept.Revision, "--body", body, "--json")
	if written.Concept.Path != path || !strings.Contains(written.Concept.Markdown, "Payments are settled.") || written.Concept.Revision == created.Concept.Revision {
		t.Fatalf("unexpected write contract: %#v", written.Concept)
	}

	patched := runCLIJSON[factile.ConceptResult](t, "--mount-file", mountFile, "patch", path, "--rev", written.Concept.Revision, "--set", "status=draft", "--json")
	if patched.Concept.Path != path || patched.Concept.Frontmatter["status"] != "draft" || patched.Concept.Revision == written.Concept.Revision {
		t.Fatalf("unexpected patch contract: %#v", patched.Concept)
	}

	deleted := runCLIJSON[factile.DeleteResult](t, "--mount-file", mountFile, "delete", path, "--rev", patched.Concept.Revision, "--json")
	if deleted.Path != path || !deleted.Deleted {
		t.Fatalf("unexpected delete contract: %#v", deleted)
	}
}

func TestCLIJSONMountAndBundleContracts(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "django-docs")
	copyTestDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), source)
	t.Chdir(tmp)
	writeCLIRootConfig(t, ".")

	inspectBundle := runCLIJSON[factile.BundleInspectResult](t, "bundle", "inspect", source, "--json")
	if inspectBundle.Source != source || inspectBundle.Kind != "local" || !inspectBundle.PlausibleOKF || !hasConceptSummaryPath(inspectBundle.Concepts, "workflows/invoice-import") {
		t.Fatalf("unexpected bundle inspect contract: %#v", inspectBundle)
	}

	found := runCLIJSON[factile.BundleFindResult](t, "bundle", "find", tmp, "--json")
	if found.StartPath != tmp || !hasString(found.Sources, source) {
		t.Fatalf("unexpected bundle find contract: %#v", found)
	}

	pathMounted := runCLIJSON[factile.MountResult](t, "mount", source, "/docs", "--title", "Docs", "--description", "Project docs", "--read-only", "--json")
	if pathMounted.Mount.MountPath != "/docs" || pathMounted.Mount.Source != source || pathMounted.Mount.Writable || pathMounted.Mount.Title != "Docs" {
		t.Fatalf("unexpected mount contract: %#v", pathMounted)
	}
	descriptor, err := os.ReadFile(filepath.Join(tmp, "docs.mount.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(descriptor), `title = "Docs"`) || !strings.Contains(string(descriptor), "writable = false") {
		t.Fatalf("mount did not write descriptor metadata:\n%s", string(descriptor))
	}
	mounts := runCLIJSON[factile.MountListResult](t, "mounts", "--json")
	docsMount := findMount(mounts.Mounts, "/docs", source)
	if docsMount == nil || docsMount.Title != "Docs" || docsMount.Writable {
		t.Fatalf("unexpected mounts contract: %#v", mounts)
	}
	pathUnmounted := runCLIJSON[factile.UnmountResult](t, "unmount", "/docs", "--json")
	if pathUnmounted.MountPath != "/docs" || !pathUnmounted.Removed {
		t.Fatalf("unexpected unmount contract: %#v", pathUnmounted)
	}
	if _, err := os.Stat(filepath.Join(tmp, "docs.mount.toml")); !os.IsNotExist(err) {
		t.Fatalf("descriptor should be removed by unmount, err=%v", err)
	}

}

func TestCLIJSONViewContracts(t *testing.T) {
	workspace := cliV2Workspace(t)
	t.Chdir(workspace)
	workflowPath := "/engineering/django/workflows/invoice-import"
	runbookPath := "/engineering/django/runbooks/ocr-failure"
	legacyPath := "/legacy/notes/legacy"

	empty := runCLIJSON[factile.ViewListResult](t, "view", "list", "--json")
	if len(empty.Views) != 0 {
		t.Fatalf("unexpected initial view list: %#v", empty)
	}

	set := runCLIJSON[factile.ViewResult](t,
		"view", "set", "invoice",
		"--title", "Invoice",
		"--description", "Invoice workflow, runbooks, and legacy notes.",
		"--path", workflowPath,
		"--path", runbookPath,
		"--path", "/legacy",
		"--json",
	)
	if set.Action != "created" || set.View.ID != "invoice" || set.View.Title != "Invoice" || strings.Join(set.View.Paths, ",") != workflowPath+","+runbookPath+",/legacy" {
		t.Fatalf("unexpected view set contract: %#v", set)
	}

	inspected := runCLIJSON[factile.ViewResult](t, "view", "inspect", "invoice", "--json")
	if inspected.View.ID != "invoice" || len(inspected.View.Paths) != 3 {
		t.Fatalf("unexpected view inspect contract: %#v", inspected)
	}
	list := runCLIJSON[factile.ViewListResult](t, "view", "list", "--json")
	if len(list.Views) != 1 || list.Views[0].ID != "invoice" {
		t.Fatalf("unexpected view list contract: %#v", list)
	}

	viewList := runCLIJSON[factile.ListResult](t, "list", "/", "--view", "invoice", "--json")
	if !hasFolderPath(viewList.Folders, "/engineering") || !hasFolderPath(viewList.Folders, "/legacy") || hasFolderPath(viewList.Folders, "/support") {
		t.Fatalf("unexpected list --view contract: %#v", viewList)
	}

	search := runCLIJSON[factile.SearchResults](t, "search", "/", "legacy", "--view", "invoice", "--json")
	if !hasSearchResultPath(search.Results, legacyPath) || hasSearchResultPath(search.Results, "/engineering/common/guides/setup") {
		t.Fatalf("unexpected search --view contract: %#v", search)
	}

	contextPack := runCLIJSON[factile.ContextPack](t, "context", "/engineering", "posted", "--view", "invoice", "--json")
	if !hasConceptPath(contextPack.Concepts, workflowPath) || !hasConceptPath(contextPack.Concepts, runbookPath) || hasConceptPath(contextPack.Concepts, legacyPath) {
		t.Fatalf("unexpected context --view contract: %#v", contextPack)
	}

	graph := runCLIJSON[factile.GraphResult](t, "graph", "/engineering", "--view", "invoice", "--json")
	if !hasGraphNodePath(graph.Nodes, workflowPath) || !hasGraphNodePath(graph.Nodes, runbookPath) || !hasGraphEdge(graph.Edges, workflowPath, runbookPath, "markdown_link") || hasGraphNodePath(graph.Nodes, "/engineering/common/guides/setup") {
		t.Fatalf("unexpected graph --view contract: %#v", graph)
	}

	validated := runCLIJSON[factile.ValidationResult](t, "validate", "/engineering", "--view", "invoice", "--json")
	if !validated.Valid || len(validated.Issues) != 0 {
		t.Fatalf("unexpected validate --view contract: %#v", validated)
	}

	deleted := runCLIJSON[factile.ViewDeleteResult](t, "view", "delete", "invoice", "--json")
	if !deleted.Deleted || deleted.ID != "invoice" {
		t.Fatalf("unexpected view delete contract: %#v", deleted)
	}

	assertCLIJSONError(t, 4, factile.ErrMountNotFound, "View not found: missing", "list", "/", "--view", "missing", "--json")
	assertCLIJSONError(t, 4, factile.ErrMountNotFound, "View not found: missing", "view", "inspect", "missing", "--json")
}

func TestCLIJSONSkillContracts(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	list := runCLIJSON[skill.ListResult](t, "skill", "list", "--json")
	if len(list.Skills) != 1 || list.Skills[0].Target != "codex" || list.Skills[0].Name != "factile" {
		t.Fatalf("unexpected skill list contract: %#v", list)
	}

	inspect := runCLIJSON[skill.InspectResult](t, "skill", "inspect", "codex", "--json")
	if inspect.Target != "codex" || inspect.Name != "factile" || !hasString(inspect.Files, ".agents/skills/factile/SKILL.md") || !strings.Contains(inspect.SkillMarkdown, "Factile local knowledge workflow") || !strings.Contains(inspect.SkillMarkdown, ".factile/config.toml") || !strings.Contains(inspect.SkillMarkdown, "<name>.mount.toml") || !strings.Contains(inspect.SkillMarkdown, ".factile/views.toml") {
		t.Fatalf("unexpected skill inspect contract: %#v", inspect)
	}

	installed := runCLIJSON[skill.InstallResult](t, "skill", "install", "codex", "--scope", "repo", "--json")
	if installed.Target != "codex" || installed.Scope != "repo" || installed.Mode != "reader" || installed.Message == "" || !hasFileChange(installed.Files, filepath.Join(".agents", "skills", "factile", "SKILL.md")) {
		t.Fatalf("unexpected skill install contract: %#v", installed)
	}

	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeFactile := filepath.Join(binDir, "factile")
	if err := os.WriteFile(fakeFactile, []byte("#!/usr/bin/env bash\ncase \"$1\" in\nlist) echo '{\"path\":\"/\",\"mounts\":[]}' ;;\ncontext) echo '{\"path\":\"/\",\"query\":\"probe\",\"concepts\":[]}' ;;\n*) echo '{}' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	doctor := runCLIJSON[skill.DoctorResult](t, "skill", "doctor", "codex", "--json")
	if doctor.Target != "codex" || !doctor.OK || len(doctor.Checks) == 0 || !hasDoctorCheck(doctor.Checks, "factile_on_path", "pass") {
		t.Fatalf("unexpected skill doctor contract: %#v", doctor)
	}

	uninstalled := runCLIJSON[skill.UninstallResult](t, "skill", "uninstall", "codex", "--scope", "repo", "--json")
	if uninstalled.Target != "codex" || uninstalled.Scope != "repo" || !hasFileChange(uninstalled.Files, filepath.Join(".agents", "skills", "factile", "SKILL.md")) {
		t.Fatalf("unexpected skill uninstall contract: %#v", uninstalled)
	}
}

func TestCLIJSONErrorContractShapes(t *testing.T) {
	mountFile := cliMountFile(t)
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("# Updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	assertCLIJSONError(t, 2, factile.ErrUnsupportedCommand, "Unsupported command: --verbose", "--verbose", "--json")
	assertCLIJSONError(t, 4, factile.ErrConceptNotFound, "Concept not found: /product-docs/missing", "--mount-file", mountFile, "read", "/product-docs/missing", "--json")
	assertCLIJSONError(t, 5, factile.ErrRevisionRequired, "Expected revision is required", "--mount-file", mountFile, "write", "/product-docs/workflows/invoice-import", "--body", body, "--json")

}

func TestCLITextModeNoJSONFallbackMatrix(t *testing.T) {
	mountFile := cliMountFile(t)
	source := filepath.Join("..", "..", "testdata", "bundles", "product-docs")
	cases := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"--mount-file", mountFile, "list", "/", "--color", "never"}},
		{name: "brief list", args: []string{"--mount-file", mountFile, "list", "/", "--brief", "--color", "never"}},
		{name: "stat", args: []string{"--mount-file", mountFile, "stat", "/product-docs", "--color", "never"}},
		{name: "read", args: []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--color", "never"}},
		{name: "search", args: []string{"--mount-file", mountFile, "search", "/product-docs", "invoice", "--color", "never"}},
		{name: "context", args: []string{"--mount-file", mountFile, "context", "/product-docs", "invoice import", "--color", "never"}},
		{name: "graph", args: []string{"--mount-file", mountFile, "graph", "/product-docs", "--color", "never"}},
		{name: "validate", args: []string{"--mount-file", mountFile, "validate", "/product-docs", "--color", "never"}},
		{name: "bundle inspect", args: []string{"bundle", "inspect", source, "--color", "never"}},
		{name: "bundle find", args: []string{"bundle", "find", filepath.Join("..", "..", "testdata", "bundles"), "--color", "never"}},
		{name: "skill list", args: []string{"skill", "list", "--color", "never"}},
		{name: "skill inspect", args: []string{"skill", "inspect", "codex", "--color", "never"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tc.args, nil, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			assertNotJSONText(t, stdout.String())
			assertNoTerminalEscapes(t, stdout.String())
		})
	}

	t.Run("init", func(t *testing.T) {
		tmp := t.TempDir()
		t.Chdir(tmp)
		if err := os.MkdirAll(".codex", 0o755); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"init", "--color", "never"}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("init exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		assertNotJSONText(t, stdout.String())
		assertNoTerminalEscapes(t, stdout.String())
	})
}

func TestCLIColorFlagsAffectTextOnly(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	mountFile := cliMountFile(t)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--color", "always"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("color always read exit code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\x1b") {
		t.Fatalf("--color always text output did not contain terminal escapes:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("color never read exit code = %d stderr=%s", code, stderr.String())
	}
	assertNoTerminalEscapes(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--json", "--color", "always"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("json color always read exit code = %d stderr=%s", code, stderr.String())
	}
	assertNoTerminalEscapes(t, stdout.String())
}

func TestCLIOutputModeAndColorValidation(t *testing.T) {
	mountFile := cliMountFile(t)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--format", "text"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--format text read exit code = %d stderr=%s", code, stderr.String())
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Fatalf("--format text emitted JSON:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "--json", "--color", "sepia", "list", "/"}, nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `"code":"invalid_path"`) || !strings.Contains(stderr.String(), "unsupported color mode: sepia") {
		t.Fatalf("expected structured invalid color error, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--json", "--format", "text", "list", "/"}, nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `"code":"invalid_path"`) || !strings.Contains(stderr.String(), "--json cannot be combined with --format text") {
		t.Fatalf("expected structured output conflict error, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLIJSONAndTextErrors(t *testing.T) {
	mountFile := cliMountFile(t)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/missing", "--json", "--color", "always"}, nil, &stdout, &stderr)
	if code != 4 || !strings.Contains(stderr.String(), `"code":"concept_not_found"`) {
		t.Fatalf("expected structured missing concept error, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	assertNoTerminalEscapes(t, stderr.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/missing"}, nil, &stdout, &stderr)
	if code != 4 || strings.Contains(stderr.String(), `"code"`) || !strings.Contains(stderr.String(), "Concept not found") {
		t.Fatalf("expected text missing concept error, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLIExitCodeMappingsAndJSONErrors(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{code: factile.ErrInvalidPath, want: 2},
		{code: factile.ErrUnsupportedCommand, want: 2},
		{code: factile.ErrValidationFailed, want: 3},
		{code: factile.ErrOKFParse, want: 3},
		{code: factile.ErrMountNotFound, want: 4},
		{code: factile.ErrAmbiguousTarget, want: 4},
		{code: factile.ErrConceptNotFound, want: 4},
		{code: factile.ErrPathIsNotBundle, want: 4},
		{code: factile.ErrPathIsNotConcept, want: 4},
		{code: factile.ErrConceptAlreadyExist, want: 5},
		{code: factile.ErrPathAlreadyExists, want: 5},
		{code: factile.ErrRevisionRequired, want: 5},
		{code: factile.ErrRevisionMismatch, want: 5},
		{code: factile.ErrSectionNotFound, want: 5},
		{code: factile.ErrSourceReadOnly, want: 6},
		{code: factile.ErrUnsafeSourcePath, want: 6},
		{code: factile.ErrUnsupportedSource, want: 6},
		{code: factile.ErrPartialFailure, want: 7},
		{code: factile.ErrLockTimeout, want: 8},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			var stderr bytes.Buffer
			got := writeError(&stderr, globals{Format: formatJSON}, factile.NewError(tc.code, "message"))
			if got != tc.want {
				t.Fatalf("exitCode(%s) = %d, want %d", tc.code, got, tc.want)
			}
			var payload struct {
				Error factile.AppError `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
				t.Fatalf("error JSON did not parse: %v\n%s", err, stderr.String())
			}
			if payload.Error.Code != tc.code || payload.Error.Message != "message" {
				t.Fatalf("unexpected error JSON: %#v", payload.Error)
			}
		})
	}
}

func TestCLINormalizedErrorExitCodeMappings(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
		want int
	}{
		{
			name: "lock timeout",
			err:  errors.New("lock timeout while waiting for .factile.lock"),
			code: factile.ErrLockTimeout,
			want: 8,
		},
		{
			name: "storage lock timeout",
			err:  fmt.Errorf("%w: waiting for .factile.lock", storage.ErrLockTimeout),
			code: factile.ErrLockTimeout,
			want: 8,
		},
		{
			name: "unsafe source path",
			err:  fmt.Errorf("%w: ../outside", storage.ErrUnsafePath),
			code: factile.ErrUnsafeSourcePath,
			want: 6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got := writeError(&stderr, globals{Format: formatJSON}, tc.err)
			if got != tc.want {
				t.Fatalf("%s exit code = %d, want %d", tc.name, got, tc.want)
			}
			if !strings.Contains(stderr.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("expected %s JSON error, got %s", tc.code, stderr.String())
			}
		})
	}
}

func TestCLIWriteContractErrorExitCodes(t *testing.T) {
	mountFile := cliMountFile(t)
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("# Updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := "/product-docs/workflows/invoice-import"

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "write", path, "--body", body, "--json"}, nil, &stdout, &stderr)
	if code != 5 || !strings.Contains(stderr.String(), `"code":"revision_required"`) {
		t.Fatalf("expected revision_required exit 5, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "create", path, "--type", "Workflow", "--title", "Invoice Import", "--body", body, "--json"}, nil, &stdout, &stderr)
	if code != 5 || !strings.Contains(stderr.String(), `"code":"concept_already_exists"`) {
		t.Fatalf("expected concept_already_exists exit 5, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "mkdir", "/product-docs/workflows", "--json"}, nil, &stdout, &stderr)
	if code != 5 || !strings.Contains(stderr.String(), `"code":"path_already_exists"`) {
		t.Fatalf("expected path_already_exists exit 5, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	rev := cliRevision(t, mountFile, path)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "patch", path, "--rev", rev, "--replace-section", "Does Not Exist", body, "--json"}, nil, &stdout, &stderr)
	if code != 5 || !strings.Contains(stderr.String(), `"code":"section_not_found"`) {
		t.Fatalf("expected section_not_found exit 5, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLIValidateInvalidExitsThree(t *testing.T) {
	mountFile := cliMountFile(t)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "validate", "/broken-docs", "--format", "json"}, nil, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("validate exit code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"valid": false`) {
		t.Fatalf("unexpected validation JSON: %s", stdout.String())
	}
}

func TestCLIListDefaultsToRoot(t *testing.T) {
	mountFile := cliMountFile(t)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "list", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list exit code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"path": "/"`) || !strings.Contains(stdout.String(), `"folders"`) || strings.Contains(stdout.String(), `"mount_path"`) {
		t.Fatalf("unexpected list JSON: %s", stdout.String())
	}
}

func TestCLIExplicitRoot(t *testing.T) {
	workspace := cliV2Workspace(t)
	tmp := t.TempDir()
	t.Chdir(tmp)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--root", workspace, "list", "/", "--brief", "--json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("explicit root list exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"/engineering"`) {
		t.Fatalf("explicit root list missing engineering card:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--root", filepath.Join(tmp, "missing-root"), "list", "/", "--json"}, nil, &stdout, &stderr)
	if code != 4 || !strings.Contains(stderr.String(), `"code":"no_active_root"`) || !strings.Contains(stderr.String(), "factile init") {
		t.Fatalf("missing explicit root should fail with no_active_root, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLISkillInstallRepoIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	if err := os.WriteFile("AGENTS.md", []byte("# Existing guidance\n\nKeep this section.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"skill", "install", "codex", "--scope", "repo", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit code = %d stderr=%s", code, stderr.String())
	}
	for _, filename := range []string{
		filepath.Join(".agents", "skills", "factile", "SKILL.md"),
		filepath.Join(".agents", "skills", "factile", "scripts", "factile-discover.sh"),
		filepath.Join(".codex", "config.toml"),
	} {
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("expected %s: %v", filename, err)
		}
	}
	agents, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "Keep this section.") || strings.Count(string(agents), "<!-- factile:codex:start -->") != 1 {
		t.Fatalf("unexpected AGENTS.md after install:\n%s", string(agents))
	}
	skillFile, err := os.ReadFile(filepath.Join(".agents", "skills", "factile", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skillFile), "Reader mode is installed") ||
		!strings.Contains(string(skillFile), "Reader commands work on paths") ||
		!strings.Contains(string(skillFile), ".factile/config.toml") ||
		!strings.Contains(string(skillFile), "<name>.mount.toml") ||
		!strings.Contains(string(skillFile), ".factile/views.toml") ||
		!strings.Contains(string(skillFile), "Use a narrower path or `--view <id>` when the task scope is specific.") ||
		!strings.Contains(string(skillFile), "factile context / '<task>' --json") ||
		strings.Contains(string(skillFile), "--format json") ||
		strings.Contains(string(skillFile), "Knowledge Base") ||
		strings.Contains(string(skillFile), ".factile/mounts.toml") ||
		strings.Contains(string(skillFile), "`factile kb") {
		t.Fatalf("reader mode guidance missing:\n%s", string(skillFile))
	}
	discoverScript, err := os.ReadFile(filepath.Join(".agents", "skills", "factile", "scripts", "factile-discover.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(discoverScript), "factile list / --json") || strings.Contains(string(discoverScript), "--format json") {
		t.Fatalf("discover script should prefer --json:\n%s", string(discoverScript))
	}
	if !strings.Contains(string(agents), "factile context / '<task summary>' --json") || !strings.Contains(string(agents), ".factile/views.toml") || strings.Contains(string(agents), "--format json") || strings.Contains(string(agents), "Knowledge Base") || strings.Contains(string(agents), ".factile/mounts.toml") {
		t.Fatalf("AGENTS managed block should prefer --json:\n%s", string(agents))
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "install", "codex", "--scope", "repo", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second install exit code = %d stderr=%s", code, stderr.String())
	}
	agents, err = os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(agents), "<!-- factile:codex:start -->") != 1 {
		t.Fatalf("managed block duplicated:\n%s", string(agents))
	}
	config, err := os.ReadFile(filepath.Join(".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "[mcp_servers.factile]") {
		t.Fatalf("missing MCP config:\n%s", string(config))
	}
	if !strings.Contains(string(config), `"--read-only"`) {
		t.Fatalf("default reader install should configure read-only MCP:\n%s", string(config))
	}
}

func TestCLISkillInstallCuratorModeWithProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"skill", "install", "codex", "--scope", "repo", "--mode", "curator", "--profile", "software", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("curator install exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"mode": "curator"`) || !strings.Contains(stdout.String(), `"profile": "software"`) {
		t.Fatalf("curator install JSON missing mode/profile:\n%s", stdout.String())
	}
	skillFile, err := os.ReadFile(filepath.Join(".agents", "skills", "factile", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skillFile), "Curator mode is installed") || !strings.Contains(string(skillFile), "Profile: `software`") || !strings.Contains(string(skillFile), "factile mount") || !strings.Contains(string(skillFile), ".factile/views.toml") {
		t.Fatalf("curator/profile guidance missing:\n%s", string(skillFile))
	}
	agents, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "Mode: curator") || !strings.Contains(string(agents), "Profile: `software`") {
		t.Fatalf("AGENTS.md curator guidance missing:\n%s", string(agents))
	}
	config, err := os.ReadFile(filepath.Join(".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "--read-only") {
		t.Fatalf("curator install should not configure read-only MCP:\n%s", string(config))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "install", "codex", "--scope", "repo", "--profile", "unknown", "--format", "json"}, nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `"code":"invalid_path"`) {
		t.Fatalf("expected unsupported profile failure, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLISkillTextOutput(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"skill", "list", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("skill list text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Agent skills:") || !strings.Contains(stdout.String(), "codex/factile") {
		t.Fatalf("skill list text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "inspect", "codex", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("skill inspect text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Skill codex/factile") || !strings.Contains(stdout.String(), "Files:") || !strings.Contains(stdout.String(), "Generated content:") {
		t.Fatalf("skill inspect text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "install", "codex", "--scope", "repo", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("skill install text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Installed codex skill (repo)") || !strings.Contains(stdout.String(), "Files:") || !strings.Contains(stdout.String(), ".agents/skills/factile/SKILL.md") {
		t.Fatalf("skill install text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeFactile := filepath.Join(binDir, "factile")
	if err := os.WriteFile(fakeFactile, []byte("#!/usr/bin/env bash\ncase \"$1\" in\nlist) echo '{\"path\":\"/\",\"mounts\":[]}' ;;\ncontext) echo '{\"path\":\"/\",\"query\":\"probe\",\"concepts\":[]}' ;;\n*) echo '{}' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "doctor", "codex", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("skill doctor text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"Skill doctor codex: OK", "Checks:", "PASS factile_on_path", "PASS factile_list_root", "factile list / returned JSON", "PASS factile_context_root"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("skill doctor text missing %q:\n%s", want, stdout.String())
		}
	}
	assertNotJSONText(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "uninstall", "codex", "--scope", "repo", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("skill uninstall text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Uninstalled codex skill (repo)") || !strings.Contains(stdout.String(), "Files:") {
		t.Fatalf("skill uninstall text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())
}

func TestCLIInitCreatesDefaultKnowledgeAndDetectsCodex(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	if err := os.MkdirAll(".codex", 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, filename := range []string{
		filepath.Join("docs", ".factile", "config.toml"),
		filepath.Join("docs", "index.md"),
		filepath.Join("docs", "log.md"),
		filepath.Join("docs", "overview.md"),
		filepath.Join(".agents", "skills", "factile", "SKILL.md"),
		filepath.Join(".codex", "config.toml"),
		"AGENTS.md",
	} {
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("expected %s: %v", filename, err)
		}
	}
	config, err := os.ReadFile(filepath.Join("docs", ".factile", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "version = 1") || !strings.Contains(string(config), "[defaults]") {
		t.Fatalf("unexpected root config:\n%s", string(config))
	}
	for _, filename := range []string{
		filepath.Join(".factile", "knowledge"),
		filepath.Join(".factile", "library.toml"),
		filepath.Join(".factile", "knowledge-bases", "project.toml"),
		filepath.Join(".factile", "mounts.toml"),
	} {
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Fatalf("fresh v2 init should not create %s, err=%v", filename, err)
		}
	}

	var result struct {
		RootPath string `json:"root_path"`
		Files    []struct {
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"files"`
		Agents []struct {
			Agent string `json:"agent"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("init JSON did not parse: %v\n%s", err, stdout.String())
	}
	if result.RootPath != "docs" || !hasInitFile(result.Files, filepath.Join("docs", ".factile", "config.toml"), "created") || !hasInitFile(result.Files, filepath.Join("docs", "overview.md"), "created") {
		t.Fatalf("unexpected init JSON: %#v\n%s", result, stdout.String())
	}
	if len(result.Agents) != 1 || result.Agents[0].Agent != "codex" {
		t.Fatalf("expected detected codex agent in init JSON: %#v\n%s", result.Agents, stdout.String())
	}
}

func TestCLIInitTextFirstRunAndRepeated(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	if err := os.MkdirAll(".codex", 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	first := stdout.String()
	for _, want := range []string{
		"Initialized Factile knowledge",
		"Root:           docs",
		"Config:         docs/.factile/config.toml (created)",
		"Index:          docs/index.md (created)",
		"Log:            docs/log.md (created)",
		"Overview:       docs/overview.md (created)",
		"Agent guidance: Codex reader mode (detected, installed)",
		"Next:",
		"factile list /",
		"factile read /overview",
		"factile context / \"what should I know?\"",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("first init text missing %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "{") || strings.Contains(first, "\x1b[") {
		t.Fatalf("first init text should not be JSON or ANSI:\n%s", first)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"init", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("repeated init text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	repeated := stdout.String()
	for _, want := range []string{
		"Factile knowledge is ready",
		"Root:           docs",
		"Config:         docs/.factile/config.toml (reused)",
		"Index:          docs/index.md (reused)",
		"Log:            docs/log.md (reused)",
		"Overview:       docs/overview.md (reused)",
		"Agent guidance: Codex reader mode (detected, already installed)",
	} {
		if !strings.Contains(repeated, want) {
			t.Fatalf("repeated init text missing %q:\n%s", want, repeated)
		}
	}
}

func TestCLIInitFromChildUsesExistingRoot(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "project")
	root := filepath.Join(parent, "docs")
	child := filepath.Join(parent, "src", "nested")
	if err := os.MkdirAll(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIRootConfig(t, root)
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init from child exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, filename := range []string{
		filepath.Join(root, ".factile", "config.toml"),
		filepath.Join(root, "index.md"),
		filepath.Join(root, "log.md"),
		filepath.Join(root, "overview.md"),
	} {
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("expected existing root file %s: %v", filename, err)
		}
	}
	if _, err := os.Stat(filepath.Join(child, ".factile")); !os.IsNotExist(err) {
		t.Fatalf("child should not get nested .factile, err=%v", err)
	}
}

func TestCLIInitHereAndExplicitAgent(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", "--here", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --here text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	here := stdout.String()
	for _, want := range []string{
		"Initialized Factile knowledge",
		"Root:           .",
		"Config:         .factile/config.toml (created)",
		"Index:          index.md (created)",
		"Log:            log.md (created)",
		"Overview:       overview.md (created)",
		"Agent guidance: none detected",
	} {
		if !strings.Contains(here, want) {
			t.Fatalf("init --here text missing %q:\n%s", want, here)
		}
	}
	for _, filename := range []string{
		filepath.Join(".factile", "config.toml"),
		"index.md",
		"log.md",
		"overview.md",
	} {
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("expected --here file %s: %v", filename, err)
		}
	}
	if _, err := os.Stat("docs"); !os.IsNotExist(err) {
		t.Fatalf("init --here should not create docs directory, err=%v", err)
	}
	for _, filename := range []string{
		filepath.Join(".factile", "library.toml"),
		filepath.Join(".factile", "knowledge-bases", "project.toml"),
		filepath.Join(".factile", "mounts.toml"),
	} {
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Fatalf("fresh v2 init --here should not create %s, err=%v", filename, err)
		}
	}

	agentDir := t.TempDir()
	t.Chdir(agentDir)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"init", "--agent", "codex", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("explicit agent init text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	explicit := stdout.String()
	if !strings.Contains(explicit, "Agent guidance: Codex reader mode (installed)") {
		t.Fatalf("explicit agent init text missing installed guidance:\n%s", explicit)
	}
}

func TestCLIInitHereJSONWithoutDetectedAgent(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", "--here", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(".agents", "skills", "factile", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected agent skill file err=%v", err)
	}
	var result struct {
		RootPath string `json:"root_path"`
		Files    []struct {
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"files"`
		Agents []struct {
			Agent string `json:"agent"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("init JSON did not parse: %v\n%s", err, stdout.String())
	}
	if result.RootPath != "." || !hasInitFile(result.Files, ".factile/config.toml", "created") || !hasInitFile(result.Files, "overview.md", "created") || len(result.Agents) != 0 {
		t.Fatalf("unexpected init --here JSON: %#v\n%s", result, stdout.String())
	}
}

func TestCLIBriefListAndStat(t *testing.T) {
	workspace := cliV2Workspace(t)
	t.Chdir(workspace)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"list", "/", "--brief", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("brief list exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"cards"`) || strings.Contains(stdout.String(), `"source"`) || strings.Contains(stdout.String(), `"folders"`) {
		t.Fatalf("unexpected brief list JSON:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"stat", "/engineering/django", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("stat exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"when_to_use": "Use when working on invoice import workflows or runbooks."`) || !strings.Contains(stdout.String(), `"writable": true`) || strings.Contains(stdout.String(), `"source"`) {
		t.Fatalf("unexpected stat JSON:\n%s", stdout.String())
	}
}

func TestCLIReaderTextOutput(t *testing.T) {
	mountFile := cliMountFile(t)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "list", "/", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	listOutput := stdout.String()
	for _, want := range []string{"/", "Folders:", "/broken-docs  Broken Docs", "/product-docs  Product Docs"} {
		if !strings.Contains(listOutput, want) {
			t.Fatalf("list text missing %q:\n%s", want, listOutput)
		}
	}
	assertNotJSONText(t, listOutput)

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "read", "/product-docs/workflows/invoice-import", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("read text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	readOutput := stdout.String()
	for _, want := range []string{
		"/product-docs/workflows/invoice-import",
		"Title:",
		"Invoice Import Workflow",
		"Type:",
		"Workflow",
		"Tags:",
		"invoices, suppliers, ocr",
		"Rev:",
		"Supplier invoices are received",
	} {
		if !strings.Contains(readOutput, want) {
			t.Fatalf("read text missing %q:\n%s", want, readOutput)
		}
	}
	if strings.Contains(readOutput, "---") {
		t.Fatalf("read text should not include raw frontmatter:\n%s", readOutput)
	}
	if strings.Contains(readOutput, "\x1b") {
		t.Fatalf("read text should not include terminal escapes with --color never:\n%q", readOutput)
	}
	assertNotJSONText(t, readOutput)
}

func TestCLIReaderBriefStatAndMountedSourceTextOutput(t *testing.T) {
	workspace := cliV2Workspace(t)
	t.Chdir(workspace)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"list", "/", "--brief", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("brief list text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	briefOutput := stdout.String()
	for _, want := range []string{"/engineering", "Title:", "Engineering"} {
		if !strings.Contains(briefOutput, want) {
			t.Fatalf("brief list text missing %q:\n%s", want, briefOutput)
		}
	}
	assertNotJSONText(t, briefOutput)

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"stat", "/engineering/django", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("stat text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	statOutput := stdout.String()
	for _, want := range []string{"/engineering/django", "Title:", "Django Product Docs", "When To Use:", "Writable:", "true"} {
		if !strings.Contains(statOutput, want) {
			t.Fatalf("stat text missing %q:\n%s", want, statOutput)
		}
	}
	assertNotJSONText(t, statOutput)

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"list", "/engineering", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mounted source list text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	groupOutput := stdout.String()
	for _, want := range []string{"/engineering/django", "/engineering/common", "/engineering/playbook"} {
		if !strings.Contains(groupOutput, want) {
			t.Fatalf("mounted source list text missing %q:\n%s", want, groupOutput)
		}
	}
	assertNotJSONText(t, groupOutput)
}

func TestCLIReaderQueryGraphValidateTextOutput(t *testing.T) {
	mountFile := cliMountFile(t)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "search", "/product-docs", "invoice", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("search text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	searchOutput := stdout.String()
	for _, want := range []string{"Search /product-docs", "Query: invoice", "1. 51.00 /product-docs/workflows/invoice-import", "Invoice Import Workflow", "Supplier invoices"} {
		if !strings.Contains(searchOutput, want) {
			t.Fatalf("search text missing %q:\n%s", want, searchOutput)
		}
	}
	assertNotJSONText(t, searchOutput)

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "context", "/product-docs", "invoice import workflow", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("context text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	contextOutput := stdout.String()
	for _, want := range []string{"Context /product-docs", "Query: invoice import workflow", "/product-docs/workflows/invoice-import", "Invoice Import Workflow", "Supplier invoices are received", "/product-docs/runbooks/ocr-failure"} {
		if !strings.Contains(contextOutput, want) {
			t.Fatalf("context text missing %q:\n%s", want, contextOutput)
		}
	}
	if strings.Contains(contextOutput, "\x1b") {
		t.Fatalf("context text should not include terminal escapes with --color never:\n%q", contextOutput)
	}
	assertNotJSONText(t, contextOutput)

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "graph", "/product-docs", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("graph text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	graphOutput := stdout.String()
	for _, want := range []string{"Graph /product-docs", "Nodes:", "/product-docs/workflows/invoice-import", "Edges:", "/product-docs/workflows/invoice-import -> /product-docs/runbooks/ocr-failure (markdown_link)"} {
		if !strings.Contains(graphOutput, want) {
			t.Fatalf("graph text missing %q:\n%s", want, graphOutput)
		}
	}
	assertNotJSONText(t, graphOutput)

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "validate", "/broken-docs", "--color", "never"}, nil, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("validate text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	validateOutput := stdout.String()
	for _, want := range []string{"invalid /broken-docs", "Errors:", "/broken-docs/bad-frontmatter", "[missing_type]", "Concept frontmatter must include non-empty type"} {
		if !strings.Contains(validateOutput, want) {
			t.Fatalf("validate text missing %q:\n%s", want, validateOutput)
		}
	}
	assertNotJSONText(t, validateOutput)
}

func TestCLIRejectsUnsupportedDepth(t *testing.T) {
	mountFile := cliMountFile(t)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "context", "/product-docs", "invoice import workflow", "--depth", "2", "--json"}, nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `"code":"invalid_path"`) || !strings.Contains(stderr.String(), "Depth must be 0 or 1") {
		t.Fatalf("expected context depth rejection, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "graph", "/product-docs/workflows/invoice-import", "--depth", "2", "--json"}, nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `"code":"invalid_path"`) || !strings.Contains(stderr.String(), "Depth must be 0 or 1") {
		t.Fatalf("expected graph depth rejection, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLIReaderMountedSourceQueryGraphTextOutput(t *testing.T) {
	workspace := cliV2Workspace(t)
	t.Chdir(workspace)

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "search",
			args: []string{"search", "/engineering", "setup", "--color", "never"},
			want: []string{"Search /engineering", "/engineering/common/guides/setup", "/engineering/playbook/guides/setup"},
		},
		{
			name: "context",
			args: []string{"context", "/engineering", "setup", "--color", "never"},
			want: []string{"Context /engineering", "/engineering/common/guides/setup", "/engineering/playbook/guides/setup"},
		},
		{
			name: "graph",
			args: []string{"graph", "/engineering", "--color", "never"},
			want: []string{"Graph /engineering", "/engineering/django/workflows/invoice-import", "/engineering/common/guides/setup", "/engineering/playbook/guides/setup"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tc.args, nil, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("%s text exit code = %d stdout=%s stderr=%s", tc.name, code, stdout.String(), stderr.String())
			}
			output := stdout.String()
			for _, want := range tc.want {
				if !strings.Contains(output, want) {
					t.Fatalf("%s text missing %q:\n%s", tc.name, want, output)
				}
			}
			assertNotJSONText(t, output)
		})
	}
}

func TestCLIWriteCommandTextOutput(t *testing.T) {
	mountFile := cliMountFile(t)
	tmp := t.TempDir()
	body := filepath.Join(tmp, "body.md")
	if err := os.WriteFile(body, []byte("# Payment Import\n\nPayments are imported.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := "/product-docs/workflows/payment-import"

	var stdout, stderr bytes.Buffer
	mkdirPath := "/product-docs/guides"
	code := Run(context.Background(), []string{"--mount-file", mountFile, "mkdir", mkdirPath, "--title", "Guides", "--bundle", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mkdir text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"Created directory " + mkdirPath, "Files:", mkdirPath + "/index.md", mkdirPath + "/log.md", mkdirPath + "/overview.md"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("mkdir text missing %q:\n%s", want, stdout.String())
		}
	}
	assertNotJSONText(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "create", path, "--type", "Workflow", "--title", "Payment Import", "--body", body, "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("create text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Created "+path) || !strings.Contains(stdout.String(), "Rev: sha256:") {
		t.Fatalf("create text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	rev := cliRevision(t, mountFile, path)
	if err := os.WriteFile(body, []byte("# Payment Import\n\nPayments are settled.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "write", path, "--rev", rev, "--body", body, "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("write text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Wrote "+path) {
		t.Fatalf("write text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	rev = cliRevision(t, mountFile, path)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "patch", path, "--rev", rev, "--set", "status=draft", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("patch text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Patched "+path) {
		t.Fatalf("patch text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	rev = cliRevision(t, mountFile, path)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "deprecate", path, "--rev", rev, "--reason", "superseded", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("deprecate text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Deprecated "+path) {
		t.Fatalf("deprecate text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	renameRev := cliRevision(t, mountFile, "/product-docs/workflows/invoice-import")
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "rename", "/product-docs/workflows/invoice-import", "/product-docs/workflows/invoice-import-v2", "--rev", renameRev, "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("rename text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Renamed /product-docs/workflows/invoice-import-v2") || !strings.Contains(stdout.String(), "Warnings:") {
		t.Fatalf("rename text should include warning summary:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())

	rev = cliRevision(t, mountFile, path)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "delete", path, "--rev", rev, "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("delete text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Deleted "+path) {
		t.Fatalf("delete text unexpected:\n%s", stdout.String())
	}
	assertNotJSONText(t, stdout.String())
}

func TestCLICuratorAndBundleTextOutput(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "django-docs")
	copyTestDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), source)
	t.Chdir(tmp)
	writeCLIRootConfig(t, ".")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"bundle", "inspect", source, "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bundle inspect text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Bundle "+source) || !strings.Contains(stdout.String(), "Plausible OKF: true") || !strings.Contains(stdout.String(), "Documents:") {
		t.Fatalf("bundle inspect text unexpected:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"bundle", "find", ".", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bundle find text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "OKF bundles under .:") || !strings.Contains(stdout.String(), source) {
		t.Fatalf("bundle find text unexpected:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"mount", source, "/docs", "--title", "Docs", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mount text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Mounted") || !strings.Contains(stdout.String(), "at /docs") {
		t.Fatalf("mount text unexpected:\n%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tmp, "docs.mount.toml")); err != nil {
		t.Fatalf("expected mount descriptor: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"mounts", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mounts text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Mounts:") || !strings.Contains(stdout.String(), "/docs") {
		t.Fatalf("mounts text unexpected:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"unmount", "/docs", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unmount text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Unmounted /docs") {
		t.Fatalf("unmount text unexpected:\n%s", stdout.String())
	}
}

func TestCLIViewCommandsTextOutput(t *testing.T) {
	workspace := cliV2Workspace(t)
	t.Chdir(workspace)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"view", "set", "invoice", "--title", "Invoice", "--description", "Invoice docs", "--path", "/engineering/django/workflows/invoice-import", "--path", "/legacy", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("view set text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"Created View invoice", "Title:", "Invoice", "Description:", "Invoice docs", "Paths:", "/engineering/django/workflows/invoice-import", "/legacy"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("view set text missing %q:\n%s", want, stdout.String())
		}
	}
	assertNotJSONText(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"view", "list", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("view list text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Views:") || !strings.Contains(stdout.String(), "invoice  Invoice") {
		t.Fatalf("view list text unexpected:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"view", "inspect", "invoice", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("view inspect text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "View invoice") || !strings.Contains(stdout.String(), "/legacy") {
		t.Fatalf("view inspect text unexpected:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"list", "/", "--view", "invoice", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list --view text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "/engineering") || !strings.Contains(stdout.String(), "/legacy") || strings.Contains(stdout.String(), "/engineering/common") {
		t.Fatalf("list --view text unexpected:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"view", "delete", "invoice", "--color", "never"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("view delete text exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Deleted View invoice") {
		t.Fatalf("view delete text unexpected:\n%s", stdout.String())
	}
}

func TestCLISkillDoctorJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"skill", "install", "codex", "--scope", "repo", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit code = %d stderr=%s", code, stderr.String())
	}
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeFactile := filepath.Join(binDir, "factile")
	if err := os.WriteFile(fakeFactile, []byte("#!/usr/bin/env bash\ncase \"$1\" in\nlist) echo '{\"path\":\"/\",\"mounts\":[]}' ;;\ncontext) echo '{\"path\":\"/\",\"query\":\"probe\",\"concepts\":[]}' ;;\n*) echo '{}' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"skill", "doctor", "codex", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("doctor did not return JSON: %v\n%s", err, stdout.String())
	}
	if !result.OK {
		t.Fatalf("doctor result was not OK: %s", stdout.String())
	}
}

func TestCLITraceFile(t *testing.T) {
	mountFile := cliMountFile(t)
	traceFile := filepath.Join(t.TempDir(), "usage.jsonl")
	t.Setenv("FACTILE_TRACE_FILE", traceFile)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "list", "/", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list exit code = %d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"surface":"cli"`) || !strings.Contains(string(data), `"command":"list"`) {
		t.Fatalf("unexpected trace data: %s", string(data))
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"--mount-file", mountFile, "list", "/", "--brief", "--format", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("brief list exit code = %d stderr=%s", code, stderr.String())
	}
	data, err = os.ReadFile(traceFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"command":"list --brief"`) {
		t.Fatalf("brief list was not traced distinctly: %s", string(data))
	}
}

func cliMountFile(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	product := filepath.Join(tmp, "product-docs")
	broken := filepath.Join(tmp, "broken-docs")
	copyTestDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	copyTestDir(t, filepath.Join("..", "..", "testdata", "bundles", "broken-docs"), broken)
	mountFile := filepath.Join(tmp, "mount-registry.toml")
	data := `[mounts."/product-docs"]
source = "` + product + `"
kind = "local"
writable = true

[mounts."/broken-docs"]
source = "` + broken + `"
kind = "local"
writable = true
`
	if err := os.WriteFile(mountFile, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return mountFile
}

func cliV2Workspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	writeCLIRootConfig(t, workspace)
	copyTestDir(t, filepath.Join("..", "..", "testdata", "bundles"), filepath.Join(tmp, "bundles"))
	writeCLITestFile(t, filepath.Join(workspace, "engineering", "common.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Common Engineering Guides"
description = "Shared setup and operating guides."
trust = "shared"
`)
	writeCLITestFile(t, filepath.Join(workspace, "engineering", "django.mount.toml"), `source = "../../bundles/product-docs"
writable = true
title = "Django Product Docs"
description = "Product workflow and runbook examples."
when_to_use = "Use when working on invoice import workflows or runbooks."
trust = "local"
`)
	writeCLITestFile(t, filepath.Join(workspace, "engineering", "playbook.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Engineering Playbook"
description = "The same shared guides mounted at another path."
trust = "shared"
`)
	copyTestDir(t, filepath.Join("..", "..", "testdata", "bundles", "legacy-notes"), filepath.Join(workspace, "legacy"))
	return workspace
}

func writeCLITestFile(t *testing.T, filename string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCLIRootConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".factile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".factile", "config.toml"), []byte(`version = 1

name = "test"
title = "Test"

[defaults]
format = "okf"
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTestDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyTestDir(t, from, to)
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func fmtJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func fmtJSONBytes(t *testing.T, data []byte) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("output did not return JSON: %v\n%s", err, string(data))
	}
	return fmtJSON(value)
}

func runCLIJSON[T any](t *testing.T, args ...string) T {
	t.Helper()
	return runCLIJSONWithCode[T](t, 0, args...)
}

func runCLIJSONWithCode[T any](t *testing.T, wantCode int, args ...string) T {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, nil, &stdout, &stderr)
	if code != wantCode {
		t.Fatalf("%v exit code = %d, want %d stdout=%s stderr=%s", args, code, wantCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("%v wrote stderr: %s", args, stderr.String())
	}
	assertNoTerminalEscapes(t, stdout.String())
	return decodeJSON[T](t, stdout.Bytes())
}

func assertCLIJSONError(t *testing.T, wantExit int, wantCode string, wantMessage string, args ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, nil, &stdout, &stderr)
	if code != wantExit {
		t.Fatalf("%v exit code = %d, want %d stdout=%s stderr=%s", args, code, wantExit, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%v wrote stdout on error: %s", args, stdout.String())
	}
	assertNoTerminalEscapes(t, stderr.String())
	var payload struct {
		Error factile.AppError `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("%v error output did not parse as JSON: %v\n%s", args, err, stderr.String())
	}
	if payload.Error.Code != wantCode || !strings.Contains(payload.Error.Message, wantMessage) {
		t.Fatalf("%v error = %#v, want code %q containing %q", args, payload.Error, wantCode, wantMessage)
	}
}

func decodeJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("output did not match JSON contract: %v\n%s", err, string(data))
	}
	return value
}

func hasFolderPath(folders []factile.FolderSummary, path string) bool {
	for _, folder := range folders {
		if folder.Path == path {
			return true
		}
	}
	return false
}

func hasCardPath(cards []factile.CardSummary, path string) bool {
	for _, card := range cards {
		if card.Path == path {
			return true
		}
	}
	return false
}

func hasConceptPath(concepts []factile.Concept, path string) bool {
	for _, concept := range concepts {
		if concept.Path == path {
			return true
		}
	}
	return false
}

func hasConceptSummaryPath(concepts []factile.ConceptSummary, pathOrID string) bool {
	for _, concept := range concepts {
		if concept.Path == pathOrID || concept.ConceptID == pathOrID {
			return true
		}
	}
	return false
}

func hasGraphNodePath(nodes []factile.GraphNode, path string) bool {
	for _, node := range nodes {
		if node.Concept.Path == path {
			return true
		}
	}
	return false
}

func hasGraphEdge(edges []factile.GraphEdge, from string, to string, kind string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func hasSearchResultPath(results []factile.SearchResult, path string) bool {
	for _, result := range results {
		if result.Concept.Path == path {
			return true
		}
	}
	return false
}

func hasValidationIssue(issues []factile.ValidationIssue, severity string, code string, path string) bool {
	for _, issue := range issues {
		if issue.Severity == severity && issue.Code == code && issue.Path == path {
			return true
		}
	}
	return false
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasMount(mounts []factile.Mount, mountPath string, source string) bool {
	return findMount(mounts, mountPath, source) != nil
}

func findMount(mounts []factile.Mount, mountPath string, source string) *factile.Mount {
	for _, mount := range mounts {
		if mount.MountPath == mountPath && mount.Source == source {
			return &mount
		}
	}
	return nil
}

func hasInitFile(files []struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}, path string, action string) bool {
	for _, file := range files {
		if file.Path == filepath.ToSlash(path) && file.Action == action {
			return true
		}
	}
	return false
}

func hasFileChange(changes []skill.FileChange, path string) bool {
	for _, change := range changes {
		if change.Path == path {
			return true
		}
	}
	return false
}

func hasDoctorCheck(checks []skill.DoctorCheck, name string, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}

func appendOutputArgs(args []string, outputArgs ...string) []string {
	combined := append([]string{}, args...)
	return append(combined, outputArgs...)
}

func assertNotJSONText(t *testing.T, output string) {
	t.Helper()
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.Contains(output, `"path":`) {
		t.Fatalf("text output looked like JSON:\n%s", output)
	}
}

func assertNoTerminalEscapes(t *testing.T, output string) {
	t.Helper()
	if strings.Contains(output, "\x1b") {
		t.Fatalf("output contained terminal escapes:\n%q", output)
	}
}

func cliRevision(t *testing.T, mountFile string, path string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--mount-file", mountFile, "read", path, "--json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("read revision exit code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result struct {
		Concept struct {
			Revision string `json:"revision"`
		} `json:"concept"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("read revision JSON parse failed: %v\n%s", err, stdout.String())
	}
	if result.Concept.Revision == "" {
		t.Fatalf("read revision missing:\n%s", stdout.String())
	}
	return result.Concept.Revision
}
