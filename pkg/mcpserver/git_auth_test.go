package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestMCPExplainsGitAuthenticationOnFirstAndCachedRead(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "")
	t.Setenv("GIT_CONFIG_KEY_1", "core.askPass")
	t.Setenv("GIT_CONFIG_VALUE_1", "")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="factile-test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	root := t.TempDir()
	writeMCPCombinedWorkspace(t, root)
	descriptor := fmt.Sprintf("source = %q\nwritable = false\n", server.URL+"/private.git")
	if err := os.WriteFile(filepath.Join(root, "coding.mount.toml"), []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/coding/overview"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"factile_read","arguments":{"path":"/coding/overview"}}}
`)
	var output bytes.Buffer
	if err := Serve(context.Background(), ws, input, &output, Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	responses := mcpResponses(t, output.String())
	for _, id := range []int{1, 2} {
		failure := responses[id].Error
		if failure == nil {
			t.Fatalf("missing authentication error: %#v", responses[id])
		}
		data := failure.Data.(map[string]any)
		details, ok := data["details"].(map[string]any)
		if data["code"] != factile.ErrRemoteSourceUnavailable || !ok || details["reason"] != "authentication_failed" {
			t.Fatalf("wrong MCP failure: %#v", failure)
		}
		for _, expected := range []string{"Git authentication failed", "cannot prompt", "credential helper", "SSH", "factile refresh"} {
			if !strings.Contains(failure.Message, expected) {
				t.Fatalf("missing %q in %s", expected, failure.Message)
			}
		}
		if strings.Contains(failure.Message, server.URL) || strings.Contains(failure.Message, "fatal:") {
			t.Fatalf("raw Git output escaped: %s", failure.Message)
		}
	}
}
