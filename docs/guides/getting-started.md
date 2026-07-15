---
type: Guide
title: Getting Started
description: Install Factile, create a root, read local knowledge, and validate the result.
tags: [factile, cli, getting-started]
timestamp: 2026-07-15T00:00:00+02:00
---

# Getting Started

Factile is one native binary. The recommended installation is:

```bash
npm install -g factile
factile version
```

The `@factile/cli` package is an equivalent scoped alias. You can also build
from source with Go:

```bash
go install github.com/factile/factile/cmd/factile@latest
```

Local roots need no external service. Git mounts additionally need the system
`git` executable; SSH sources use the normal Git and SSH configuration already
available to the user.

## Create a root

From a repository:

```bash
factile init
```

The default root is `docs/` and contains `.factile/config.toml`, `index.md`, and
`overview.md`. Use `factile init --here` only when the current directory itself
should be the knowledge root.

Inspect the result:

```bash
factile status
factile list /
factile list / --brief
factile stat /overview
factile read /overview
```

Bare `factile` is the same workspace summary as `factile status`. A lone path
such as `factile /overview` reads a document and falls back to listing when the
path is a folder.

## Find useful context

```bash
factile search / "release process"
factile context / "what should I know before changing releases?"
factile graph /
factile validate /
```

Use JSON for an agent or script:

```bash
factile list / --brief --json
factile context / "release process" --json
```

## Add a source when needed

Mount only knowledge this root actually needs:

```bash
factile mount ./reference /reference
factile mount https://github.com/example/public-docs.git /public-docs --ref main
factile mounts
```

Explicit mounts are read-only by default. Opt into `--writable` only for a
local directory whose authority is intentionally curated through this root.
Git sources are always read-only.

## Optional agent setup

Install repository-scoped Codex guidance in reader mode:

```bash
factile skill install codex --scope repo
factile skill doctor codex
```

Finish by running `factile validate /`. Continue with
[Reading knowledge](reading-knowledge.md),
[Curating roots, mounts, and views](curating-knowledge.md), or
[Editing documents safely](editing-documents.md).
