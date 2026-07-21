---
type: Guide
title: Getting Started
description: Install Factile, create a workspace and root bundle, read local knowledge, and validate the result.
tags: [factile, cli, getting-started]
timestamp: 2026-07-21T00:00:00+02:00
---

# Getting Started

> The tagged v0.4.0 release already includes Root Layout v2, but predates the
> human-first repeatable init reconciler below. Build this checkout to use that
> newer init behavior until a later release is available.

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
`factile init --workspace . --root .` when the current directory should start
an independent workspace and also be its root bundle. Initialization adds the
anchored ignore rule without creating `.factile/`.

In an interactive terminal, a new workspace asks for only three unresolved
choices: root directory, title, and description. Press Enter to accept `docs`,
the humanized repository name, and a short generated description. Factile then
shows the resolved workspace, root, metadata, and detected agent action before
asking for confirmation.

For a deterministic non-interactive setup, pass the values explicitly:

```bash
factile init --workspace . \
  --root docs \
  --name project-docs \
  --title "Project Docs" \
  --description "Documentation and knowledge for this project." \
  --agent codex \
  --yes --json
```

Bare `init` searches upward for the nearest workspace. If it finds one, it
reconciles that workspace and its selected root even when the current directory
is a secondary bundle. If none exists, the current directory becomes the
workspace. An existing bundle there becomes a combined workspace with root
`.`; otherwise the root defaults to `docs`. `--workspace <directory>` always
selects an exact existing directory for `init` and establishes the boundary
there if needed; `--root <directory>` selects a bundle inside that workspace.

Rerun `factile init` whenever Factile is upgraded or the workspace needs
repair. Existing workspaces skip the setup questions. Init can restore missing
starter documents and refresh detected repo-scoped agent guidance, but it never
overwrites authored Markdown, changes a preserved agent mode or profile,
refreshes Git mounts, or changes user-scoped configuration. An explicit root
change asks for separate confirmation and leaves the previous root untouched.

Before writing, init validates the complete layout and generated ownership. It
refuses an unrecognized canonical skill or malformed managed markers; there is
no force override. Publication is atomic per file, not transactional across the
whole workspace, so an interrupted run can leave complete old and new files.
Rerun the same command to converge safely. If the selected workspace is outside
ordinary discovery from the caller, init prints shell-safe next commands with
an explicit `--workspace` value.

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

Init reports the reconciled metadata, each file action, agent action, and five
ordered local health checks. Warnings still exit successfully. Failed health
returns exit code `3` after emitting the complete text or JSON result, so the
reported problem can be fixed without losing authored content.

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

## Agent setup and diagnostics

`--agent auto` is the init default. It upgrades an existing managed repo
install or detects Codex from `.codex/`, `.agents/skills/`, or `AGENTS.md`.
Use `--agent codex` to request the repo integration or `--agent none` to skip
it. For advanced mode, profile, or user-scope changes, use the dedicated skill
command:

```bash
factile skill install codex --scope repo --mode curator --profile software
factile skill doctor codex
```

Init never changes user-scoped guidance. Doctor is the focused diagnostic when
the generated skill, managed `AGENTS.md` block, or MCP configuration appears
out of sync.

Finish by running `factile validate /`. Continue with
[Reading knowledge](reading-knowledge.md),
[Curating workspaces, mounts, and views](curating-knowledge.md), or
[Editing documents safely](editing-documents.md).
