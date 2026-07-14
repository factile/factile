package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/gitsource"
	"github.com/factile/factile/pkg/version"
	"github.com/factile/factile/pkg/vfs"
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
		if strings.Contains(tool.Name, "write") || strings.Contains(tool.Name, "create") || strings.Contains(tool.Name, "delete") || strings.Contains(tool.Name, "link") || strings.Contains(tool.Name, "mkdir") {
			t.Fatalf("read-only tools include write tool: %s", tool.Name)
		}
	}
	if descriptions["factile_context"] != "Retrieve focused OKF context for a task or question." {
		t.Fatalf("unexpected context description: %q", descriptions["factile_context"])
	}
	if descriptions["factile_mounts"] != "List configured mounts and cached Git source status without refreshing." || descriptions["factile_refresh"] != "Immediately check and refresh generated state for one Git mount." {
		t.Fatalf("unexpected Git reader tool descriptions: %#v", descriptions)
	}
	if descriptions["factile_stat"] == "" || descriptions["factile_mounts"] == "" || descriptions["factile_refresh"] == "" || descriptions["factile_view_list"] == "" || descriptions["factile_view_inspect"] == "" {
		t.Fatalf("read-only tools missing reader, mount, or view inspection tools: %#v", descriptions)
	}
	if descriptions["factile_kb_list"] != "" || descriptions["factile_kb_inspect"] != "" || descriptions["factile_mount"] != "" || descriptions["factile_unmount"] != "" || descriptions["factile_view_set"] != "" || descriptions["factile_view_delete"] != "" || descriptions["factile_mkdir"] != "" {
		t.Fatalf("read-only tools should hide catalog and write tools: %#v", descriptions)
	}
	for _, name := range []string{"factile_list", "factile_search", "factile_context", "factile_graph", "factile_validate"} {
		properties, ok := schemas[name]["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema missing properties: %#v", name, schemas[name])
		}
		if properties["view"] == nil || schemas[name]["additionalProperties"] != false {
			t.Fatalf("%s schema missing view or closed-shape contract: %#v", name, schemas[name])
		}
	}
	readWrite := New(ws, Options{})
	if len(readWrite.Tools()) <= len(readOnly.Tools()) {
		t.Fatal("expected write tools in read-write mode")
	}
	readWriteSchemas := map[string]map[string]any{}
	readWriteDescriptions := map[string]string{}
	for _, tool := range readWrite.Tools() {
		readWriteSchemas[tool.Name] = tool.InputSchema
		readWriteDescriptions[tool.Name] = tool.Description
	}
	if readWriteDescriptions["factile_mount"] != "Create or replace a read-only-by-default local or Git path mount." {
		t.Fatalf("unexpected mount tool description: %q", readWriteDescriptions["factile_mount"])
	}
	properties, ok := readWriteSchemas["factile_mount"]["properties"].(map[string]any)
	if !ok || properties["mount_path"] == nil || properties["source"] == nil || properties["writable"] == nil || properties["read_only"] == nil || properties["ref"] == nil || properties["revision"] == nil || readWriteSchemas["factile_mount"]["additionalProperties"] != false {
		t.Fatalf("write tools missing mount schema: %#v", readWriteSchemas["factile_mount"])
	}
	properties, ok = readWriteSchemas["factile_unmount"]["properties"].(map[string]any)
	if !ok || properties["mount_path"] == nil || readWriteSchemas["factile_unmount"]["additionalProperties"] != false {
		t.Fatalf("write tools missing unmount schema: %#v", readWriteSchemas["factile_unmount"])
	}
	properties, ok = readWriteSchemas["factile_view_set"]["properties"].(map[string]any)
	if !ok || properties["paths"] == nil || readWriteSchemas["factile_view_set"]["additionalProperties"] != false {
		t.Fatalf("write tools missing view set schema: %#v", readWriteSchemas["factile_view_set"])
	}
	properties, ok = readWriteSchemas["factile_mkdir"]["properties"].(map[string]any)
	if !ok || properties["path"] == nil || properties["title"] == nil || properties["log"] == nil || properties["overview"] == nil || properties["bundle"] == nil || readWriteSchemas["factile_mkdir"]["additionalProperties"] != false {
		t.Fatalf("write tools missing mkdir schema: %#v", readWriteSchemas["factile_mkdir"])
	}
	if readWriteSchemas["factile_kb_create"] != nil || readWriteSchemas["factile_kb_link"] != nil || readWriteSchemas["factile_kb_unlink"] != nil {
		t.Fatalf("MCP should not expose KB catalog write tools: %#v", readWriteSchemas)
	}
}

func TestReadOnlyRejectsHiddenWriteToolCalls(t *testing.T) {
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: t.TempDir()})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"./docs","mount_path":"/docs"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_unmount","arguments":{"mount_path":"/docs"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_create","arguments":{"path":"/engineering/note","type":"Note","title":"Note","markdown":"Body"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_view_set","arguments":{"id":"reader","paths":["/engineering"]}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"factile_view_delete","arguments":{"id":"reader"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_mkdir","arguments":{"path":"/engineering/guides","bundle":true}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `"code":"source_read_only"`) != 6 {
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

func TestServeV2MountToolsAndReaderCards(t *testing.T) {
	tmp := t.TempDir()
	writeMCPRootConfig(t, tmp)
	writeMCPConceptFile(t, filepath.Join(tmp, "overview.md"), "Guide", "Overview", "# Overview\n\nRoot-local docs are readable.\n")
	product := filepath.Join(tmp, "product-docs")
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + product + `","mount_path":"/engineering/docs","title":"Product Docs","read_only":true}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_mounts","arguments":{}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/overview"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/engineering","brief":true}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"factile_stat","arguments":{"path":"/engineering/docs"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_unmount","arguments":{"mount_path":"/engineering/docs"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())
	mounted := mcpStructured[factile.MountResult](t, responses[1])
	if mounted.Mount.MountPath != "/engineering/docs" || mounted.Mount.Title != "Product Docs" || mounted.Mount.Writable {
		t.Fatalf("unexpected MCP mount result: %#v", mounted)
	}
	mounts := mcpStructured[factile.MountListResult](t, responses[2])
	if !mcpHasMount(mounts.Mounts, "/engineering/docs", product) {
		t.Fatalf("unexpected MCP mounts result: %#v", mounts)
	}
	read := mcpStructured[factile.ConceptResult](t, responses[3])
	if read.Concept.Path != "/overview" || read.Concept.Frontmatter["title"] != "Overview" {
		t.Fatalf("unexpected MCP root-local read: %#v", read.Concept)
	}
	list := mcpStructured[factile.ListResult](t, responses[4])
	if !mcpHasCardPath(list.Cards, "/engineering/docs") {
		t.Fatalf("unexpected MCP mounted cards: %#v", list.Cards)
	}
	stat := mcpStructured[factile.StatResult](t, responses[5])
	if stat.Card.Path != "/engineering/docs" || stat.Card.Title != "Product Docs" || stat.Card.Writable == nil || *stat.Card.Writable {
		t.Fatalf("unexpected MCP mounted stat: %#v", stat.Card)
	}
	unmounted := mcpStructured[factile.UnmountResult](t, responses[6])
	if !unmounted.Removed || unmounted.MountPath != "/engineering/docs" {
		t.Fatalf("unexpected MCP unmount result: %#v", unmounted)
	}
	for _, stale := range []string{filepath.Join(tmp, ".factile", "library.toml"), filepath.Join(tmp, ".factile", "mounts.toml"), filepath.Join(tmp, ".factile", "knowledge-bases")} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("v2 MCP mount should not create stale catalog file %s, err=%v", stale, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "engineering", "docs.mount.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected MCP unmount to remove descriptor, err=%v", err)
	}
}

func TestMCPMountCapabilityCompatibility(t *testing.T) {
	tmp := t.TempDir()
	writeMCPRootConfig(t, tmp)
	source := filepath.Join(tmp, "source")
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), source)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + source + `","mount_path":"/default"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + source + `","mount_path":"/writable","writable":true}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + source + `","mount_path":"/legacy-writable","read_only":false}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + source + `","mount_path":"/conflict","writable":true,"read_only":false}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())
	defaultMount := mcpStructured[factile.MountResult](t, responses[1])
	if defaultMount.Mount.Writable {
		t.Fatalf("omitted MCP capability should be read-only: %#v", defaultMount.Mount)
	}
	writableMount := mcpStructured[factile.MountResult](t, responses[2])
	if !writableMount.Mount.Writable {
		t.Fatalf("writable MCP input should enable local writes: %#v", writableMount.Mount)
	}
	legacyMount := mcpStructured[factile.MountResult](t, responses[3])
	if !legacyMount.Mount.Writable {
		t.Fatalf("legacy read_only=false should retain writable behavior: %#v", legacyMount.Mount)
	}
	conflict := responses[4]
	if conflict.Error == nil {
		t.Fatalf("conflicting MCP capability inputs should fail validation: %#v", conflict)
	}
	errorData, ok := conflict.Error.Data.(map[string]any)
	if !ok || errorData["code"] != factile.ErrValidationFailed {
		t.Fatalf("conflicting MCP capability inputs should fail validation: %#v", conflict)
	}
	if _, err := os.Stat(filepath.Join(tmp, "conflict.mount.toml")); !os.IsNotExist(err) {
		t.Fatalf("conflicting MCP capability inputs wrote a descriptor: %v", err)
	}
}

func TestMCPGitMountRefreshAndReadOnlyVisibility(t *testing.T) {
	tmp := t.TempDir()
	writeMCPRootConfig(t, tmp)
	remote := mcpGitRemote(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: tmp})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/git","ref":"main"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_mounts","arguments":{}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_refresh","arguments":{"mount_path":"/git"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/writable","writable":true}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/conflict","ref":"main","revision":"1111111111111111111111111111111111111111"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/empty-ref","ref":""}}}
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/empty-revision","revision":""}}}
{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"factile_mkdir","arguments":{"path":"/git/new"}}}
{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"factile_create","arguments":{"path":"/git/new","type":"Guide","title":"New","markdown":"# New"}}}
{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"factile_write","arguments":{"path":"/git/overview","expected_revision":"sha256:unchanged","markdown":"# Changed"}}}
{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"factile_patch","arguments":{"path":"/git/overview","expected_revision":"sha256:unchanged","set":{"status":"draft"}}}}
{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"factile_rename","arguments":{"old_path":"/git/overview","new_path":"/git/renamed","expected_revision":"sha256:unchanged"}}}
{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"factile_delete","arguments":{"path":"/git/overview","expected_revision":"sha256:unchanged"}}}
{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"factile_deprecate","arguments":{"path":"/git/overview","expected_revision":"sha256:unchanged","reason":"superseded"}}}
{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/sha256-lower","revision":"` + strings.Repeat("a", 64) + `"}}}
{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"` + remote + `","mount_path":"/sha256-upper","revision":"` + strings.Repeat("A", 64) + `"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())
	mounted := mcpStructured[factile.MountResult](t, responses[1])
	if mounted.Mount.Kind != "git" || mounted.Mount.Ref != "main" || mounted.Mount.Writable {
		t.Fatalf("unexpected MCP Git mount: %#v", mounted)
	}
	mounts := mcpStructured[factile.MountListResult](t, responses[2])
	if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus == nil || !mounts.Mounts[0].SourceStatus.SnapshotAvailable {
		t.Fatalf("MCP mounts missing Git status: %#v", mounts)
	}
	refreshed := mcpStructured[factile.RefreshResult](t, responses[3])
	if refreshed.Outcome != "unchanged" || !refreshed.Status.SnapshotAvailable {
		t.Fatalf("unexpected MCP refresh: %#v", refreshed)
	}
	for _, tc := range []struct {
		id   int
		code string
	}{{id: 4, code: factile.ErrSourceReadOnly}, {id: 5, code: factile.ErrValidationFailed}, {id: 6, code: factile.ErrValidationFailed}, {id: 7, code: factile.ErrValidationFailed}, {id: 15, code: factile.ErrValidationFailed}, {id: 16, code: factile.ErrValidationFailed}} {
		response := responses[tc.id]
		if response.Error == nil {
			t.Fatalf("MCP Git rejection %d succeeded: %#v", tc.id, response)
		}
		data, ok := response.Error.Data.(map[string]any)
		if !ok || data["code"] != tc.code {
			t.Fatalf("MCP Git rejection %d code = %#v", tc.id, response)
		}
	}
	for _, mountPath := range []string{"/sha256-lower", "/sha256-upper"} {
		descriptor := filepath.Join(tmp, strings.TrimPrefix(mountPath, "/")+".mount.toml")
		if _, err := os.Stat(descriptor); !os.IsNotExist(err) {
			t.Fatalf("64-hex MCP revision wrote %s: %v", descriptor, err)
		}
	}
	for _, id := range []int{8, 9, 10, 11, 12, 13, 14} {
		response := responses[id]
		if response.Error == nil {
			t.Fatalf("Git mutation %d should fail below the MCP adapter: %#v", id, response)
		}
		data, ok := response.Error.Data.(map[string]any)
		if !ok || data["code"] != factile.ErrSourceReadOnly {
			t.Fatalf("Git mutation %d should fail below the MCP adapter: %#v", id, response)
		}
	}

	out.Reset()
	readOnlyInput := strings.NewReader(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_refresh","arguments":{"mount_path":"/git"}}}
`)
	if err := Serve(context.Background(), ws, readOnlyInput, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	readOnlyRefresh := mcpStructured[factile.RefreshResult](t, mcpResponses(t, out.String())[6])
	if readOnlyRefresh.MountPath != "/git" {
		t.Fatalf("read-only MCP refresh failed: %#v", readOnlyRefresh)
	}

	out.Reset()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledInput := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"factile_refresh","arguments":{"mount_path":"/git"}}}
`)
	if err := Serve(canceledCtx, ws, canceledInput, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	canceled := mcpResponses(t, out.String())[7]
	if canceled.Error == nil || !strings.Contains(canceled.Error.Message, "context canceled") {
		t.Fatalf("canceled MCP refresh did not preserve cancellation: %#v", canceled)
	}
}

func TestMCPGitMountUsesCachedSnapshotWhenGitIsUnavailable(t *testing.T) {
	root := t.TempDir()
	writeMCPRootConfig(t, root)
	remote := mcpGitRemote(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	if _, err := ws.Mount(context.Background(), remote, "/git", factile.MountOptions{}); err != nil {
		t.Fatal(err)
	}
	cache, err := gitsource.OpenCache(vfs.LoadOptions{Root: root}, gitsource.NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry("/git", remote)
	if err != nil {
		t.Fatal(err)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	state.LastAttemptAt = time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	if err := cache.WriteState(entry, state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uncached.mount.toml"), []byte("source = \""+remote+"\"\nwritable = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", t.TempDir())
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/git/overview"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_refresh","arguments":{"mount_path":"/git"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/uncached/overview"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())
	read := mcpStructured[factile.ConceptResult](t, responses[1])
	if read.Concept.Path != "/git/overview" {
		t.Fatalf("cached MCP read with missing Git = %#v", read)
	}
	refreshed := mcpStructured[factile.RefreshResult](t, responses[2])
	if refreshed.Outcome != "stale" || refreshed.Warning == nil {
		t.Fatalf("cached MCP refresh with missing Git = %#v", refreshed)
	}
	failed := responses[3]
	if failed.Error == nil {
		t.Fatalf("uncached MCP read with missing Git succeeded: %#v", failed)
	}
	data, ok := failed.Error.Data.(map[string]any)
	if !ok || data["code"] != factile.ErrRemoteSourceUnavailable {
		t.Fatalf("uncached MCP missing-Git error = %#v", failed)
	}
}

func TestMCPRejectsEmptyGitURIDelimitersBeforeMutation(t *testing.T) {
	root := t.TempDir()
	writeMCPRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"https://example.test/repository.git?","mount_path":"/native-query"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"https://example.test/repository.git#","mount_path":"/native-fragment"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"git+https://example.test/repository.git?","mount_path":"/git-plus-query"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":"git+https://example.test/repository.git#","mount_path":"/git-plus-fragment"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())
	for id := 1; id <= 4; id++ {
		response := responses[id]
		if response.Error == nil {
			t.Fatalf("empty URI delimiter request %d succeeded: %#v", id, response)
		}
		data, ok := response.Error.Data.(map[string]any)
		if !ok || data["code"] != factile.ErrValidationFailed {
			t.Fatalf("empty URI delimiter request %d error = %#v", id, response)
		}
	}
	for _, mountPath := range []string{"/native-query", "/native-fragment", "/git-plus-query", "/git-plus-fragment"} {
		descriptor := filepath.Join(root, strings.TrimPrefix(mountPath, "/")+".mount.toml")
		if _, err := os.Stat(descriptor); !os.IsNotExist(err) {
			t.Fatalf("empty URI delimiter MCP request wrote %s: %v", descriptor, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !os.IsNotExist(err) {
		t.Fatalf("empty URI delimiter MCP requests initialized Git cache: %v", err)
	}
}

func TestMCPGitStatusRedactsInvalidHandAuthoredSource(t *testing.T) {
	root := t.TempDir()
	writeMCPRootConfig(t, root)
	source := "https://alice:correct-horse@example.test/private.git?token=hunter2"
	if err := os.WriteFile(filepath.Join(root, "private.mount.toml"), []byte("source = \""+source+"\"\nwritable = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"factile_mounts\",\"arguments\":{}}}\n")
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "[redacted]") || !strings.Contains(text, `"last_error_code":"validation_failed"`) {
		t.Fatalf("invalid descriptor status was not safely rendered: %s", text)
	}
	for _, secret := range []string{"alice", "correct-horse", "hunter2", source} {
		if strings.Contains(text, secret) {
			t.Fatalf("MCP status exposed %q: %s", secret, text)
		}
	}
}

func TestServeMkdirTool(t *testing.T) {
	mountFile := mcpMountFile(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: mountFile})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mkdir","arguments":{"path":"/product-docs/guides","title":"Guides","bundle":true}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/product-docs/guides/overview"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, out.String())
	made := mcpStructured[factile.DirectoryResult](t, responses[1])
	if made.Directory.Path != "/product-docs/guides" || !made.Directory.Created || !mcpHasString(made.Directory.Files, "/product-docs/guides/index.md") || !mcpHasString(made.Directory.Files, "/product-docs/guides/log.md") || !mcpHasString(made.Directory.Files, "/product-docs/guides/overview.md") {
		t.Fatalf("unexpected MCP mkdir result: %#v", made.Directory)
	}
	overview := mcpStructured[factile.ConceptResult](t, responses[2])
	if overview.Concept.Path != "/product-docs/guides/overview" || overview.Concept.Frontmatter["title"] != "Guides Overview" {
		t.Fatalf("unexpected MCP mkdir overview: %#v", overview.Concept)
	}
}

func TestServeMountedSourceReaderToolsUseAllSources(t *testing.T) {
	workspaceDir := mcpV2Workspace(t)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/engineering","brief":true}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_search","arguments":{"path":"/engineering","query":"setup"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_context","arguments":{"path":"/engineering","query":"setup"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_graph","arguments":{"path":"/engineering"}}}
`)
	var out bytes.Buffer
	if err := Serve(context.Background(), ws, input, &out, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		`"path":"/engineering/django"`,
		`"path":"/engineering/common"`,
		`"path":"/engineering/playbook"`,
		`"path":"/engineering/common/guides/setup"`,
		`"path":"/engineering/playbook/guides/setup"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("mounted source reader MCP output missing %s:\n%s", expected, text)
		}
	}
}

func TestServeViewToolsAndReaderFilters(t *testing.T) {
	workspaceDir := t.TempDir()
	writeMCPRootConfig(t, workspaceDir)
	product := filepath.Join(workspaceDir, "product-docs")
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles", "product-docs"), product)
	if err := os.MkdirAll(filepath.Join(workspaceDir, "engineering"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "engineering", "django.mount.toml"), []byte(`source = "`+product+`"
writable = true
title = "Django"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMCPConceptFile(t, filepath.Join(workspaceDir, "legacy", "notes", "legacy.md"), "Guide", "Legacy", "# Legacy\n\nLegacy note.\n")
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: workspaceDir})
	workflowPath := "/engineering/django/workflows/invoice-import"
	runbookPath := "/engineering/django/runbooks/ocr-failure"
	legacyPath := "/legacy/notes/legacy"

	var writeOut bytes.Buffer
	setInput := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_view_set","arguments":{"id":"invoice","title":"Invoice","description":"Invoice workflow and support notes.","paths":["` + workflowPath + `","` + runbookPath + `","/legacy"]}}}
`)
	if err := Serve(context.Background(), ws, setInput, &writeOut, Options{}); err != nil {
		t.Fatal(err)
	}
	setResponses := mcpResponses(t, writeOut.String())
	set := mcpStructured[factile.ViewResult](t, setResponses[1])
	if set.Action != "created" || set.View.ID != "invoice" || len(set.View.Paths) != 3 {
		t.Fatalf("unexpected MCP view set: %#v", set)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, ".factile", "views.toml")); err != nil {
		t.Fatalf("expected MCP view set to write views.toml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, ".factile", "library.toml")); !os.IsNotExist(err) {
		t.Fatalf("MCP view set should not create library.toml, err=%v", err)
	}

	readOnlyInput := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_view_list","arguments":{}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_view_inspect","arguments":{"id":"invoice"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_list","arguments":{"path":"/","view":"invoice"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_search","arguments":{"path":"/","query":"setup","view":"invoice"}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"factile_context","arguments":{"path":"/engineering","query":"posted","view":"invoice"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"factile_graph","arguments":{"path":"/engineering","view":"invoice"}}}
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"factile_validate","arguments":{"path":"/engineering","view":"invoice"}}}
`)
	var readOnlyOut bytes.Buffer
	if err := Serve(context.Background(), ws, readOnlyInput, &readOnlyOut, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, readOnlyOut.String())
	views := mcpStructured[factile.ViewListResult](t, responses[1])
	if len(views.Views) != 1 || views.Views[0].ID != "invoice" {
		t.Fatalf("unexpected MCP view list: %#v", views)
	}
	inspected := mcpStructured[factile.ViewResult](t, responses[2])
	if inspected.View.ID != "invoice" || inspected.View.Title != "Invoice" {
		t.Fatalf("unexpected MCP view inspect: %#v", inspected)
	}
	list := mcpStructured[factile.ListResult](t, responses[3])
	if !mcpHasFolderPath(list.Folders, "/engineering") || !mcpHasFolderPath(list.Folders, "/legacy") {
		t.Fatalf("unexpected MCP list --view: %#v", list)
	}
	search := mcpStructured[factile.SearchResults](t, responses[4])
	for _, result := range search.Results {
		if result.Concept.Path != workflowPath && result.Concept.Path != runbookPath && result.Concept.Path != legacyPath {
			t.Fatalf("MCP search --view leaked out-of-view docs: %#v", search.Results)
		}
	}
	contextPack := mcpStructured[factile.ContextPack](t, responses[5])
	if !mcpHasConceptPath(contextPack.Concepts, workflowPath) || !mcpHasConceptPath(contextPack.Concepts, runbookPath) || mcpHasConceptPath(contextPack.Concepts, legacyPath) {
		t.Fatalf("unexpected MCP context --view: %#v", contextPack)
	}
	graph := mcpStructured[factile.GraphResult](t, responses[6])
	if !mcpHasGraphNodePath(graph.Nodes, workflowPath) || !mcpHasGraphNodePath(graph.Nodes, runbookPath) || !mcpHasGraphEdge(graph.Edges, workflowPath, runbookPath, "markdown_link") {
		t.Fatalf("unexpected MCP graph --view: %#v", graph)
	}
	validated := mcpStructured[factile.ValidationResult](t, responses[7])
	if !validated.Valid || len(validated.Issues) != 0 {
		t.Fatalf("unexpected MCP validate --view: %#v", validated)
	}

	writeOut.Reset()
	deleteInput := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_view_delete","arguments":{"id":"invoice"}}}
`)
	if err := Serve(context.Background(), ws, deleteInput, &writeOut, Options{}); err != nil {
		t.Fatal(err)
	}
	deleted := mcpStructured[factile.ViewDeleteResult](t, mcpResponses(t, writeOut.String())[1])
	if !deleted.Deleted || deleted.ID != "invoice" {
		t.Fatalf("unexpected MCP view delete: %#v", deleted)
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
	mountFile := filepath.Join(tmp, "mount-registry.toml")
	if err := os.WriteFile(mountFile, []byte(`[mounts."/product-docs"]
source = "`+product+`"
kind = "local"
writable = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return mountFile
}

func mcpV2Workspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	writeMCPRootConfig(t, workspace)
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles"), filepath.Join(tmp, "bundles"))
	writeMCPFile(t, filepath.Join(workspace, "engineering", "common.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Common Engineering Guides"
description = "Shared setup and operating guides."
trust = "shared"
`)
	writeMCPFile(t, filepath.Join(workspace, "engineering", "django.mount.toml"), `source = "../../bundles/product-docs"
writable = true
title = "Django Product Docs"
description = "Product workflow and runbook examples."
when_to_use = "Use when working on invoice import workflows or runbooks."
trust = "local"
`)
	writeMCPFile(t, filepath.Join(workspace, "engineering", "playbook.mount.toml"), `source = "../../bundles/shared-guides"
writable = false
title = "Engineering Playbook"
description = "The same shared guides mounted at another path."
trust = "shared"
`)
	copyMCPDir(t, filepath.Join("..", "..", "testdata", "bundles", "legacy-notes"), filepath.Join(workspace, "legacy"))
	return workspace
}

func writeMCPFile(t *testing.T, filename string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMCPRootConfig(t *testing.T, root string) {
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

func writeMCPConceptFile(t *testing.T, filename string, typ string, title string, markdown string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `---
type: ` + typ + `
title: ` + title + `
---

` + markdown
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mcpGitRemote(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	writeMCPRootConfig(t, source)
	writeMCPConceptFile(t, filepath.Join(source, "overview.md"), "Reference", "Git Fixture", "# Git Fixture\n")
	mcpGitRun(t, "", "init", "--", source)
	mcpGitRun(t, source, "config", "--local", "--", "user.name", "Factile Test")
	mcpGitRun(t, source, "config", "--local", "--", "user.email", "factile@example.test")
	mcpGitRun(t, source, "add", "--", ".")
	mcpGitRun(t, source, "commit", "-m", "fixture")
	mcpGitRun(t, source, "branch", "-M", "main")
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	mcpGitRun(t, "", "clone", "--bare", "--", source, remotePath)
	absolute, err := filepath.Abs(remotePath)
	if err != nil {
		t.Fatal(err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
}

func mcpGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitsource.NewRunner().Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
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

func mcpHasCardPath(cards []factile.CardSummary, path string) bool {
	for _, card := range cards {
		if card.Path == path {
			return true
		}
	}
	return false
}

func mcpHasMount(mounts []factile.Mount, mountPath string, source string) bool {
	for _, mount := range mounts {
		if mount.MountPath == mountPath && mount.Source == source {
			return true
		}
	}
	return false
}

func mcpHasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
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

func mcpHasSearchResultPath(results []factile.SearchResult, path string) bool {
	for _, result := range results {
		if result.Concept.Path == path {
			return true
		}
	}
	return false
}

func mcpHasGraphNodePath(nodes []factile.GraphNode, path string) bool {
	for _, node := range nodes {
		if node.Concept.Path == path {
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
