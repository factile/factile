package okf

import (
	"strings"
	"testing"

	"github.com/factile/factile/pkg/revision"
)

func TestParseConceptPreservesUnknownFrontmatter(t *testing.T) {
	doc, err := ParseConcept("workflows/import", []byte(`---
type: Workflow
title: Import
custom_field: kept
tags: [one, two]
---

# Import
`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Frontmatter["custom_field"] != "kept" {
		t.Fatalf("custom field not preserved: %#v", doc.Frontmatter)
	}
	if got := StringSliceField(doc.Frontmatter, "tags"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("tags not parsed: %#v", got)
	}
	serialized := string(Serialize(doc))
	if !strings.Contains(serialized, "custom_field: kept") {
		t.Fatalf("serialized document lost custom field:\n%s", serialized)
	}
}

func TestParseConceptErrors(t *testing.T) {
	if _, err := ParseConcept("bad", []byte("# Missing")); err == nil {
		t.Fatal("expected missing frontmatter error")
	}
	if _, err := ParseConcept("bad", []byte("---\ntype Workflow\n---\n")); err == nil {
		t.Fatal("expected invalid frontmatter error")
	}
}

func TestReservedAndConceptIDMapping(t *testing.T) {
	if !IsReservedFile("index.md") || !IsReservedFile("log.md") {
		t.Fatal("expected index.md and log.md to be reserved")
	}
	if _, ok := ConceptIDFromRel("index.md"); ok {
		t.Fatal("reserved files must not become concepts")
	}
	id, ok := ConceptIDFromRel("workflows/invoice-import.md")
	if !ok || id != "workflows/invoice-import" {
		t.Fatalf("unexpected concept id: %q %v", id, ok)
	}
	rel, err := RelFromConceptID("/workflows/invoice-import.md")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "workflows/invoice-import.md" {
		t.Fatalf("unexpected rel path: %s", rel)
	}
}

func TestRevisionDigestIsDeterministic(t *testing.T) {
	a := revision.DigestBytes([]byte("same"))
	b := revision.DigestBytes([]byte("same"))
	if a != b || !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("unexpected digest: %s %s", a, b)
	}
}
