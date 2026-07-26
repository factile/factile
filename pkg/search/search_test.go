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
		{name: "one question term", query: "How?", field: Fields{Path: "/how", Title: "How"}},
		{name: "resource", query: "`factile:test/path`", field: Fields{Path: "/resource", Resource: "factile:test/path"}},
		{name: "path-like concept id", query: "(/docs/file.md)", field: Fields{Path: "/concept", ConceptID: "/docs/file.md"}},
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
