package okf

import (
	"os"
	"strings"
	"testing"
)

func TestEditsPreserveSourceBytes(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/bundles/editing/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, nl := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll(string(raw), "\n", nl)
		doc, err := ParseConcept("guide", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		body := strings.Replace(doc.Markdown, "An old sentence.", "A new sentence.", 1)
		got, err := ReplaceMarkdown("guide", []byte(source), body)
		if err != nil || string(got) != strings.Replace(source, "An old sentence.", "A new sentence.", 1) {
			t.Fatalf("body preservation: %s %v", got, err)
		}
		got, err = PatchFrontmatter("guide", []byte(source), map[string]any{"status": "active"}, []string{"title"})
		want := strings.Replace(strings.Replace(source, "status: draft", "status: active", 1), "title: \"Editing Guide\""+nl, "", 1)
		if err != nil || string(got) != want {
			t.Fatalf("metadata preservation:\ngot %q\nwant %q\n%v", got, want, err)
		}
		got, err = PatchFrontmatter("guide", []byte(source), map[string]any{"title": "Editing Guide"}, nil)
		if err != nil || string(got) != source {
			t.Fatalf("no-op changes bytes: %s %v", got, err)
		}
		got, err = PatchFrontmatter("guide", []byte(source), map[string]any{"tags": []string{"three"}}, nil)
		want = strings.Replace(source, "tags:"+nl+"  - one"+nl+"  - two"+nl, "tags: [three]"+nl, 1)
		if err != nil || string(got) != want {
			t.Fatalf("list preservation:\ngot %q\nwant %q\n%v", got, want, err)
		}
	}
}

func TestReservedDocuments(t *testing.T) {
	for _, id := range []string{"index", "log", "nested/index", "nested/log"} {
		for _, source := range []string{"# Plain\r\n", "---\r\ntype: Guide\r\n---\r\n# Typed\r\n", "---\ntitle: Untyped reserved\n---\nBody\n"} {
			doc, err := ParseConcept(id, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReplaceMarkdown(id, []byte(source), doc.Markdown+"Added\r\n")
			if err != nil || string(got) != source+"Added\r\n" {
				t.Fatalf("reserved edit: %q %v", got, err)
			}
		}
	}
	for _, id := range []string{"guide", "nested/guide"} {
		if _, err := ParseConcept(id, []byte("# Plain\n")); err == nil {
			t.Fatal("ordinary concept accepted without frontmatter")
		}
	}
	for _, source := range []string{"---\nbroken\n---\nBody", "---\ntitle: Missing closing delimiter"} {
		if _, err := ParseConcept("index", []byte(source)); err == nil {
			t.Fatal("malformed reserved frontmatter accepted")
		}
	}
}

func TestMetadataBlockScalarAndDelimiterAtEOF(t *testing.T) {
	source := "---\ntype: Guide\ndescription: |\n  # literal body text\n  line two\n# Comment on next field\ntitle: \"Keep\"\n---"
	got, err := PatchFrontmatter("guide", []byte(source), map[string]any{"description": "Changed"}, nil)
	want := "---\ntype: Guide\ndescription: Changed\n# Comment on next field\ntitle: \"Keep\"\n---"
	if err != nil || string(got) != want {
		t.Fatalf("block scalar content: %q %v", got, err)
	}
	got, err = ReplaceMarkdown("guide", got, "Body\n")
	if err != nil || string(got) != want+"\nBody\n" {
		t.Fatalf("EOF delimiter: %q %v", got, err)
	}
}
