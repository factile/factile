package search

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMarkdownPassagesUseHeadingSectionsAndKeepFencedHeadings(t *testing.T) {
	body := "Prelude alpha.\n\n## One\n\nbeta\n```md\n\n## Hidden\n```\n\n### Two\n\ngamma\n"
	passages := markdownPassages(body)
	if len(passages) != 3 {
		t.Fatalf("passages = %#v, want prelude and two real heading sections", passages)
	}
	if got := passageTexts(body, passages); !equalStrings(got, []string{
		"Prelude alpha.\n\n",
		"## One\n\nbeta\n```md\n\n## Hidden\n```\n\n",
		"### Two\n\ngamma\n",
	}) {
		t.Fatalf("passage texts = %#v", got)
	}
	if passages[0].headingLevel != 0 ||
		passages[1].headingLevel != 2 || passages[1].heading != "One" ||
		passages[2].headingLevel != 3 || passages[2].heading != "Two" {
		t.Fatalf("passage headings = %#v", passages)
	}
}

func TestMarkdownPassagesUseParagraphsWithoutHeadings(t *testing.T) {
	body := "First alpha.\nStill first.\n\n- beta one\n- beta two\n\nLast gamma."
	passages := markdownPassages(body)
	if got := passageTexts(body, passages); !equalStrings(got, []string{
		"First alpha.\nStill first.\n",
		"- beta one\n- beta two\n",
		"Last gamma.",
	}) {
		t.Fatalf("headingless passages = %#v", got)
	}
}

func TestMarkdownPassagesDoNotSplitInsideFencedCode(t *testing.T) {
	body := "before\n\n```text\nfirst\n\n## not a heading\nsecond\n```\n\nafter"
	passages := markdownPassages(body)
	if got := passageTexts(body, passages); !equalStrings(got, []string{
		"before\n",
		"```text\nfirst\n\n## not a heading\nsecond\n```\n",
		"after",
	}) {
		t.Fatalf("fenced passages = %#v", got)
	}
}

func TestScoreUsesOnlyTheBestBodyPassage(t *testing.T) {
	scored := Score("lumen harbor", []Fields{
		{
			Path: "/scattered",
			Body: "## First\n" + strings.Repeat("lumen ", 20) +
				"\n\n## Second\n" + strings.Repeat("harbor ", 20),
		},
		{
			Path: "/coherent",
			Body: "## Early\nLumen appears alone.\n\n" +
				strings.Repeat("background ", 30) +
				"\n\n## Recovery\nUse the lumen harbor procedure.",
		},
	})
	if len(scored) != 2 || scored[0].Path != "/coherent" || scored[0].Score != 7 || scored[1].Score != 1.5 {
		t.Fatalf("passage ranking = %#v", scored)
	}
	if !strings.Contains(scored[0].Snippet, "## Recovery") ||
		!strings.Contains(scored[0].Snippet, "lumen harbor") ||
		strings.Contains(scored[0].Snippet, "appears alone") {
		t.Fatalf("winning-passage snippet = %q", scored[0].Snippet)
	}
}

func TestScoreTreatsSubheadingsAsModestEvidence(t *testing.T) {
	scored := Score("needle", []Fields{
		{Path: "/description", Description: "needle"},
		{Path: "/h2-repeated", Body: "## needle needle needle needle\n"},
		{Path: "/h2", Body: "## needle\n"},
		{Path: "/h1", Body: "# needle\n"},
		{Path: "/body", Body: "needle\n"},
	})
	seen := map[string]float64{}
	for _, result := range scored {
		seen[result.Path] = result.Score
	}
	for path, want := range map[string]float64{
		"/description": 6,
		"/h2-repeated": 5,
		"/h2":          3,
		"/h1":          1,
		"/body":        1,
	} {
		if seen[path] != want {
			t.Fatalf("%s score = %v, want %v; results=%#v", path, seen[path], want, scored)
		}
	}
}

func TestScoreBreaksPassageTiesByEarliestSourcePosition(t *testing.T) {
	scored := Score("alpha beta", []Fields{{
		Path: "/doc",
		Body: "## First\nalpha then beta\n\n" +
			"## Second\nalpha then beta\n",
	}})
	if len(scored) != 1 || !strings.Contains(scored[0].Snippet, "## First") || strings.Contains(scored[0].Snippet, "## Second") {
		t.Fatalf("earliest tied passage result = %#v", scored)
	}
}

func TestSnippetChoosesTheSmallestMultiTermSpan(t *testing.T) {
	body := "## Notes\nalpha " + strings.Repeat("padding ", 30) + "alpha near beta"
	snippet := Snippet(body, []string{"alpha", "beta"})
	if !strings.Contains(snippet, "alpha near beta") || strings.Count(snippet, "alpha") != 1 {
		t.Fatalf("compact-span snippet = %q", snippet)
	}
	if len(snippet) > snippetWindowBytes {
		t.Fatalf("snippet length = %d, want at most %d bytes", len(snippet), snippetWindowBytes)
	}
}

func TestSnippetPrefersAnExactPhrasePassage(t *testing.T) {
	body := "## Separated\nalpha appears before beta.\n\n## Phrase\nalpha beta appears here."
	snippet := Snippet(body, []string{"alpha", "beta"})
	if !strings.Contains(snippet, "## Phrase") || strings.Contains(snippet, "## Separated") {
		t.Fatalf("exact-phrase snippet = %q", snippet)
	}
}

func TestScoreKeepsMetadataOnlySnippetEmpty(t *testing.T) {
	scored := Score("violet atlas", []Fields{{
		Path:        "/metadata",
		Description: "Violet Atlas",
		Body:        "Unrelated body text.",
	}})
	if len(scored) != 1 || scored[0].Snippet != "" {
		t.Fatalf("metadata-only result = %#v", scored)
	}
}

func TestSnippetUsesValidUTF8Boundaries(t *testing.T) {
	body := "## Multibyte\n" + strings.Repeat("é", 80) + " needle " + strings.Repeat("界", 60)
	snippet := Snippet(body, []string{"needle"})
	if !utf8.ValidString(snippet) || !strings.Contains(snippet, "needle") {
		t.Fatalf("UTF-8 snippet = %q", snippet)
	}
	if len(snippet) > snippetWindowBytes {
		t.Fatalf("UTF-8 snippet length = %d, want at most %d bytes", len(snippet), snippetWindowBytes)
	}
}

func TestScorePreservesSingleH1SectionCompatibility(t *testing.T) {
	scored := Score("invoice", []Fields{{
		Path:        "/invoice",
		Title:       "Invoice",
		Description: "Invoice guidance.",
		Tags:        []string{"invoice"},
		Body:        "# Invoice Guide\n\nInvoice invoice.",
	}})
	if len(scored) != 1 || scored[0].Score != 41 {
		t.Fatalf("single-section score = %#v, want 41", scored)
	}
	if scored[0].Snippet != "# Invoice Guide  Invoice invoice." {
		t.Fatalf("single-section snippet = %q", scored[0].Snippet)
	}
}

func passageTexts(body string, passages []markdownPassage) []string {
	texts := make([]string, 0, len(passages))
	for _, passage := range passages {
		texts = append(texts, passage.text(body))
	}
	return texts
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
