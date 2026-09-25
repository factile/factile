package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestHelpRoutesDoNotNeedWorkspace(t *testing.T) {
	missing := t.TempDir() + "/missing"
	for _, command := range [][]string{{"migrate"}, {"patch"}, {"create"}, {"write"}, {"rename"}, {"delete"}, {"deprecate"}, {"mkdir"}, {"view", "set"}, {"mcp", "serve"}} {
		var want bytes.Buffer
		canonical := append(append([]string{}, command...), "--help")
		if code := Run(context.Background(), canonical, nil, &want, &bytes.Buffer{}); code != 0 {
			t.Fatal(command, code)
		}
		for _, args := range [][]string{append([]string{"help"}, command...), append(append([]string{}, command...), "help")} {
			var out, errOut bytes.Buffer
			stdin := strings.NewReader("must not read input")
			args = append([]string{"--workspace", missing}, args...)
			code := Run(context.Background(), args, stdin, &out, &errOut)
			if code != 0 || out.String() != want.String() || errOut.Len() != 0 || stdin.Len() != len("must not read input") {
				t.Fatalf("%v: exit=%d output=%s errors=%s", args, code, out.String(), errOut.String())
			}
		}
	}
	var want bytes.Buffer
	Run(context.Background(), []string{"--help"}, nil, &want, &bytes.Buffer{})
	for _, args := range [][]string{{"help"}, {"--help", "--json"}, {"help", "--help"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, nil, &out, &errOut); code != 0 || out.String() != want.String() || errOut.Len() != 0 {
			t.Fatalf("%v: %d %s", args, code, errOut.String())
		}
	}
}

func TestPatchHelpBatchExample(t *testing.T) {
	dir := patchWorkspace(t)
	read := runCLIJSON[factile.ConceptResult](t, "--workspace", dir, "read", "/guide", "--json")
	_, payload, ok := strings.Cut(patchUsage, "<<'JSON'\n")
	if !ok {
		t.Fatal("missing example")
	}
	payload, _, ok = strings.Cut(payload, "\nJSON")
	if !ok {
		t.Fatal("unterminated example")
	}
	receipt := runCLIJSONWithInput[factile.EditReceipt](t, strings.NewReader(payload), "--workspace", dir, "patch", "/guide", "--rev", read.Concept.Revision, "--input", "-", "--brief", "--json")
	result := runCLIJSON[factile.ConceptResult](t, "--workspace", dir, "read", "/guide", "--json")
	if result.Concept.Markdown != strings.Replace(read.Concept.Markdown, "An old sentence.", "A new sentence.", 1) || result.Concept.Frontmatter["status"] != "stable" || receipt.Revision != result.Concept.Revision {
		t.Fatalf("example did not perform described edits: %#v", result)
	}
	// A literal help word in an edit remains content, not a request for help.
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", dir, "patch", "/guide", "--rev", receipt.Revision, "--replace-text", "A new sentence.", "help", "--brief", "--json"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("literal help: %s", errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Changed {
		t.Fatal("literal help was not applied")
	}
}
