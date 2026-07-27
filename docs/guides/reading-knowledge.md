---
type: Guide
title: Reading Knowledge
description: Navigate, read, search, assemble context, inspect links, and validate a Factile workspace.
tags: [factile, cli, reader, context]
timestamp: 2026-07-27T00:00:00+02:00
---

# Reading Knowledge

Reader commands use logical Factile paths and work the same across root-bundle
content, mounted local bundles, and cached Git bundles.

## Start shallow

```bash
factile status
factile list /
factile list / --brief
factile list /guides
factile stat /guides/setup
```

`list` shows immediate folders and documents. `--brief` returns compact cards
with useful metadata. `stat` inspects one path without loading the complete
document.

Read a known document explicitly:

```bash
factile read /guides/setup
```

For interactive scanning, a lone path is convenient:

```bash
factile /guides/setup
factile /guides
```

It tries `read` first, then `list` only when no document exists at the path.

## Search and context

Search returns ranked document matches:

```bash
factile search / "database migration"
factile search /operations "rollback"
```

Context starts with search and assembles matching documents within a token
budget:

```bash
factile context / "how do releases roll back?"
factile context / "release rollback" --max-tokens 8000 --depth 1
```

Search ignores common question framing such as `how do` and conservative prose
terms such as `a`, `an`, `the`, and `with` when substantive terms remain. A
query made only of those terms keeps them as a deliberate fallback. Plain words
match complete Unicode letter-or-digit tokens, so `cat` does not match
`application`. Paths, resources, and terms containing identifier punctuation
remain case-insensitive literals. Search does not stem words or infer synonyms,
and JSON results still echo the original query.

Ranking keeps the established metadata field weights, caps repeated evidence
for one term after its third occurrence in a field, and scales multi-term
scores by the share of distinct query terms the document matches. Body evidence
comes from the best single Markdown passage: a heading-bounded section, or a
blank-line paragraph when the document has no headings. Fenced code remains
inside its surrounding passage. Subheadings add modest evidence without
duplicating an H1 title, and exact multi-term phrases in descriptions and the
winning body passage receive a small fixed bonus.

Each result snippet comes from that same winning passage and favors an exact
phrase or the smallest span covering the matched terms. Context preserves the
Search order but still returns complete Markdown documents. These deterministic
rules favor broad, coherent evidence without turning search into semantic
retrieval.

`--depth 0` disables related-link expansion. `--depth 1` includes one-hop links
and backlinks; deeper values are not supported. Omitted documents are reported
when they do not fit the budget.

## Inspect relationships

```bash
factile graph /
factile graph /architecture --depth 1
```

The graph is derived from Markdown links. It is not a separate database and
does not infer relationships absent from the authored documents.

## Narrow with a view

When a workspace defines a relevant view, inspect it before using it:

```bash
factile view list
factile view inspect onboarding
factile list / --view onboarding
factile search / "setup" --view onboarding
factile context / "first contribution" --view onboarding
factile graph / --view onboarding
factile validate / --view onboarding
```

A view narrows scope; it does not change paths or access. `read` remains
path-only.

## Validate what you read

```bash
factile validate /
factile validate /guides
```

Malformed concepts are errors. Broken visible Markdown links are warnings. A
warning does not by itself make the report invalid, but it should be reviewed.

Use `--json` for every scripted or agent call. It preserves result fields that
human text intentionally summarizes.
