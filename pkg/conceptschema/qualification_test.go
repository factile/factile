package conceptschema

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const suiteRevision = "9ad349be933f1e74810cb4fd3ad19780694dc77e"

type suiteRegistry map[string]any

func (r suiteRegistry) Load(url string) (any, error) {
	if value, ok := r[url]; ok {
		return value, nil
	}
	return nil, fmt.Errorf("unregistered suite resource")
}

// This opt-in qualification uses only a separately acquired, pinned public suite.
// Public unit tests remain offline and do not require the private contract repo.
func TestDraft2020Qualification(t *testing.T) {
	root := os.Getenv("JSON_SCHEMA_TEST_SUITE_ROOT")
	if root == "" {
		t.Skip("set JSON_SCHEMA_TEST_SUITE_ROOT to the pinned public suite checkout")
	}
	for _, check := range []struct {
		args []string
		want string
	}{{[]string{"rev-parse", "HEAD"}, suiteRevision}, {[]string{"status", "--porcelain"}, ""}} {
		args := append([]string{"-C", root}, check.args...)
		out, err := exec.Command("git", args...).Output()
		if err != nil || strings.TrimSpace(string(out)) != check.want {
			t.Fatalf("suite checkout verification: %s %v", out, err)
		}
	}
	read := func(path string) any {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value, err := decodeJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	registry := suiteRegistry{}
	if err := filepath.WalkDir(filepath.Join(root, "remotes"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".json") {
			relative, err := filepath.Rel(filepath.Join(root, "remotes"), path)
			if err != nil {
				return err
			}
			registry["http://localhost:1234/"+filepath.ToSlash(relative)] = read(path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	groups, err := filepath.Glob(filepath.Join(root, "tests/draft2020-12/*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 46 {
		t.Fatalf("required group count %d", len(groups))
	}
	for _, name := range []string{"bignum", "float-overflow", "non-bmp-regex", "refOfUnknownKeyword"} {
		groups = append(groups, filepath.Join(root, "tests/draft2020-12/optional", name+".json"))
	}
	count, excluded := 0, 0
	for _, group := range groups {
		for _, raw := range read(group).([]any) {
			caseData := raw.(map[string]any)
			for _, raw := range caseData["tests"].([]any) {
				test := raw.(map[string]any)
				if filepath.Base(group) == "vocabulary.json" && caseData["description"] == "schema that uses custom metaschema with with no validation vocabulary" && test["description"] == "no validation: invalid number, but it still validates" {
					excluded++
					continue
				}
				count++
				t.Run(filepath.Base(group)+"/"+caseData["description"].(string)+"/"+test["description"].(string), func(t *testing.T) {
					var operationErr error
					defer func() {
						if operationErr != nil {
							t.Fatal(operationErr)
						}
					}()
					defer recoverResource(&operationErr)
					b := &budget{context.Background(), maxWork}
					b.size(caseData["schema"], 0, true)
					c := compiler(b)
					c.UseLoader(registry)
					if err := c.AddResource(resourceURL, caseData["schema"]); err != nil {
						t.Fatal(err)
					}
					schema, err := c.Compile(resourceURL)
					if err != nil {
						t.Fatal(err)
					}
					b.estimate(schema, test["data"], 0, 0)
					actual := schema.Validate(test["data"]) == nil
					if actual != test["valid"] {
						t.Fatalf("valid=%v, want %v", actual, test["valid"])
					}
				})
			}
		}
	}
	if count != 1284 || excluded != 1 {
		t.Fatalf("unexpected suite selection: %d tests, %d excluded", count, excluded)
	}
	t.Logf("configured jsonschema v6.0.3, exact JSON numbers, bounded regex and work: %d official tests; suite %s", count, suiteRevision)
}
