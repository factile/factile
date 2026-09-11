package uibridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestSchemaValidationAcrossReaderAndWriterBridge(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/bundles/schema-profiles")); err != nil {
		t.Fatal(err)
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	expected, err := ws.Validate(context.Background(), "/", factile.ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, curator := range []bool{false, true} {
		handler := NewHandler(ws, Options{Curator: curator})
		responses := []*httptest.ResponseRecorder{request(handler, http.MethodGet, APIPrefix+"/reader/validate?path=%2F")}
		if curator {
			responses = append(responses, requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/validate", `{"path":"/"}`))
		}
		for _, response := range responses {
			if response.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			var actual factile.ValidationResult
			if err := json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(actual)
			b, _ := json.Marshal(expected)
			if !bytes.Equal(a, b) || actual.Valid || !actual.OKF.Valid || len(actual.SchemaDiagnostics) != 2 {
				t.Fatalf("bridge lost schema result: %s", response.Body.String())
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "concept-schemas/huge.schema.json"), bytes.Repeat([]byte(" "), (1<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	response := request(NewHandler(ws, Options{}), http.MethodGet, APIPrefix+"/reader/validate?path=%2F")
	if response.Code == http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("schema_resource_limit")) {
		t.Fatalf("resource failure hidden: %d %s", response.Code, response.Body.String())
	}
}
