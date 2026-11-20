package search

import "testing"

func TestSearchFallbackPreservesLiteralAndAmbiguousTerms(t *testing.T) {
	fields := []Fields{
		{Path: "/editing", Title: "Editing", Body: "## Rename a document\n"},
		{Path: "/troubleshooting", Title: "Troubleshooting"},
		{Path: "/other", Body: "GetDocument rollback rollbeck"},
	}
	for _, test := range []struct{ query, path string }{
		{"rename", "/editing"},
		{"renaming", "/editing"},
		{"renxme", "/editing"},
		{"troubleshoting", "/troubleshooting"},
		{"zxqvplm", ""},
		{"GetDocment", ""},
		{"get_docment", ""},
		{"/troubleshoting", ""},
		{"rollbock", ""},
		{"rollbak", "/other"},
	} {
		got := Score(test.query, fields)
		if test.path == "" {
			if len(got) != 0 {
				t.Errorf("%q unexpectedly matched %v", test.query, got)
			}
		} else if len(got) == 0 || got[0].Path != test.path {
			t.Errorf("%q expected %s, got %v", test.query, test.path, got)
		}
	}
}
