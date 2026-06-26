package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
	Timestamp   string `json:"timestamp"`
	Surface     string `json:"surface"`
	Command     string `json:"command"`
	Path        string `json:"path,omitempty"`
	Query       string `json:"query,omitempty"`
	ExitCode    int    `json:"exit_code"`
	DurationMS  int64  `json:"duration_ms"`
	ResultCount int    `json:"result_count"`
}

func Append(event Event) {
	filename := os.Getenv("FACTILE_TRACE_FILE")
	if filename == "" {
		return
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(event)
}
