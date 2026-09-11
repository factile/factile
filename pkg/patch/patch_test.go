package patch

import (
	"strings"
	"testing"
)

func TestReplaceSectionPreservesFollowingHeading(t *testing.T) {
	markdown := "# Doc\n\n## Flow\n\nold\n\n### Detail\n\nnested\n\n## Next\n\nnext\n"
	got, err := ReplaceSection(markdown, "flow", "new")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## Flow\n\nnew\n\n## Next") {
		t.Fatalf("replacement should be separated from following heading:\n%s", got)
	}
	if strings.Contains(got, "old") || strings.Contains(got, "nested") {
		t.Fatalf("replacement should remove old section body and nested headings:\n%s", got)
	}
}

func TestReplaceSectionMissingHeading(t *testing.T) {
	if _, err := ReplaceSection("# Doc\n", "Missing", "new"); err == nil {
		t.Fatal("expected missing section error")
	}
}

func TestAppendSectionPreservesFollowingHeading(t *testing.T) {
	markdown := "# Doc\n\n## Flow\n\nold\n\n## Next\n\nnext\n"
	got, err := AppendSection(markdown, "Flow", "added")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "old\n\nadded\n\n## Next") {
		t.Fatalf("append should be separated from following heading:\n%s", got)
	}
}

func TestAppendSectionCreatesMissingSection(t *testing.T) {
	got, err := AppendSection("# Doc\n", "Notes", "added")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "# Doc\n\n## Notes\n\nadded\n") {
		t.Fatalf("missing section append created unexpected markdown:\n%s", got)
	}
}

func TestReplaceTextExactAndAmbiguous(t *testing.T) {
	for _, tc := range []struct{ source, old, new, want, code string }{
		{"A [link](/old).", "/old", "/new", "A [link](/new).", ""},
		{"one\r\ntwo", "one\r\ntwo", "three", "three", ""},
		{"remove this", "remove ", "", "this", ""},
		{"same", "same", "same", "same", ""},
		{"old", "missing", "new", "", "text_not_found"},
		{"old old", "old", "new", "", "text_ambiguous"},
		{"aaa", "aa", "new", "", "text_ambiguous"},
		{"old", "", "new", "", "invalid_patch"},
	} {
		got, err := ReplaceText(tc.source, tc.old, tc.new)
		if tc.code != "" {
			if err == nil || err.(*Error).Code != tc.code {
				t.Fatalf("%#v: %v", tc, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%#v: %q %v", tc, got, err)
		}
	}
}

func TestSectionFencesAndDuplicates(t *testing.T) {
	for _, fence := range []string{"```", "~~~~", "   ```"} {
		for _, nl := range []string{"\n", "\r\n"} {
			prefix := strings.Join([]string{"# Doc", fence, "## Steps", "Example", fence, ""}, nl)
			source := prefix + "## Steps" + nl + nl + "Old" + nl + nl + "## Next" + nl + "Keep" + nl
			want := prefix + "## Steps" + nl + nl + "New" + nl + nl + "## Next" + nl + "Keep" + nl
			got, err := ReplaceSection(source, "Steps", "New")
			if err != nil || got != want {
				t.Fatalf("fenced heading: got %q want %q %v", got, want, err)
			}
			if _, err = AppendSection(source+"## Steps"+nl, "Steps", "extra"); err == nil || err.(*Error).Code != "section_ambiguous" {
				t.Fatalf("append ambiguity: %v", err)
			}
			if _, err = ReplaceSection(source+"## Steps"+nl, "Steps", "extra"); err == nil || err.(*Error).Code != "section_ambiguous" {
				t.Fatalf("replace ambiguity: %v", err)
			}
		}
	}
	source := "## Steps\nold\n```\n## Next\n```\n\n## Real\nkeep\n"
	got, err := ReplaceSection(source, "Steps", "new")
	if err != nil || got != "## Steps\n\nnew\n\n## Real\nkeep\n" {
		t.Fatalf("section ends inside fence: %q %v", got, err)
	}
}

func TestUnifiedDiff(t *testing.T) {
	if got := Diff("/guide", "old\n", "new\n"); got != "--- a/guide\n+++ b/guide\n@@ -1,1 +1,1 @@\n-old\n+new\n" {
		t.Fatal(got)
	}
	if got := Diff("/guide", "", "new"); got != "--- a/guide\n+++ b/guide\n@@ -0,0 +1,1 @@\n+new\n\\ No newline at end of file\n" {
		t.Fatal(got)
	}
	if got := Diff("/guide", "same", "same"); got != "" {
		t.Fatal(got)
	}
}
