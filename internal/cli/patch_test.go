package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/mcpserver"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/revision"
)

func patchWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir("../../testdata/bundles/editing")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile("../../testdata/bundles/editing/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, entry.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCLIAndMCPPatchParity(t *testing.T) {
	for _, tc := range []struct{ name, path, extra, input, code string }{
		{"exact portable numbers", "/guide", "", `{"set":{"extension":{"count":9007199254740993,"ratio":0.12345678901234567890123456789}},"operations":[{"op":"set","key":"observation","value":9007199254740993123456789}]}`, ""},
		{"ordered exact metadata", "/guide", "", `{"operations":[{"op":"replace_text","old":"An old sentence.","new":"New sentence."},{"op":"replace_text","old":"New sentence.","new":"Final sentence."},{"op":"set","key":"status","value":"active"}]}`, ""},
		{"repeated section", "/guide", "", `{"operations":[{"op":"append_section","heading":"Notes","markdown":"First"},{"op":"append_section","heading":"Notes","markdown":"Second"}]}`, ""},
		{"fence", "/guide", "", `{"replace_sections":{"Steps":"Changed"}}`, ""},
		{"duplicate section", "/guide", "\n## Steps\nMore", `{"replace_sections":{"Steps":"Changed"}}`, "section_ambiguous"},
		{"ambiguous text", "/guide", "", `{"operations":[{"op":"replace_text","old":"Steps","new":"Changed"}]}`, "text_ambiguous"},
		{"missing text", "/guide", "", `{"operations":[{"op":"replace_text","old":"not here","new":"Changed"}]}`, "text_not_found"},
		{"atomic failure", "/guide", "", `{"operations":[{"op":"set","key":"status","value":"active"},{"op":"replace_text","old":"not here","new":"Changed"}]}`, "text_not_found"},
		{"plain index", "/index", "", `{"operations":[{"op":"replace_text","old":"Guide","new":"New guide"}]}`, ""},
		{"plain log", "/log", "", `{"operations":[{"op":"replace_text","old":"Initial entry.","new":"Initial entry.\nNext entry."}]}`, ""},
		{"stale", "/guide", "", `{"expected_revision":"stale","set":{"status":"active"}}`, "revision_mismatch"},
		{"missing revision", "/guide", "", `{"set":{"status":"active"}}`, "revision_required"},
		{"invalid type", "/guide", "", `{"delete_keys":["type"]}`, "validation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cliDir, mcpDir := patchWorkspace(t), patchWorkspace(t)
			filename := strings.TrimPrefix(tc.path, "/") + ".md"
			before, err := os.ReadFile(filepath.Join(cliDir, filename))
			if err != nil {
				t.Fatal(err)
			}
			before = append(before, []byte(tc.extra)...)
			// Exercise exact CRLF preservation through both adapters.
			before = []byte(strings.ReplaceAll(string(before), "\n", "\r\n"))
			for _, dir := range []string{cliDir, mcpDir} {
				if err = os.WriteFile(filepath.Join(dir, filename), before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var args map[string]any
			decoder := json.NewDecoder(strings.NewReader(tc.input))
			decoder.UseNumber()
			if err = decoder.Decode(&args); err != nil {
				t.Fatal(err)
			}
			if tc.name != "missing revision" && tc.name != "stale" {
				args["expected_revision"] = revision.DigestBytes(before)
			}
			args["brief"], args["diff"] = true, true
			cliInput, _ := json.Marshal(args)
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"--workspace", cliDir, "patch", tc.path, "--input", "-", "--json"}, bytes.NewReader(cliInput), &stdout, &stderr)
			args["path"] = tc.path
			request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "factile_patch", "arguments": args}})
			var mcpOut bytes.Buffer
			ws := factile.NewWorkspace(factile.WorkspaceOptions{Workspace: mcpDir})
			if err = mcpserver.Serve(context.Background(), ws, bytes.NewReader(request), &mcpOut, mcpserver.Options{}); err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Result struct {
					Structured json.RawMessage `json:"structuredContent"`
					Content    []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
				Error struct {
					Message string `json:"message"`
					Data    struct {
						Code    string         `json:"code"`
						Details map[string]any `json:"details"`
					} `json:"data"`
				} `json:"error"`
			}
			cliBytes, _ := os.ReadFile(filepath.Join(cliDir, filename))
			mcpBytes, _ := os.ReadFile(filepath.Join(mcpDir, filename))
			comparableMCP := string(mcpBytes)
			responseMCP := mcpOut.String()
			cliDoc, _ := okf.ParseConcept(strings.TrimSuffix(filename, ".md"), cliBytes)
			mcpDoc, _ := okf.ParseConcept(strings.TrimSuffix(filename, ".md"), mcpBytes)
			if generated, ok := cliDoc.Frontmatter["generated"].(map[string]any); ok {
				other, ok := mcpDoc.Frontmatter["generated"].(map[string]any)
				if !ok || generated["by"] != other["by"] {
					t.Fatal("producer differs")
				}
				if _, ok := okf.Datetime(generated["at"]); !ok {
					t.Fatal("invalid CLI change time")
				}
				if _, ok := okf.Datetime(other["at"]); !ok {
					t.Fatal("invalid MCP change time")
				}
				comparableMCP = strings.ReplaceAll(comparableMCP, other["at"].(string), generated["at"].(string))
				responseMCP = strings.ReplaceAll(responseMCP, other["at"].(string), generated["at"].(string))
				responseMCP = strings.ReplaceAll(responseMCP, revision.DigestBytes(mcpBytes), revision.DigestBytes(cliBytes))
			}
			if err = json.Unmarshal([]byte(responseMCP), &envelope); err != nil {
				t.Fatal(err)
			}
			if string(cliBytes) != comparableMCP {
				t.Fatalf("saved bytes differ: CLI %q MCP %q", cliBytes, mcpBytes)
			}
			if tc.name == "exact portable numbers" {
				for _, number := range []string{"9007199254740993", "0.12345678901234567890123456789", "9007199254740993123456789"} {
					if !bytes.Contains(cliBytes, []byte(number)) || !bytes.Contains(mcpBytes, []byte(number)) {
						t.Fatalf("portable number lost: %s: CLI %s MCP %s", number, cliBytes, mcpBytes)
					}
				}
			}
			if tc.code != "" {
				var cliError struct {
					Error factile.AppError `json:"error"`
				}
				if err = json.Unmarshal(stderr.Bytes(), &cliError); err != nil {
					t.Fatalf("CLI stderr: %s", stderr.String())
				}
				if code == 0 || cliError.Error.Code != tc.code || envelope.Error.Data.Code != tc.code || envelope.Error.Message != cliError.Error.Message || !reflect.DeepEqual(envelope.Error.Data.Details, cliError.Error.Details) {
					t.Fatalf("errors differ: CLI %s MCP %s", stderr.String(), mcpOut.String())
				}
				if !bytes.Equal(cliBytes, before) {
					t.Fatal("failed edit changed bytes")
				}
			} else {
				if code != 0 {
					t.Fatalf("CLI error: %s", stderr.String())
				}
				var cliValue, mcpValue any
				if err = json.Unmarshal(stdout.Bytes(), &cliValue); err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal(envelope.Result.Structured, &mcpValue); err != nil {
					t.Fatalf("MCP response: %s", mcpOut.String())
				}
				if !reflect.DeepEqual(cliValue, mcpValue) {
					t.Fatalf("receipts differ: CLI %s MCP %s", stdout.String(), mcpOut.String())
				}
				if len(envelope.Result.Content) != 1 {
					t.Fatalf("MCP text envelope: %s", mcpOut.String())
				}
				var textValue any
				if err = json.Unmarshal([]byte(envelope.Result.Content[0].Text), &textValue); err != nil || !reflect.DeepEqual(textValue, mcpValue) {
					t.Fatal("MCP text does not match structuredContent")
				}
				if strings.Contains(stdout.String(), `"concept"`) || strings.Contains(stdout.String(), `"markdown"`) {
					t.Fatal("brief echoes document")
				}
				if tc.name == "repeated section" && (!bytes.Contains(cliBytes, []byte("First")) || !bytes.Contains(cliBytes, []byte("Second"))) {
					t.Fatal("repeated operation lost")
				}
			}
		})
	}
}

func TestPatchFlagsAreOrderedAndJSONStrict(t *testing.T) {
	dir := patchWorkspace(t)
	read := runCLIJSON[factile.ConceptResult](t, "--workspace", dir, "read", "/guide", "--json")
	receipt := runCLIJSONWithInput[factile.EditReceipt](t, strings.NewReader("Added"), "--workspace", dir, "patch", "/guide", "--rev", read.Concept.Revision, "--append-section", "Notes", "-", "--replace-text", "Added", "Replaced", "--set", "status=active", "--delete-key", "status", "--set", "status=done", "--brief", "--json")
	got := runCLIJSON[factile.ConceptResult](t, "--workspace", dir, "read", "/guide", "--json")
	if !strings.Contains(got.Concept.Markdown, "Replaced") || got.Concept.Frontmatter["status"] != "done" || receipt.Revision != got.Concept.Revision {
		t.Fatalf("order: %#v", got)
	}
	first := filepath.Join(t.TempDir(), "first.txt")
	second := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(first, []byte("First"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("Second"), 0600); err != nil {
		t.Fatal(err)
	}
	result := runCLIJSON[factile.ConceptResult](t, "--workspace", dir, "patch", "/guide", "--rev", receipt.Revision, "--append-section", "Notes", first, "--append-section", "Notes", second, "--json")
	if !strings.Contains(result.Concept.Markdown, "First") || !strings.Contains(result.Concept.Markdown, "Second") {
		t.Fatal("repeated flag discarded")
	}
	for _, input := range []string{`{"operatoins":[]}`, `{"operations":[{"op":"replace_text","old":"Guide","new":"New","typo":true}]}`, `{} {}`, `{"operations":"wrong"}`} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), []string{"--workspace", dir, "patch", "/guide", "--rev", result.Concept.Revision, "--input", "-", "--json"}, strings.NewReader(input), &out, &errOut); code != 2 {
			t.Fatalf("bad JSON exit %d: %s", code, errOut.String())
		}
	}
}

func TestPatchReceiptGolden(t *testing.T) {
	dir := patchWorkspace(t)
	if err := os.WriteFile(filepath.Join(dir, "log.md"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", dir, "patch", "/log", "--rev", revision.DigestBytes([]byte("old\n")), "--replace-text", "old", "new", "--brief", "--diff", "--json"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("patch: %s", errOut.String())
	}
	golden, err := os.ReadFile("../../testdata/golden/edit-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != strings.TrimSpace(string(golden)) {
		t.Fatalf("receipt contract:\n%s\nwant:\n%s", out.String(), golden)
	}
}
