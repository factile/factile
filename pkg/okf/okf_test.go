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

func TestParseConceptAcceptsStructuredYAMLFrontmatter(t *testing.T) {
	doc, err := ParseConcept("datasets/orders", []byte(`---
type: Data Model
title: Orders
metadata:
  owner: analytics
  reviewed: true
  priority: 2
aliases:
  - orders
  - completed_orders
notes: |
  First line.
  Second line.
---

# Orders
`))
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := doc.Frontmatter["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata not parsed as map: %#v", doc.Frontmatter["metadata"])
	}
	if metadata["owner"] != "analytics" || metadata["reviewed"] != true || metadata["priority"] != int64(2) {
		t.Fatalf("metadata not preserved: %#v", metadata)
	}
	aliases, ok := doc.Frontmatter["aliases"].([]any)
	if !ok || len(aliases) != 2 || aliases[0] != "orders" || aliases[1] != "completed_orders" {
		t.Fatalf("aliases not parsed as list: %#v", doc.Frontmatter["aliases"])
	}
	if doc.Frontmatter["notes"] != "First line.\nSecond line.\n" {
		t.Fatalf("block scalar not preserved: %#v", doc.Frontmatter["notes"])
	}
	serialized := string(Serialize(doc))
	for _, want := range []string{"metadata:\n", "  owner: analytics\n", "aliases: [orders, completed_orders]\n", "notes: \"First line.\\nSecond line.\\n\""} {
		if !strings.Contains(serialized, want) {
			t.Fatalf("serialized document missing %q:\n%s", want, serialized)
		}
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
