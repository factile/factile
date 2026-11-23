package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
)

func TestInitPreservesMigrationForExistingIndexlessBundle(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("docs", 0755); err != nil {
		t.Fatal(err)
	}
	const legacy = "---\ntype: Note\ntimestamp: 2026-02-03\n---\n# Old note\n"
	if err := os.WriteFile("docs/note.md", []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	runCLIJSON[map[string]any](t, "init", "--root", "docs", "--agent", "none", "--json")
	index, err := os.ReadFile("docs/index.md")
	if err != nil || bytes.Contains(index, []byte("okf_version")) {
		t.Fatalf("init index = %q, %v", index, err)
	}
	preview := runCLIJSON[struct {
		SourceVersion string `json:"source_version"`
		Changes       []any  `json:"changes"`
	}](t, "migrate", "docs", "--json")
	if preview.SourceVersion != "unversioned" || len(preview.Changes) != 2 {
		t.Fatalf("unexpected migration preview: %#v", preview)
	}
	runCLIJSON[map[string]any](t, "migrate", "docs", "--apply", "--json")
	note, err := os.ReadFile("docs/note.md")
	if err != nil {
		t.Fatal(err)
	}
	concept, err := okf.ParseConcept("note", note)
	if err != nil || concept.Frontmatter["timestamp"] != nil || !bytes.Contains(note, []byte("legacy_metadata:")) || !bytes.Contains(note, []byte("2026-02-03")) {
		t.Fatalf("migration note = %q, %v", note, err)
	}
}

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
	if index.Frontmatter["okf_version"] != "0.3" || index.Frontmatter["custom"] == nil || index.Frontmatter["title"] != "Original" {
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
		"conflicting lifecycle": "---\ntype: Note\nstatus: draft\ndeprecated: true\n---\n# Note\n",
		"unattributed citation": "---\ntype: Note\n---\n# Citations\nUnclear source\n",
		"invalid concept":       "---\ntype: [broken\n---\n# Note\n",
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

func TestMigrationPreviewListsPhysicalInventory(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, filepath.Join(root, "index.md"), "# Plain index\n")
	writeCLITestFile(t, filepath.Join(root, "README.md"), "# Ordinary Markdown\n")
	writeCLITestFile(t, filepath.Join(root, "file.json"), "{\"keep\":true}\n")
	writeCLITestFile(t, filepath.Join(root, "child/index.md"), "---\nokf_version: \"0.2\"\n---\n")
	writeCLITestFile(t, filepath.Join(root, "child/concept.md"), "---\ntype: Separate\n---\n")
	snapshot := runMigrationPreview(t, root, 3)
	if snapshot["source_version"] != "unversioned" {
		t.Fatalf("unexpected version: %v", snapshot)
	}
	entries := snapshot["inventory"].([]any)
	indexed := map[string]map[string]any{}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		indexed[entry["path"].(string)] = entry
	}
	if indexed["README.md"]["kind"] != "ordinary_markdown" || indexed["file.json"]["revision"] == nil ||
		indexed["child"]["kind"] != "nested_bundle" || indexed["child/concept.md"] != nil {
		t.Fatalf("unexpected inventory: %v", indexed)
	}
	sub := runMigrationPreview(t, filepath.Join(root, "child"), 0)
	if sub["source_version"] != "0.2" {
		t.Fatalf("unexpected child version: %v", sub["source_version"])
	}
	if data, err := os.ReadFile(filepath.Join(root, "index.md")); err != nil || string(data) != "# Plain index\n" {
		t.Fatalf("preview changed root: %s %v", data, err)
	}
}

func runMigrationPreview(t *testing.T, root string, expectedStatus int) map[string]any {
	t.Helper()
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"migrate", root, "--json"}, nil, &out, &stderr)
	if code != expectedStatus {
		t.Fatalf("exit %d, want %d: %s %s", code, expectedStatus, out.String(), stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMigrationConvertsSixLegacyPathFields(t *testing.T) {
	root := t.TempDir()
	index := "---\nokf_version: \"0.2\"\nlicense: 'MIT' # authored\nx-keep: {path: guide.md}\n---\n"
	writeCLITestFile(t, filepath.Join(root, "index.md"), index)
	writeCLITestFile(t, filepath.Join(root, "Readme.md"), "# Ordinary Markdown\n")
	writeCLITestFile(t, filepath.Join(root, "shared policy.md"), "---\ntype: Policy\n---\n")
	for _, name := range []string{"run.sql", "executor.txt", "attester.py"} {
		writeCLITestFile(t, filepath.Join(root, name), "Asset\n")
	}
	const review = "verified: [{by: 'human:older', at: '2026-01-01T00:00:00Z'}, {by: 'process:later', at: '2026-01-03T00:00:00Z'}]"
	content := "---\ntype: Attested Computation\nruntime: sql\nstatus: deprecated\n" +
		"resource: shared%20policy.md#part\n" +
		"sources: {resource: shared%20policy.md, id: old, x-extra: {preserve: true}}\n" +
		"computation: run.sql\nexecutor: {resource: executor.txt, receipt: [job]}\n" +
		"attester: {resource: attester.py}\nsuperseded_by: shared%20policy.md\n" +
		"generated: {by: writer/1, at: '2026-01-02T00:00:00Z'}\n" + review + "\n---\n"
	writeCLITestFile(t, filepath.Join(root, "sub", "guide.md"), content)
	writeCLITestFile(t, filepath.Join(root, "sub", "run.sql"), "Conflicting asset\n")
	conflict := runMigrationPreview(t, root, 3)
	if !strings.Contains(prettyMigration(conflict), "ambiguous_path") {
		t.Fatalf("missing ambiguity: %v", conflict)
	}
	if err := os.Remove(filepath.Join(root, "sub", "run.sql")); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if status := Run(context.Background(), []string{"migrate", root, "--apply", "--json"}, nil, &out, &stderr); status != 0 {
		t.Fatalf("apply: %s %s", out.String(), stderr.String())
	}
	for _, finding := range []string{"root_path_preserved", "review_tier_changed"} {
		if !strings.Contains(out.String(), finding) {
			t.Fatalf("missing %s from %s", finding, out.String())
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "sub", "guide.md"))
	doc, err := okf.ParseConcept("sub/guide", after)
	if err != nil {
		t.Fatal(err)
	}
	fields := doc.Frontmatter
	if fields["resource"] != "/shared%20policy.md#part" || fields["computation"] != "/run.sql" ||
		fields["superseded_by"] != "/shared%20policy.md" ||
		fields["executor"].(map[string]any)["resource"] != "/executor.txt" ||
		fields["attester"].(map[string]any)["resource"] != "/attester.py" ||
		fields["sources"].(map[string]any)["resource"] != "/shared%20policy.md" ||
		!strings.Contains(string(after), review) {
		t.Fatalf("unexpected migration: %s", after)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "index.md")); string(got) != strings.Replace(index, "0.2", "0.3", 1) {
		t.Fatalf("authored index changed: %s", got)
	}
	if changes := runMigrationPreview(t, root, 0)["changes"].([]any); len(changes) != 0 {
		t.Fatalf("repeat preview: %v", changes)
	}
}

func prettyMigration(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestMigrationRejectsStaleReviewedPlan(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, filepath.Join(root, "note.md"), "---\ntype: Note\n---\n")
	writeCLITestFile(t, filepath.Join(root, "asset.txt"), "original\n")
	plan := runMigrationPreview(t, root, 0)["plan_revision"].(string)
	for _, args := range [][]string{
		{"asset.txt", "changed asset\n"},
		{"another.md", "---\ntype: Added\n---\n"},
	} {
		writeCLITestFile(t, filepath.Join(root, args[0]), args[1])
		var out, stderr bytes.Buffer
		status := Run(context.Background(), []string{"migrate", root, "--apply", "--expect-plan", plan, "--json"}, nil, &out, &stderr)
		if status != 3 || !strings.Contains(out.String(), "stale_plan") {
			t.Fatalf("stale plan accepted: %d %s %s", status, out.String(), stderr.String())
		}
		if _, err := os.Stat(filepath.Join(root, "index.md")); !os.IsNotExist(err) {
			t.Fatalf("stale plan wrote the marker: %v", err)
		}
		plan = runMigrationPreview(t, root, 0)["plan_revision"].(string)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"migrate", root, "--apply", "--expect-plan", plan, "--json"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("reviewed plan failed: %d %s %s", code, stdout.String(), stderr.String())
	}
	if changes := runMigrationPreview(t, root, 0)["changes"].([]any); len(changes) != 0 {
		t.Fatalf("rerun proposed changes: %v", changes)
	}
}

func TestMigrationFailureLeavesRootMarkerForRetry(t *testing.T) {
	root := t.TempDir()
	const oldRoot = "---\nokf_version: \"0.2\"\n---\n"
	writeCLITestFile(t, filepath.Join(root, "index.md"), oldRoot)
	for _, name := range []string{"first", "second"} {
		writeCLITestFile(t, filepath.Join(root, name+".md"), "---\ntype: Note\ntimestamp: 2020-01-01\n---\n")
	}
	store := storage.Local{Root: root, Portable: true}
	inventory, err := inspectMigrationInventory(store)
	if err != nil {
		t.Fatal(err)
	}
	after := map[string][]byte{
		"index": []byte(strings.Replace(oldRoot, "0.2", "0.3", 1)),
	}
	for _, name := range []string{"first", "second"} {
		after[name] = []byte("---\ntype: Note\nlegacy_metadata: {timestamp: 2020-01-01}\n---\n")
	}
	changes := make([]migrationChange, 0, len(after))
	for _, id := range []string{"index", "first", "second"} {
		changes = append(changes, migrationChange{
			Path: id + ".md", Before: migrationRevision(inventory.Inputs[id]), After: migrationRevision(after[id]),
		})
	}
	failure := fmt.Errorf("simulated write failure")
	err = applyMigration(context.Background(), store, inventory, changes, after, func(id string, data []byte) error {
		if id == "second" {
			return failure
		}
		return store.AtomicReplace(id, data)
	})
	if err != failure || changes[0].Applied || !changes[1].Applied || changes[2].Applied {
		t.Fatalf("unexpected failure receipt: %v %v", err, changes)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "index.md")); string(raw) != oldRoot {
		t.Fatalf("marker published after partial failure: %s", raw)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"migrate", root, "--apply", "--json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("retry failed: %d %s %s", code, stdout.String(), stderr.String())
	}
	if changes := runMigrationPreview(t, root, 0)["changes"].([]any); len(changes) != 0 {
		t.Fatalf("retry failed to converge: %v", changes)
	}
}
