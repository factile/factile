---
type: Guide
title: Reading Knowledge
description: Navigate, read, search, assemble context, inspect links, and validate a Factile root.
tags: [factile, cli, reader, context]
timestamp: 2026-07-15T00:00:00+02:00
---

# Reading Knowledge

Reader commands use logical Factile paths and work the same across root-local,
mounted local, and cached Git sources.

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

When a root defines a relevant view, inspect it before using it:

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
