package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestCLIExplainsGitAuthenticationOnFirstAndCachedRead(t *testing.T) {
	// Use real Git against a local authentication challenge, without user helpers.
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
	writeCLICombinedWorkspace(t, root)
	t.Chdir(root)
	writeCLITestFile(t, filepath.Join(root, "coding.mount.toml"), fmt.Sprintf("source = %q\nwritable = false\n", server.URL+"/private.git"))
	for _, jsonOutput := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"read", "/coding/overview", "--color", "never"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if code := Run(context.Background(), args, nil, &stdout, &stderr); code != 6 || stdout.Len() != 0 {
			t.Fatalf("read returned %d: stdout=%s stderr=%s", code, &stdout, &stderr)
		}
		message := stderr.String()
		if jsonOutput {
			var envelope struct {
				Error factile.AppError `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != factile.ErrRemoteSourceUnavailable || envelope.Error.Details["reason"] != "authentication_failed" {
				t.Fatalf("wrong cached error: %#v", envelope.Error)
			}
			message = envelope.Error.Message
		}
		for _, expected := range []string{"Git authentication failed", "cannot prompt", "credential helper", "SSH", "factile refresh"} {
			if !strings.Contains(message, expected) {
				t.Fatalf("missing %q in %s", expected, message)
			}
		}
		if strings.Contains(message, server.URL) || strings.Contains(message, "fatal:") {
			t.Fatalf("raw Git output escaped: %s", message)
		}
	}
	mounts := runCLIJSON[factile.MountListResult](t, "mounts", "--json")
	if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus.LastErrorReason != "authentication_failed" {
		t.Fatalf("mount status lost authentication reason: %#v", mounts)
	}
}
