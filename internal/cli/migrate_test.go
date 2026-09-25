package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/okf"
)

func TestMigrationPreviewApplyAndIdempotence(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"index.md":            "---\nokf_version: \"0.1\"\ntitle: Original\ncustom: {keep: true}\n---\n# Original\n",
		"nested/index.md":     "---\ntitle: Nested\n---\n# Nested\n",
		"log.md":              "---\ntimestamp: 2026-01-01T00:00:00Z\n---\n# Log\n\n## 2026-01-01\n\n- Added.\n",
		"known.md":            "---\ntype: Guide\n# keep the comment\ngenerated: {by: process:known, custom: kept}\ntimestamp: 2026-01-01T08:00:00+02:00\nverified: {by: 'human:reviewer', at: '2026-01-02T00:00:00Z'}\n---\n# Known\n\nUnchanged claim.\n\n# Citations\n- [Evidence](https://example.com/evidence) - explanation\n- https://example.com/raw\n",
		"unknown.md":          "---\ntype: Note\ntimestamp: 2026-02-03\ncustom: {nested: [true, value]}\n---\n# Unknown\n\n```markdown\n# Citations\n- leave the example alone\n```\n",
		"retired.md":          "---\ntype: Note\ndeprecated: true\ndeprecated_reason: Replaced\n---\n# Retired\n",
		"flow.md":             "---\n{type: Note, timestamp: '2026-01-01T00:00:00Z', custom: {nested: [true, value]}}\n---\n# Flow\n",
		"asset.json":          "{\"unchanged\":true}",
		".factile/private.md": "private state",
	}
	for name, body := range files {
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(apply bool) map[string]any {
		t.Helper()
		var out, errors bytes.Buffer
		args := []string{"migrate", root, "--json"}
		if apply {
			args = append(args, "--apply")
		}
		if code := Run(context.Background(), args, nil, &out, &errors); code != 0 {
			t.Fatalf("%d: %s %s", code, out.String(), errors.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	preview := run(false)
	if len(preview["changes"].([]any)) != 7 {
		t.Fatalf("unexpected changes: %#v", preview)
	}
	for name, want := range files {
		actual, _ := os.ReadFile(filepath.Join(root, name))
		if string(actual) != want {
			t.Fatal("preview wrote", name)
		}
	}
	applied := run(true)
	for _, value := range applied["changes"].([]any) {
		if !value.(map[string]any)["applied"].(bool) {
			t.Fatal(value)
		}
	}
	knownBytes, _ := os.ReadFile(filepath.Join(root, "known.md"))
	known, err := okf.ParseConcept("known", knownBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := known.Frontmatter["timestamp"]; exists {
		t.Fatal("timestamp retained as active field")
	}
	generated := known.Frontmatter["generated"].(map[string]any)
	if generated["at"] != "2026-01-01T08:00:00+02:00" || generated["by"] != "process:known" || generated["custom"] != "kept" {
		t.Fatal(generated)
	}
	if known.Frontmatter["verified"].(map[string]any)["by"] != "human:reviewer" {
		t.Fatal("lost explicit review")
	}
	if !bytes.Contains(knownBytes, []byte("# keep the comment")) || !strings.Contains(known.Markdown, "Unchanged claim.\n\n# Sources\n- [Evidence]") {
		t.Fatal(string(knownBytes))
	}
	sources := known.Frontmatter["sources"].([]any)
	if len(sources) != 2 {
		t.Fatal(sources)
	}
	for _, source := range sources {
		if _, exists := source.(map[string]any)["id"]; exists {
			t.Fatal("invented claim attribution")
		}
	}
	unknownBytes, _ := os.ReadFile(filepath.Join(root, "unknown.md"))
	unknown, _ := okf.ParseConcept("unknown", unknownBytes)
	if unknown.Frontmatter["generated"] != nil || unknown.Frontmatter["verified"] != nil {
		t.Fatal("invented history")
	}
	if unknown.Frontmatter["legacy_metadata"].(map[string]any)["timestamp"] != "2026-02-03" {
		t.Fatal("lost unknown history")
	}
	if !strings.Contains(unknown.Markdown, "# Citations\n- leave the example alone") {
		t.Fatal("changed fenced example")
	}
	indexBytes, _ := os.ReadFile(filepath.Join(root, "index.md"))
	index, _ := okf.ParseConcept("index", indexBytes)
	if len(index.Frontmatter) != 1 || index.Frontmatter["okf_version"] != "0.2" || !strings.Contains(index.Markdown, "custom:") {
		t.Fatal(string(indexBytes))
	}
	if changes := run(true)["changes"].([]any); len(changes) != 0 {
		t.Fatal("second conversion changed files", changes)
	}
	for _, name := range []string{"asset.json", ".factile/private.md"} {
		actual, _ := os.ReadFile(filepath.Join(root, name))
		if string(actual) != files[name] {
			t.Fatal("changed unrelated file", name)
		}
	}
}

func TestMigrationAmbiguityAndValidationNeverPartiallyApply(t *testing.T) {
	for name, body := range map[string]string{
		"custom status":         "---\ntype: Note\nstatus: proposal\n---\n# Note\n",
		"conflicting lifecycle": "---\ntype: Note\nstatus: draft\ndeprecated: true\n---\n# Note\n",
		"unattributed citation": "---\ntype: Note\n---\n# Citations\nUnclear source\n",
		"invalid concept":       "---\ntitle: Missing type\n---\n# Note\n",
		"unknown version":       "---\nokf_version: next\n---\n# Index\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			filename := "note.md"
			if name == "unknown version" {
				filename = "index.md"
			}
			if err := os.WriteFile(filepath.Join(root, filename), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			untouched := "---\ntype: Note\ndeprecated: true\n---\n# Safe conversion\n"
			os.WriteFile(filepath.Join(root, "safe.md"), []byte(untouched), 0644)
			var out, errors bytes.Buffer
			code := Run(context.Background(), []string{"migrate", root, "--apply", "--json"}, nil, &out, &errors)
			if code != 3 {
				t.Fatalf("exit %d: %s %s", code, out.String(), errors.String())
			}
			for name, want := range map[string]string{filename: body, "safe.md": untouched} {
				got, _ := os.ReadFile(filepath.Join(root, name))
				if string(got) != want {
					t.Fatal("partial write", name)
				}
			}
		})
	}
}

func TestMigrationUsesNativeSchemaValidationAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "concept-schemas"), 0755)
	os.WriteFile(filepath.Join(root, "concept-schemas/note.schema.json"), []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","x-factile-concept-schema":{"format":"concept-schema/v1","id":"note","concept_type":"Note"},"required":["owner"]}`), 0644)
	os.WriteFile(filepath.Join(root, "note.md"), []byte("---\ntype: Note\n---\n# Note\n"), 0644)
	outside := filepath.Join(t.TempDir(), "outside.md")
	os.WriteFile(outside, []byte("not a concept"), 0644)
	os.Symlink(outside, filepath.Join(root, "linked.md"))
	var out, errors bytes.Buffer
	code := Run(context.Background(), []string{"migrate", root, "--apply", "--json"}, nil, &out, &errors)
	if code != 3 || !strings.Contains(out.String(), "concept_schema") {
		t.Fatalf("exit %d: %s %s", code, out.String(), errors.String())
	}
	got, _ := os.ReadFile(outside)
	if string(got) != "not a concept" {
		t.Fatal("followed symlink")
	}
}
