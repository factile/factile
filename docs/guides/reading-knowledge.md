---
type: Guide
title: Reading Knowledge
description: Navigate, read, search, assemble context, inspect links, and validate a Factile workspace.
tags: [factile, cli, reader, context]
legacy_metadata:
  timestamp: "2026-07-27T00:00:00+02:00"
---

# Reading Knowledge

Reader commands use logical Factile paths and work the same across root-bundle
content, mounted local bundles, and cached Git bundles.

## Read a portable OKF bundle

Use the physical bundle directory when a third-party bundle has no Factile
workspace manifest.

```bash
factile --okf-bundle ./downloaded-bundle validate /
factile --okf-bundle ./downloaded-bundle list /
factile --okf-bundle ./downloaded-bundle mcp serve --stdio
```

The selector opens only that directory and makes it read-only. Use
`--workspace` to read a configured workspace with mounts and views.
An OKF 0.3 reader skips plain Markdown and hidden files, and derives trust
from verification events that apply to the current content. Use
`factile read <path> --include-review --json` to inspect review history and
the current events separately.

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
document. For a local Markdown document, `stat` also shows `source_path`.
Use it to find the owning file when reading through a mount. The card's
`writable` value still describes access from the active workspace.

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

## Read a document's Git history

```bash
factile history /guides/setup
factile history /guides/setup --diff <commit>
```

History follows the owning local Git checkout, including for a mounted
document. A diff compares the selected commit with the current local file.

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
remain case-insensitive literals. When literal search finds nothing, Factile
tries common word endings and unambiguous one-letter corrections for longer
words found in the selected documents. JSON still echoes the original query.

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

Use `--passages` to return the best matching section of each document:

```bash
factile context /guides "rename" --passages --max-tokens 1200 --json
```

Passage mode includes the source path and revision, heading when available,
and original Markdown. It skips link expansion and reports matches without
matching body text or passages that exceed the remaining budget.

## Inspect relationships

```bash
factile graph /
factile graph /architecture --depth 1
```

The graph is derived from Markdown links. It is not a separate database and
does not infer relationships absent from the authored documents.

Absolute links start at the document's owning bundle. In a composed workspace,
an unresolved absolute link can also reach an existing target under an explicit
workspace mount, such as a shared contracts bundle.

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
