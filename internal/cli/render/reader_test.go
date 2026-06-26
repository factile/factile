package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestRenderListEmpty(t *testing.T) {
	r, err := New(Options{ColorMode: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := r.RenderList(&out, factile.ListResult{Path: "/empty"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "/empty\nNo entries.\n" {
		t.Fatalf("empty list output = %q", got)
	}
}

func TestRenderReadUsesMetadataAndMarkdown(t *testing.T) {
	r, err := New(Options{ColorMode: ColorNever, Width: 72})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = r.RenderRead(&out, factile.ConceptResult{Concept: factile.Concept{
		Path:     "/docs/guide",
		Revision: "sha256:abc",
		Frontmatter: map[string]any{
			"type":  "Guide",
			"title": "Guide",
			"tags":  []string{"docs", "cli"},
		},
		Markdown: "\n# Guide\n\nRead [Docs](https://example.test).\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"/docs/guide", "Title:", "Guide", "Type:", "Tags:", "docs, cli", "Rev:", "sha256:abc", "Docs", "https://example.test"} {
		if !strings.Contains(got, want) {
			t.Fatalf("read output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "---") || strings.Contains(got, "{") {
		t.Fatalf("read output should not look like raw frontmatter or JSON:\n%s", got)
	}
}
