---
type: Guide
title: Getting Started
description: Install Factile, create a workspace and root bundle, read local knowledge, and validate the result.
tags: [factile, cli, getting-started]
timestamp: 2026-07-15T00:00:00+02:00
---

# Getting Started

> **Implementation status:** the Root Layout v2 setup below is the accepted
> target under `ft-qhg`. Released v0.3.1 still creates the legacy
> `.factile/config.toml` layout; check current executable help until the v2
> implementation lands.

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

Local workspaces need no external service. Git mounts additionally need the system
`git` executable; SSH sources use the normal Git and SSH configuration already
available to the user.

## Create a workspace

From a repository:

```bash
factile init
```

The default v2 layout is:

```text
factile.toml                 # [workspace], root = "docs"
.gitignore                   # contains the anchored rule /.factile/
docs/
  factile.toml               # [bundle]
  index.md
  overview.md
```

The workspace selects `docs/` as its root bundle and logical `/`. Use
`factile init --here` only for a standalone directory that should contain one
combined `[workspace]` and `[bundle]` manifest with `root = "."`.
Initialization adds the anchored ignore rule without creating `.factile/`.

Inspect the result:

```bash
factile status
factile list /
factile list / --brief
factile stat /overview
factile read /overview
```

Bare `factile` is the same workspace summary as `factile status`. Status names
the workspace, root bundle, and local state directory separately. A lone path
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

Mount only bundles this workspace's root bundle actually needs:

```bash
factile mount ./reference /reference
factile mount https://github.com/example/public-docs.git /public-docs --ref main
factile mounts
```

Explicit mounts are read-only by default. Opt into `--writable` only for a
local bundle whose authority is intentionally curated through this workspace.
Git sources are always read-only.

## Optional agent setup

Install repository-scoped Codex guidance in reader mode:

```bash
factile skill install codex --scope repo
factile skill doctor codex
```

Finish by running `factile validate /`. Continue with
[Reading knowledge](reading-knowledge.md),
[Curating workspaces, mounts, and views](curating-knowledge.md), or
[Editing documents safely](editing-documents.md).
