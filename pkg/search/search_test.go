package search

import (
	"strings"
	"testing"
)

func TestScoreMatchesIndexedFields(t *testing.T) {
	fields := []Fields{
		{Path: "/title", Title: "Needle"},
		{Path: "/tag", Tags: []string{"needle"}},
		{Path: "/description", Description: "needle"},
		{Path: "/concept", ConceptID: "concepts/needle"},
		{Path: "/resource", Resource: "factile:test/needle"},
		{Path: "/body", Body: "needle"},
	}

	scored := Score("needle", fields)
	seen := map[string]float64{}
	for _, item := range scored {
		seen[item.Path] = item.Score
	}
	for path, want := range map[string]float64{
		"/title":       22,
		"/tag":         10,
		"/description": 6,
		"/concept":     5,
		"/resource":    4,
		"/body":        1,
	} {
		if seen[path] != want {
			t.Fatalf("%s score = %v, want %v: %#v", path, seen[path], want, scored)
		}
	}
}

func TestQueryTermsCleanNaturalQuestions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "edge punctuation and scaffolding",
			query: ` HOW?! "Releases," [roll] (back?) `,
			want:  []string{"releases", "roll", "back"},
		},
		{
			name:  "duplicates",
			query: "release release RELEASE?",
			want:  []string{"release"},
		},
		{
			name:  "all scaffolding fallback",
			query: "How? DO!",
			want:  []string{"how", "do"},
		},
		{
			name:  "one token fallback",
			query: "How?",
			want:  []string{"how"},
		},
		{
			name:  "identifier characters",
			query: "`factile:test/path` (/docs/file.md) snake_case kebab-case key:value",
			want:  []string{"factile:test/path", "/docs/file.md", "snake_case", "kebab-case", "key:value"},
		},
		{
			name:  "question framing and common prose",
			query: "how should an existing repository align with shared engineering practices?",
			want:  []string{"existing", "repository", "align", "shared", "engineering", "practices"},
		},
		{
			name:  "all common prose fallback",
			query: "the AND with",
			want:  []string{"the", "and", "with"},
		},
		{
			name:  "negation remains substantive",
			query: "how not to publish",
			want:  []string{"not", "publish"},
		},
		{
			name:  "punctuation only",
			query: "?!",
			want:  nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := queryTerms(tc.query)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("queryTerms(%q) = %#v, want %#v", tc.query, got, tc.want)
			}
		})
	}
}

func TestScoreMatchesPlainTermsAsCompleteTokens(t *testing.T) {
	fields := []Fields{
		{Path: "/cat", Title: "CAT", Body: "A cat rests here."},
		{Path: "/application", Title: "Application", Body: "application"},
		{Path: "/publication", Body: "publication"},
		{Path: "/concatenate", Body: "concatenate"},
		{Path: "/education", Body: "education"},
	}
	scored := Score("(CAT?)", fields)
	if len(scored) != 1 || scored[0].Path != "/cat" || scored[0].Score != 23 {
		t.Fatalf("complete-token score = %#v, want only /cat with unchanged title and body evidence", scored)
	}
	if snippet := Snippet("application publication", []string{"cat"}); snippet != "" {
		t.Fatalf("substring-only snippet = %q, want empty", snippet)
	}
}

func TestScoreMatchesUnicodeLetterAndDigitTokens(t *testing.T) {
	scored := Score("ÉLAN", []Fields{
		{Path: "/word", Body: "élan"},
		{Path: "/prefixed", Body: "préélan"},
		{Path: "/suffixed", Body: "élan42"},
	})
	if len(scored) != 1 || scored[0].Path != "/word" || scored[0].Score != 1 {
		t.Fatalf("Unicode token score = %#v, want only the complete token", scored)
	}
	folded := Score("ΟΣ", []Fields{{Path: "/sigma", Body: "ος"}})
	if len(folded) != 1 || folded[0].Path != "/sigma" {
		t.Fatalf("Unicode case-fold score = %#v, want final sigma match", folded)
	}
}

func TestScoreSaturatesTermFrequencyPerField(t *testing.T) {
	for _, tc := range []struct {
		name        string
		occurrences int
		wantScore   float64
	}{
		{name: "one", occurrences: 1, wantScore: 1},
		{name: "two", occurrences: 2, wantScore: 2},
		{name: "three", occurrences: 3, wantScore: 3},
		{name: "four", occurrences: 4, wantScore: 3},
		{name: "one hundred", occurrences: 100, wantScore: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scored := Score("needle", []Fields{{
				Path: "/doc",
				Body: strings.Repeat("needle ", tc.occurrences),
			}})
			if len(scored) != 1 || scored[0].Score != tc.wantScore {
				t.Fatalf("%d occurrences score = %#v, want %v", tc.occurrences, scored, tc.wantScore)
			}
		})
	}
}

func TestScoreRewardsDistinctTermCoverage(t *testing.T) {
	scored := Score("quartz beacon ember", []Fields{
		{Path: "/partial", Body: strings.Repeat("quartz ", 100)},
		{Path: "/complete", Body: "quartz beacon ember"},
	})
	if len(scored) != 2 || scored[0].Path != "/complete" || scored[0].Score != 8 || scored[1].Path != "/partial" || scored[1].Score != 1 {
		t.Fatalf("coverage ranking = %#v", scored)
	}
}

func TestScoreAddsExactMultiTermPhraseEvidence(t *testing.T) {
	scored := Score("alpha beta", []Fields{
		{Path: "/body-separated", Body: "alpha then beta"},
		{Path: "/body-phrase", Body: "alpha beta"},
		{Path: "/description-phrase", Description: "alpha beta"},
		{Path: "/title-phrase", Title: "alpha beta"},
	})
	seen := map[string]float64{}
	for _, result := range scored {
		seen[result.Path] = result.Score
	}
	for path, want := range map[string]float64{
		"/body-separated":     2,
		"/body-phrase":        7,
		"/description-phrase": 17,
		"/title-phrase":       34,
	} {
		if seen[path] != want {
			t.Fatalf("%s phrase score = %v, want %v; results=%#v", path, seen[path], want, scored)
		}
	}
	for i := 1; i < len(scored); i++ {
		if scored[i-1].Score < scored[i].Score {
			t.Fatalf("scores are not descending: %#v", scored)
		}
	}
}

func TestScoreNaturalQuestionRanksReleaseGuidance(t *testing.T) {
	const query = "how do releases roll back?"
	scored := Score(query, []Fields{
		{
			Path:  "/coding/practices/boundary-contracts",
			Title: "Boundary Contracts",
			Body:  "How do contract releases work? How do boundaries evolve?",
		},
		{
			Path:  "/coding/practices/project/releases",
			Title: "Project Releases",
			Tags:  []string{"releases"},
			Body:  "# Project Releases\n\nRoll back a release by restoring the prior artifact.",
		},
	})
	if len(scored) != 2 || scored[0].Path != "/coding/practices/project/releases" {
		t.Fatalf("natural question ranking = %#v", scored)
	}
	if !strings.Contains(strings.ToLower(scored[0].Snippet), "releases") {
		t.Fatalf("release snippet does not use a substantive term: %q", scored[0].Snippet)
	}
}

func TestScoreDeduplicatesTerms(t *testing.T) {
	fields := []Fields{{Path: "/doc", Body: "needle"}}
	single := Score("needle", fields)
	repeated := Score("needle needle NEEDLE?", fields)
	if len(single) != 1 || len(repeated) != 1 || repeated[0].Score != single[0].Score {
		t.Fatalf("duplicate terms changed score: single=%#v repeated=%#v", single, repeated)
	}
}

func TestScoreFallsBackForQuestionTermsAndPreservesIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		field Fields
	}{
		{name: "all scaffolding", query: "How? DO!", field: Fields{Path: "/question", Body: "how do"}},
		{name: "all common prose", query: "the AND with", field: Fields{Path: "/common", Body: "the and with"}},
		{name: "one question term", query: "How?", field: Fields{Path: "/how", Title: "How"}},
		{name: "resource", query: "`factile:test/path`", field: Fields{Path: "/resource", Resource: "factile:test/path"}},
		{name: "path-like concept id", query: "(/docs/file.md)", field: Fields{Path: "/concept", ConceptID: "/docs/file.md"}},
		{name: "snake case", query: "snake_case", field: Fields{Path: "/snake", Body: "prefixsnake_casevalue"}},
		{name: "kebab case", query: "kebab-case", field: Fields{Path: "/kebab", Body: "prefixkebab-casevalue"}},
		{name: "key value", query: "key:value", field: Fields{Path: "/key-value", Body: "prefixkey:value"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scored := Score(tc.query, []Fields{tc.field})
			if len(scored) != 1 || scored[0].Path != tc.field.Path {
				t.Fatalf("Score(%q) = %#v", tc.query, scored)
			}
		})
	}
}

func TestScoreUsesPathTieBreaks(t *testing.T) {
	scored := Score("needle", []Fields{
		{Path: "/b", Resource: "needle"},
		{Path: "/a", Resource: "needle"},
	})
	if len(scored) != 2 {
		t.Fatalf("expected two matches, got %#v", scored)
	}
	if scored[0].Path != "/a" || scored[1].Path != "/b" {
		t.Fatalf("expected path tie-break ordering, got %#v", scored)
	}
}

func TestScoreEmptyQueryReturnsNoMatches(t *testing.T) {
	if scored := Score("", []Fields{{Path: "/doc", Title: "Anything"}}); len(scored) != 0 {
		t.Fatalf("empty query should not score documents, got %#v", scored)
	}
}

func TestSnippetReturnsNearbyTextAndNormalizesNewlines(t *testing.T) {
	body := "Intro line before the match.\nThe invoice import workflow handles supplier invoices.\nFinal line."
	snippet := Snippet(body, []string{"invoice"})
	if snippet == "" {
		t.Fatal("expected snippet for matching term")
	}
	if strings.ContainsAny(snippet, "\r\n") || !strings.Contains(snippet, "invoice import workflow") {
		t.Fatalf("unexpected snippet: %q", snippet)
	}
	if got := Snippet(body, []string{"missing"}); got != "" {
		t.Fatalf("missing term snippet = %q, want empty", got)
	}
}
