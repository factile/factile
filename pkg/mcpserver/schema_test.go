package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestMCPSchemaValidationMatchesWorkspace(t *testing.T) {
	root := t.TempDir()
	copyMCPDir(t, "../../testdata/bundles/schema-profiles", root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})
	for _, readOnly := range []bool{false, true} {
		input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factile_validate","arguments":{"path":"/"}}}` + "\n")
		var out bytes.Buffer
		if err := Serve(context.Background(), ws, input, &out, Options{ReadOnly: readOnly}); err != nil {
			t.Fatal(err)
		}
		response := mcpResponses(t, out.String())[1]
		actual := mcpStructured[factile.ValidationResult](t, response)
		expected, err := ws.Validate(context.Background(), "/", factile.ValidateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(expected)
		if !bytes.Equal(a, b) || actual.Valid || !actual.OKF.Valid || len(actual.SchemaDiagnostics) != 2 {
			t.Fatalf("MCP dropped profile details: %s\n%s", a, b)
		}
		if !reflect.DeepEqual(actual.ConceptSchemas[0].ScopePaths, []string{"/"}) {
			t.Fatal("scope lost")
		}
	}
}
