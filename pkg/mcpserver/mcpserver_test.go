package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/version"
)

func TestToolsRespectReadOnly(t *testing.T) {
	ws := factile.NewWorkspace(factile.WorkspaceOptions{})
	readOnly := New(ws, Options{ReadOnly: true})
	descriptions := map[string]string{}
	schemas := map[string]map[string]any{}
	for _, tool := range readOnly.Tools() {
		descriptions[tool.Name] = tool.Description
		schemas[tool.Name] = tool.InputSchema
		if tool.InputSchema == nil {
			t.Fatalf("tool missing input schema: %s", tool.Name)
		}
		if strings.Contains(tool.Name, "write") || strings.Contains(tool.Name, "create") || strings.Contains(tool.Name, "delete") || strings.Contains(tool.Name, "link") {
			t.Fatalf("read-only tools include write tool: %s", tool.Name)
		}
	}
	if descriptions["factile_context"] != "Retrieve focused local OKF context for a task or question." {
		t.Fatalf("unexpected context description: %q", descriptions["factile_context"])
	}
	if descriptions["factile_stat"] == "" || descriptions["factile_kb_list"] == "" || descriptions["factile_kb_inspect"] == "" {
		t.Fatalf("read-only tools missing reader or catalog inspection tools: %#v", descriptions)
	}
	if descriptions["factile_kb_view_set"] != "" || descriptions["factile_kb_view_delete"] != "" {
		t.Fatalf("read-only tools should hide View mutation tools: %#v", descriptions)
	}
	for _, name := range []string{"factile_list", "factile_search", "factile_context", "factile_graph"} {
		properties, ok := schemas[name]["properties"].(map[string]any)
		if !ok || properties["view"] == nil {
			t.Fatalf("%s schema missing optional view argument: %#v", name, schemas[name])
		}
	}
	readWrite := New(ws, Options{})
	if len(readWrite.Tools()) <= len(readOnly.Tools()) {
		t.Fatal("expected write tools in read-write mode")
	}
	readWriteSchemas := map[string]map[string]any{}
	for _, tool := range readWrite.Tools() {
		readWriteSchemas[tool.Name] = tool.InputSchema
	}
	properties, ok := readWriteSchemas["factile_kb_view_set"]["properties"].(map[string]any)
	if !ok || properties["bundles"] == nil {
		t.Fatalf("write tools missing View set schema: %#v", readWriteSchemas["factile_kb_view_set"])
	}
}

func TestReadOnlyRejectsHiddenWriteToolCalls(t *testing.T) {
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: t.TempDir()})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_kb_create","arguments":{"path":"/engineering","title":"Engineering"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_kb_view_set","arguments":{"knowledge_base_path":"/engineering","view_id":"reader","bundles":["docs"]}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `"code":"source_read_only"`) != 2 {
		t.Fatalf("expected source_read_only error, got:\n%s", out.String())
	}
}

func TestServeToolsListAndReadCall(t *testing.T) {
	mountFile := mcpMountFile(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/product-docs/workflows/invoice-import"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, Instructions) || !strings.Contains(text, "factile_read") || !strings.Contains(text, "/product-docs/workflows/invoice-import") {
		t.Fatalf("unexpected MCP output: %s", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var initialize struct {
		Result struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &initialize); err != nil {
		t.Fatalf("initialize response did not parse: %v\n%s", err, lines[0])
	}
	if initialize.Result.ServerInfo.Name != version.Name || initialize.Result.ServerInfo.Version != version.Current().Version {
		t.Fatalf("unexpected serverInfo: %#v", initialize.Result.ServerInfo)
	}
}

func TestServeReaderCardsAndKBCatalogTools(t *testing.T) {
	tmp := t.TempDir()
	product := filepath.Join(tmp, "product-docs")
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_kb_create","arguments":{"path":"/engineering","title":"Engineering"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_kb_link","arguments":{"knowledge_base_path":"/engineering","source":"` + product + `","bundle_path":"/engineering/docs","title":"Product Docs","read_only":true}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_kb_view_set","arguments":{"knowledge_base_path":"/engineering","view_id":"reader","bundles":["/engineering/docs"],"title":"Reader"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_kb_inspect","arguments":{"path":"/engineering"}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/engineering","brief":true,"view":"reader"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_stat","arguments":{"path":"/engineering/docs"}}}
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"factile_kb_view_delete","arguments":{"knowledge_base_path":"/engineering","view_id":"reader"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		`"path":"/engineering"`,
		`"path":"/engineering/docs"`,
		`"cards"`,
		`"card"`,
		`"writable":false`,
		`"view":{"id":"reader"`,
		`"bundles":["engineering-docs"]`,
		`"view_id":"reader"`,
		`"deleted":true`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("MCP output missing %s:\n%s", expected, text)
		}
	}
}

func TestServeViewScopedReaderTools(t *testing.T) {
	workspaceDir := filepath.Join("..", "..", "testdata", "catalog-workspace")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/engineering","brief":true,"view":"reader"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_search","arguments":{"path":"/engineering","query":"invoice","view":"reader"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_context","arguments":{"path":"/engineering","query":"invoice import workflow","view":"reader"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_graph","arguments":{"path":"/engineering","view":"reader"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		`"path":"/engineering/django"`,
		`"path":"/engineering/common"`,
		`"path":"/engineering/django/workflows/invoice-import"`,
		`"path":"/engineering/common/guides/setup"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("view-scoped MCP output missing %s:\n%s", expected, text)
		}
	}
	if strings.Contains(text, `"path":"/engineering/playbook`) {
		t.Fatalf("view-scoped MCP output leaked excluded playbook bundle:\n%s", text)
	}
}

func TestServeStructuredContentContracts(t *testing.T) {
	mountFile := mcpMountFile(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/product-docs/workflows/invoice-import"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/product-docs"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_search","arguments":{"path":"/product-docs","query":"invoice"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_context","arguments":{"path":"/product-docs","query":"invoice import workflow"}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"factile_graph","arguments":{"path":"/product-docs"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_validate","arguments":{"path":"/product-docs"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())

	read := mcpStructured[factile.ConceptResult](t, responses[1])
	if read.Concept.Path != "/product-docs/workflows/invoice-import" || read.Concept.Frontmatter["type"] != "Workflow" {
		t.Fatalf("unexpected MCP read contract: %#v", read)
	}
	list := mcpStructured[factile.ListResult](t, responses[2])
	if list.Path != "/product-docs" || !mcpHasFolderPath(list.Folders, "/product-docs/runbooks") || !mcpHasFolderPath(list.Folders, "/product-docs/workflows") {
		t.Fatalf("unexpected MCP list contract: %#v", list)
	}
	search := mcpStructured[factile.SearchResults](t, responses[3])
	if search.Query != "invoice" || len(search.Results) == 0 || search.Results[0].Concept.Path != "/product-docs/workflows/invoice-import" {
		t.Fatalf("unexpected MCP search contract: %#v", search)
	}
	contextPack := mcpStructured[factile.ContextPack](t, responses[4])
	if contextPack.Query != "invoice import workflow" || !mcpHasConceptPath(contextPack.Concepts, "/product-docs/workflows/invoice-import") {
		t.Fatalf("unexpected MCP context contract: %#v", contextPack)
	}
	graph := mcpStructured[factile.GraphResult](t, responses[5])
	if !mcpHasGraphEdge(graph.Edges, "/product-docs/workflows/invoice-import", "/product-docs/runbooks/ocr-failure", "markdown_link") {
		t.Fatalf("unexpected MCP graph contract: %#v", graph)
	}
	validation := mcpStructured[factile.ValidationResult](t, responses[6])
	if validation.Path != "/product-docs" || !validation.Valid || len(validation.Issues) != 0 {
		t.Fatalf("unexpected MCP validate contract: %#v", validation)
	}
}

func TestServeIgnoresInitializedNotification(t *testing.T) {
	mountFile := mcpMountFile(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"codex","version":"test"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected responses only for requests, got %d lines:\n%s", len(lines), out.String())
	}
	if strings.Contains(out.String(), "notifications/initialized") || strings.Contains(out.String(), "Unsupported MCP method") {
		t.Fatalf("notification produced an MCP error response:\n%s", out.String())
	}
	if !strings.Contains(lines[1], `"inputSchema"`) {
		t.Fatalf("tools/list response did not include tool input schemas:\n%s", lines[1])
	}
}

func TestServeTraceFile(t *testing.T) {
	mountFile := mcpMountFile(t)
	traceFile := filepath.Join(t.TempDir(), "usage.jsonl")
	t.Setenv("FACTILE_TRACE_FILE", traceFile)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/product-docs/workflows/invoice-import"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/product-docs","brief":true}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"surface":"mcp"`) || !strings.Contains(string(data), `"command":"factile_read"`) {
		t.Fatalf("unexpected trace data: %s", string(data))
	}
	if !strings.Contains(string(data), `"command":"factile_list --brief"`) {
		t.Fatalf("brief list trace missing: %s", string(data))
	}
}

func mcpMountFile(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	product := filepath.Join(tmp, "product-docs")
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	mountFile := filepath.Join(tmp, "mounts.toml")
	if err := os.WriteFile(mountFile, []byte(`[mounts."/product-docs"]
source = "`+product+`"
kind = "local"
writable = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return mountFile
}

func copyMCPDir(t *testing.T, src, dst string) {
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
			copyMCPDir(t, from, to)
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

type mcpTestResponse struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      int               `json:"id"`
	Result  mcpTestCallResult `json:"result"`
	Error   *rpcError         `json:"error,omitempty"`
}

type mcpTestCallResult struct {
	Content           []mcpTestContent `json:"content"`
	StructuredContent json.RawMessage  `json:"structuredContent"`
}

type mcpTestContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func mcpResponses(t *testing.T, output string) map[int]mcpTestResponse {
	t.Helper()
	responses := map[int]mcpTestResponse{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var response mcpTestResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("MCP response did not parse: %v\n%s", err, line)
		}
		if response.JSONRPC != "2.0" {
			t.Fatalf("unexpected MCP jsonrpc value: %#v", response)
		}
		responses[response.ID] = response
	}
	return responses
}

func mcpStructured[T any](t *testing.T, response mcpTestResponse) T {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("unexpected MCP error: %#v", response.Error)
	}
	if len(response.Result.Content) != 1 || response.Result.Content[0].Type != "text" {
		t.Fatalf("unexpected MCP content shape: %#v", response.Result.Content)
	}
	if len(response.Result.StructuredContent) == 0 {
		t.Fatalf("missing MCP structuredContent: %#v", response)
	}
	var textValue any
	if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &textValue); err != nil {
		t.Fatalf("MCP text content did not contain JSON: %v\n%s", err, response.Result.Content[0].Text)
	}
	var structuredValue any
	if err := json.Unmarshal(response.Result.StructuredContent, &structuredValue); err != nil {
		t.Fatalf("MCP structuredContent did not parse: %v\n%s", err, string(response.Result.StructuredContent))
	}
	textJSON, err := json.Marshal(textValue)
	if err != nil {
		t.Fatal(err)
	}
	structuredJSON, err := json.Marshal(structuredValue)
	if err != nil {
		t.Fatal(err)
	}
	if string(textJSON) != string(structuredJSON) {
		t.Fatalf("MCP text JSON and structuredContent differed:\ntext=%s\nstructured=%s", textJSON, structuredJSON)
	}
	var value T
	if err := json.Unmarshal(response.Result.StructuredContent, &value); err != nil {
		t.Fatalf("MCP structuredContent did not match contract: %v\n%s", err, string(response.Result.StructuredContent))
	}
	return value
}

func mcpHasFolderPath(folders []factile.FolderSummary, path string) bool {
	for _, folder := range folders {
		if folder.Path == path {
			return true
		}
	}
	return false
}

func mcpHasConceptPath(concepts []factile.Concept, path string) bool {
	for _, concept := range concepts {
		if concept.Path == path {
			return true
		}
	}
	return false
}

func mcpHasGraphEdge(edges []factile.GraphEdge, from string, to string, kind string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return true
		}
	}
	return false
}
