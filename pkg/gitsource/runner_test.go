package gitsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunnerUsesArgumentArrayAndNonInteractiveEnvironment(t *testing.T) {
	recordPath := t.TempDir() + string(os.PathSeparator) + "record.json"
	t.Setenv("FACTILE_GIT_HELPER", "record")
	t.Setenv("FACTILE_GIT_HELPER_RECORD", recordPath)
	t.Setenv("GIT_SSH_COMMAND", "unsafe-interactive-command")
	for key := range repositoryEnvironment {
		t.Setenv(key, "untrusted")
	}
	preserved := map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "url.file:///fixture/.insteadOf",
		"GIT_CONFIG_VALUE_0": "https://example.test/",
		"HTTPS_PROXY":        "http://proxy.example.test",
		"SSH_AUTH_SOCK":      "/tmp/factile-test-agent.sock",
	}
	for key, value := range preserved {
		t.Setenv(key, value)
	}
	runner := helperRunner(5 * time.Second)
	remote := "https://example.test/repo.git; touch should-not-run"
	ref := "-option-looking-ref"
	if _, err := runner.Run(context.Background(), "", "fetch", "--", remote, ref); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Args []string          `json:"args"`
		Env  map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if !containsExact(record.Args, remote) || !containsExact(record.Args, ref) {
		t.Fatalf("Git values were not preserved as distinct arguments: %#v", record.Args)
	}
	for key, want := range map[string]string{
		"GCM_INTERACTIVE":     "Never",
		"GIT_LFS_SKIP_SMUDGE": "1",
		"GIT_SSH_COMMAND":     "ssh -oBatchMode=yes",
		"GIT_SSH_VARIANT":     "ssh",
		"GIT_TERMINAL_PROMPT": "0",
	} {
		if record.Env[key] != want {
			t.Fatalf("%s = %q, want %q", key, record.Env[key], want)
		}
	}
	for key := range repositoryEnvironment {
		if record.Env[key] != "" {
			t.Fatalf("repository environment %s was inherited as %q", key, record.Env[key])
		}
	}
	for key, want := range preserved {
		if record.Env[key] != want {
			t.Fatalf("transport environment %s = %q, want %q", key, record.Env[key], want)
		}
	}
	if !containsSequence(record.Args, "-c", "submodule.recurse=false") {
		t.Fatalf("runner did not disable recursive submodules: %#v", record.Args)
	}
}

func TestRunnerUsesOnlyItsManagedIndexOverride(t *testing.T) {
	recordPath := t.TempDir() + string(os.PathSeparator) + "record.json"
	t.Setenv("FACTILE_GIT_HELPER", "record")
	t.Setenv("FACTILE_GIT_HELPER_RECORD", recordPath)
	t.Setenv("GIT_INDEX_FILE", "/tmp/untrusted-index")
	runner := helperRunner(5 * time.Second)
	managedIndex := t.TempDir() + string(os.PathSeparator) + "index"
	if _, err := runner.runWithManagedIndex(context.Background(), "", managedIndex, "read-tree", "HEAD"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Env["GIT_INDEX_FILE"] != managedIndex {
		t.Fatalf("managed index = %q, want %q", record.Env["GIT_INDEX_FILE"], managedIndex)
	}
}

func TestRunnerReportsMissingGit(t *testing.T) {
	runner := NewRunner()
	runner.GitPath = "factile-git-executable-that-does-not-exist"
	_, err := runner.Run(context.Background(), "", "version")
	if !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("missing Git error = %v", err)
	}
}

func TestRunnerBoundsExecutionAndHonorsCancellation(t *testing.T) {
	t.Setenv("FACTILE_GIT_HELPER", "sleep")
	runner := helperRunner(20 * time.Millisecond)
	started := time.Now()
	_, err := runner.Run(context.Background(), "", "version")
	if !errors.Is(err, ErrGitTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("bounded command took too long: %s", time.Since(started))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, "", "version")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestRunnerSanitizesAndTruncatesGitErrors(t *testing.T) {
	t.Setenv("FACTILE_GIT_HELPER", "secret-error")
	runner := helperRunner(5 * time.Second)
	_, err := runner.Run(context.Background(), "", "fetch")
	if !errors.Is(err, ErrGitCommand) {
		t.Fatalf("command error = %v", err)
	}
	message := err.Error()
	for _, secret := range []string{"alice", "correct-horse", "hunter2", "bearer-secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("Git error exposed %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "[redacted]") || len(message) > maxGitErrorLength+100 {
		t.Fatalf("Git error was not sanitized and bounded: len=%d %s", len(message), message)
	}
}

func TestBoundedBufferDiscardsExcessOutput(t *testing.T) {
	buffer := boundedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("bounded write = %d, %v", written, err)
	}
	if buffer.String() != "abcd" || !buffer.truncated {
		t.Fatalf("unexpected bounded buffer: %q truncated=%t", buffer.String(), buffer.truncated)
	}
	if written, err := buffer.Write([]byte("gh")); err != nil || written != 2 || buffer.String() != "abcd" {
		t.Fatalf("discarding write = %d, %v, %q", written, err, buffer.String())
	}
}

func helperRunner(timeout time.Duration) Runner {
	return Runner{
		GitPath: "git",
		Timeout: timeout,
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			helperArgs := []string{"-test.run=TestGitSourceCommandHelper", "--"}
			helperArgs = append(helperArgs, args...)
			return exec.CommandContext(ctx, os.Args[0], helperArgs...)
		},
	}
}

func TestGitSourceCommandHelper(t *testing.T) {
	mode := os.Getenv("FACTILE_GIT_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "record":
		environment := map[string]string{
			"GCM_INTERACTIVE":     os.Getenv("GCM_INTERACTIVE"),
			"GIT_LFS_SKIP_SMUDGE": os.Getenv("GIT_LFS_SKIP_SMUDGE"),
			"GIT_SSH_COMMAND":     os.Getenv("GIT_SSH_COMMAND"),
			"GIT_SSH_VARIANT":     os.Getenv("GIT_SSH_VARIANT"),
			"GIT_TERMINAL_PROMPT": os.Getenv("GIT_TERMINAL_PROMPT"),
		}
		for key := range repositoryEnvironment {
			environment[key] = os.Getenv(key)
		}
		for _, key := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "HTTPS_PROXY", "SSH_AUTH_SOCK"} {
			environment[key] = os.Getenv(key)
		}
		record := struct {
			Args []string          `json:"args"`
			Env  map[string]string `json:"env"`
		}{
			Args: append([]string(nil), os.Args...),
			Env:  environment,
		}
		data, err := json.Marshal(record)
		if err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("FACTILE_GIT_HELPER_RECORD"), data, 0o600); err != nil {
			os.Exit(2)
		}
	case "sleep":
		time.Sleep(5 * time.Second)
	case "secret-error":
		_, _ = fmt.Fprint(os.Stderr, "fatal: https://alice:correct-horse@example.test/repo.git?token=hunter2 Authorization: Bearer bearer-secret "+strings.Repeat("x", maxGitErrorLength*2))
		os.Exit(2)
	default:
		os.Exit(2)
	}
}

func containsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSequence(values []string, first, second string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == first && values[index+1] == second {
			return true
		}
	}
	return false
}
