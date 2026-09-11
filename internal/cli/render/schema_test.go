package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestLegacyAndSkippedSchemaCoverage(t *testing.T) {
	r, err := New(Options{ColorMode: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		result factile.ValidationResult
		want   string
	}{
		{factile.ValidationResult{Path: "/", Valid: true}, "Schemas: not evaluated or not reported by this source"},
		{factile.ValidationResult{Path: "/remote", Valid: false, ConceptSchemas: []factile.BundleSchemaValidation{{BundlePath: "/remote", ScopePaths: []string{"/remote"}, CompleteBundle: true, SkippedReason: "Source could not be loaded"}}}, "Schemas /remote (complete bundle): not evaluated; Source could not be loaded"},
	} {
		var out bytes.Buffer
		if err := r.RenderValidation(&out, test.result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), test.want) {
			t.Fatalf("%s", out.String())
		}
	}
}
