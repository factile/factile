package render

import (
	"strings"
	"testing"
)

func TestRenderMetadataOrdersAndSkipsFields(t *testing.T) {
	r, err := New(Options{ColorMode: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	got := r.RenderMetadata(map[string]any{
		"zeta":        "last",
		"title":       "Guide",
		"type":        "Reference",
		"description": "",
		"tags":        []string{"factile", "cli"},
		"resource":    "https://example.test",
		"timestamp":   "2026-06-26T00:00:00+02:00",
		"revision":    "sha256:abc",
		"writable":    false,
		"alpha":       "first",
		"empty_list":  []string{},
		"nested":      map[string]any{"b": "two", "a": "one"},
	})
	want := strings.Join([]string{
		"Title:     Guide",
		"Type:      Reference",
		"Tags:      factile, cli",
		"Resource:  https://example.test",
		"Timestamp: 2026-06-26T00:00:00+02:00",
		"Rev:       sha256:abc",
		"Writable:  false",
		"Alpha:     first",
		"Nested:    {\"a\":\"one\",\"b\":\"two\"}",
		"Zeta:      last",
	}, "\n")
	if got != want {
		t.Fatalf("metadata output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
	if strings.Contains(got, "---") || strings.Contains(got, "description") || strings.Contains(got, "empty_list") {
		t.Fatalf("metadata rendered raw or empty fields:\n%s", got)
	}
}

func TestRenderMetadataColorStyles(t *testing.T) {
	r, err := New(Options{ColorMode: ColorAlways})
	if err != nil {
		t.Fatal(err)
	}
	got := r.RenderMetadata(map[string]any{"title": "Guide"})
	if !containsANSI(got) || !strings.Contains(got, "Title") || !strings.Contains(got, "Guide") {
		t.Fatalf("expected styled metadata, got %q", got)
	}
}
