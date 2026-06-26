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
	seen := map[string]bool{}
	for _, item := range scored {
		seen[item.Path] = true
	}
	for _, field := range fields {
		if !seen[field.Path] {
			t.Fatalf("search did not match %s field: %#v", field.Path, scored)
		}
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
