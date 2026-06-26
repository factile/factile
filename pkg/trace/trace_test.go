package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendWritesJSONLinesAndCreatesParent(t *testing.T) {
	traceFile := filepath.Join(t.TempDir(), "nested", "usage.jsonl")
	t.Setenv("FACTILE_TRACE_FILE", traceFile)

	Append(Event{Timestamp: "2026-06-26T00:00:00Z", Surface: "cli", Command: "read", Path: "/docs/a", ExitCode: 0, DurationMS: 12, ResultCount: 1})
	Append(Event{Surface: "mcp", Command: "factile_read", Path: "/docs/b", Query: "needle", ExitCode: 1})

	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two trace lines, got %d:\n%s", len(lines), string(data))
	}
	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Timestamp != "2026-06-26T00:00:00Z" || first.Surface != "cli" || first.Command != "read" || first.ResultCount != 1 {
		t.Fatalf("unexpected first trace event: %#v", first)
	}
	var second Event
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if second.Timestamp == "" || second.Surface != "mcp" || second.Query != "needle" || second.ExitCode != 1 {
		t.Fatalf("unexpected second trace event: %#v", second)
	}
}

func TestAppendWithoutTraceFileIsNoop(t *testing.T) {
	t.Setenv("FACTILE_TRACE_FILE", "")
	Append(Event{Surface: "cli", Command: "list"})
}
