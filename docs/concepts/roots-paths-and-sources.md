---
type: Domain Concept
title: Roots, Paths, Sources, and Views
description: User-facing model for locating and composing local Factile knowledge.
tags: [factile, roots, paths, mounts, sources, views]
timestamp: 2026-07-15T00:00:00+02:00
---

# Roots, Paths, Sources, and Views

Factile presents one logical tree assembled from ordinary Markdown directories.
Four ideas explain most local behavior: the active root, Factile paths, mounted
sources, and views.

## Active root

An active root is the directory containing `.factile/config.toml`. Factile
discovers it from the selected working directory or `--root` location.

`factile init` creates a docs-rooted layout by default:

```text
repository/
  docs/
    .factile/
      config.toml
    index.md
    overview.md
```

Use `factile init --here` when the current directory itself should be the root.
The active root's content is the implicit writable source mounted at `/`.

## Factile paths

Factile paths describe logical knowledge, not checkout locations:

| Factile path | Typical root-local file |
|---|---|
| `/` | active root |
| `/overview` | `overview.md` |
| `/guides` | `guides/` and optional `guides/index.md` |
| `/guides/setup` | `guides/setup.md` |

Public document paths omit `.md`; input containing `.md` can be normalized.
Paths begin with `/`. `.factile` and `.git` are private implementation
segments and cannot become public knowledge paths.

`index.md` is directory navigation and `log.md` is chronological history. They
are reserved files rather than document concepts. Other Markdown documents use
YAML frontmatter with a non-empty `type`. Type values are open; there is no
central allowlist.

## Sources and mounts

A source is a directory that contributes knowledge. Factile currently supports:

- the local active root;
- an explicitly mounted local directory; and
- an explicitly mounted read-only Git repository.

A descriptor named `<child>.mount.toml` creates a mount path from its physical
location. For example:

```text
docs/
  reference.mount.toml        -> /reference
  engineering/
    django.mount.toml         -> /engineering/django
```

The descriptor records the source, writability, optional display metadata, and
an optional Git ref or exact revision. Relative local sources resolve from the
descriptor's directory.

Every explicit mount defaults to read-only. A local source can opt into writes;
a Git source cannot. The logical tree therefore composes knowledge without
making every source editable from every consumer.

## Git source state

Git sources are materialized as immutable snapshots below the active root's
`.factile/cache/git/` directory. That cache is generated state, not authored
knowledge.

A Git mount may:

- follow remote `HEAD` when no selector is given;
- follow a branch or tag through `--ref`; or
- pin one 40-hex SHA-1 commit through `--revision`.

Floating sources check at most once per 24 hours during ordinary use. An
explicit `factile refresh <mount-path>` checks immediately. When a later refresh
fails, Factile can keep serving the last usable snapshot as stale. A source
without any usable snapshot fails closed.

## Views

A view is a named list of existing Factile paths stored in
`.factile/views.toml`. Selecting a view narrows supported reader commands:

```bash
factile list / --view onboarding
factile context / "first contribution" --view onboarding
```

Views do not move documents, change their paths, alter writability, create
folders, or grant access. `read` addresses one path directly and does not take a
view.

## Revisions

Two revisions answer different questions:

- a document revision identifies the content last read and is required for a
  safe update; and
- a Git source revision identifies the repository snapshot backing a mount.

Do not substitute one for the other. Read the document immediately before a
write, and use a full Git commit when a reproducible source snapshot matters.
