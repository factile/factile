package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestMCPSelectedGitBundleAndSelectionErrors(t *testing.T) {
	root := t.TempDir()
	writeMCPCombinedWorkspace(t, root)
	source := t.TempDir()
	writeMCPBundleManifest(t, filepath.Join(source, "docs"), "selected")
	if err := os.WriteFile(filepath.Join(source, "factile.toml"), []byte("version = 2\n[workspace]\nroot = \"docs\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeMCPConceptFile(t, filepath.Join(source, "docs", "practices", "boundary-contracts.md"), "Guide", "Boundary Contracts", "# Boundary Contracts\n")
	mcpGitRun(t, "", "init", "--", source)
	mcpGitRun(t, source, "config", "--local", "--", "user.name", "Factile Test")
	mcpGitRun(t, source, "config", "--local", "--", "user.email", "factile@example.test")
	mcpGitRun(t, source, "add", "--", ".")
	mcpGitRun(t, source, "commit", "-m", "nested bundle")
	mcpGitRun(t, source, "branch", "-M", "main")
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	mcpGitRun(t, "", "clone", "--bare", "--", source, remotePath)
	remote := (&url.URL{Scheme: "file", Path: filepath.ToSlash(remotePath)}).String()
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	input := strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":%q,"mount_path":"/coding"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/coding/practices/boundary-contracts"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_validate","arguments":{"path":"/coding"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/coding/docs/practices/boundary-contracts"}}}
`, remote))
	var output bytes.Buffer
	if err := Serve(context.Background(), ws, input, &output, Options{}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, output.String())
	mounted := mcpStructured[factile.MountResult](t, responses[1])
	if mounted.Mount.Writable {
		t.Fatal("Git mount is writable")
	}
	read := mcpStructured[factile.ConceptResult](t, responses[2])
	if read.Concept.Path != "/coding/practices/boundary-contracts" {
		t.Fatalf("read = %#v", read)
	}
	validated := mcpStructured[factile.ValidationResult](t, responses[3])
	if !validated.Valid {
		t.Fatalf("validation = %#v", validated)
	}
	if responses[4].Error == nil {
		t.Fatal("repository-relative path remained visible")
	}
	before, err := os.ReadFile(filepath.Join(root, "coding.mount.toml"))
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(source, "factile.toml"), []byte("version = 2\n[workspace]\nroot = \"missing\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mcpGitRun(t, source, "add", "--", ".")
	mcpGitRun(t, source, "commit", "-m", "invalid selection")
	mcpGitRun(t, source, "push", "--", remote, "main:main")
	input = strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_refresh","arguments":{"mount_path":"/coding"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/coding/practices/boundary-contracts"}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":%q,"mount_path":"/coding","title":"Replacement"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"factile_mount","arguments":{"source":%q,"mount_path":"/invalid"}}}
`, remote, remote))
	output.Reset()
	if err := Serve(context.Background(), ws, input, &output, Options{}); err != nil {
		t.Fatal(err)
	}
	responses = mcpResponses(t, output.String())
	refreshed := mcpStructured[factile.RefreshResult](t, responses[1])
	if refreshed.Outcome != "stale" || refreshed.Status.LastErrorReason != "missing_bundle" {
		t.Fatalf("refresh = %#v", refreshed)
	}
	mcpStructured[factile.ConceptResult](t, responses[2])
	for _, id := range []int{3, 4} {
		response := responses[id]
		if response.Error == nil {
			t.Fatalf("invalid candidate accepted: %#v", response)
		}
		data, ok := response.Error.Data.(map[string]any)
		if !ok || data["code"] != "validation_failed" || response.Error.Message != "Git snapshot selected bundle is missing." {
			t.Fatalf("error = %#v", response.Error)
		}
		details, ok := data["details"].(map[string]any)
		if !ok || details["reason"] != "missing_bundle" || len(details) != 1 {
			t.Fatalf("details = %#v", data)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, "coding.mount.toml"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed mount changed descriptor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "invalid.mount.toml")); !os.IsNotExist(err) {
		t.Fatalf("failed mount created descriptor: %v", err)
	}
}
