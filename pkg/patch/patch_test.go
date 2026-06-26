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
	got := AppendSection(markdown, "Flow", "added")
	if !strings.Contains(got, "old\n\nadded\n\n## Next") {
		t.Fatalf("append should be separated from following heading:\n%s", got)
	}
}

func TestAppendSectionCreatesMissingSection(t *testing.T) {
	got := AppendSection("# Doc\n", "Notes", "added")
	if !strings.Contains(got, "# Doc\n\n## Notes\n\nadded\n") {
		t.Fatalf("missing section append created unexpected markdown:\n%s", got)
	}
}
