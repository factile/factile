package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIReadPortableFrontmatterJSON(t *testing.T) {
	workspace := t.TempDir()
	writeCLICombinedWorkspace(t, workspace)
	fixture, err := os.ReadFile("../../testdata/bundles/portable-frontmatter/probe.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "probe.md"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--workspace", workspace, "read", "/probe", "--json"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("read exit %d: %s", code, stderr.String())
	}
	var payload struct {
		Concept struct {
			Frontmatter json.RawMessage `json:"frontmatter"`
		} `json:"concept"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("../../testdata/golden/portable-frontmatter.json")
	if err != nil {
		t.Fatal(err)
	}
	decode := func(raw []byte) any {
		t.Helper()
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		if err := d.Decode(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if !reflect.DeepEqual(decode(payload.Concept.Frontmatter), decode(expected)) {
		t.Fatalf("portable JSON mismatch: %s", payload.Concept.Frontmatter)
	}
}

func TestCLIFrontmatterBudgetReturnsJSON(t *testing.T) {
	workspace := t.TempDir()
	writeCLICombinedWorkspace(t, workspace)
	for _, fields := range []int{40000, 50000} {
		var raw strings.Builder
		raw.WriteString("---\ntype: Probe\n")
		for i := 0; i < fields; i++ {
			fmt.Fprintf(&raw, "k%d: v\n", i)
		}
		raw.WriteString("---\n")
		writeCLITestFile(t, filepath.Join(workspace, "wide.md"), raw.String())
		for _, command := range []string{"read", "validate"} {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"--workspace", workspace, command, "/wide", "--json"}, nil, &stdout, &stderr)
			if fields == 40000 {
				if code != 0 || !json.Valid(stdout.Bytes()) || stderr.Len() != 0 {
					t.Fatalf("%s exit %d: %s", command, code, stderr.String())
				}
			} else {
				output := append(stdout.Bytes(), stderr.Bytes()...)
				if code == 0 || !json.Valid(output) || !bytes.Contains(output, []byte("okf_parse_error")) {
					t.Fatalf("%s did not return a structured limit failure: exit %d, %s", command, code, output)
				}
			}
		}
	}
}

func TestCLIValidateRequiresStringType(t *testing.T) {
	for _, value := range []string{"", "null", "TRUE", "42", "[Probe]", "{name: Probe}"} {
		t.Run(value, func(t *testing.T) {
			workspace := t.TempDir()
			writeCLICombinedWorkspace(t, workspace)
			if err := os.WriteFile(filepath.Join(workspace, "probe.md"), []byte("---\ntype: "+value+"\n---\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), []string{"--workspace", workspace, "validate", "/probe", "--json"}, nil, &stdout, &stderr); code != 3 || !bytes.Contains(stdout.Bytes(), []byte(`"code": "missing_type"`)) {
				t.Fatalf("validate exit %d: %s %s", code, stdout.String(), stderr.String())
			}
		})
	}
}
