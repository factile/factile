---
type: Guide
title: Agents and Local MCP
description: Install Factile agent guidance and use the local stdio MCP server in reader or curator mode.
tags: [factile, agents, codex, mcp, skills]
timestamp: 2026-07-21T00:00:00+02:00
---

# Agents and Local MCP

Factile can install repository- or user-scoped Codex guidance and expose the
same explicit local workspace and root-bundle tree through stdio MCP. Neither
surface connects to a hosted Factile service.

Normal repository onboarding and repeat repair use:

```bash
factile init
```

Its default `--agent auto` behavior upgrades an existing managed repo install
or detects Codex from `.codex/`, `.agents/skills/`, or `AGENTS.md`. A repeat
run preserves the installed reader/curator mode and optional profile. Use
`--agent codex` to request repo guidance or `--agent none` to skip it without
uninstalling anything. Init never modifies user-scoped guidance.

## Advanced inspection and reconfiguration

```bash
factile skill list
factile skill inspect codex
factile skill install codex --scope repo
factile skill doctor codex
```

Use `skill install` when intentionally changing scope, mode, or profile rather
than for ordinary repo repair. Repository scope manages three outputs for one
workspace:

- `.agents/skills/factile/SKILL.md`, the canonical workflow;
- a concise Factile router inside the managed `AGENTS.md` block; and
- the managed Factile MCP block in `.codex/config.toml`.

Reinstalling also removes the retired `factile-discover.sh` helper. User scope
installs only the generated skill for the current user:

```bash
factile skill install codex --scope user
```

Generated ownership is conservative. Init and install refuse to replace an
unrecognized skill at the canonical repo path; there is no force override, and
`--agent none` leaves that path alone. Repeated complete managed blocks are
collapsed to one while preserving all bytes outside the owned regions. Orphan,
reversed, nested, or incomplete markers are malformed and fail before mutation.
Doctor checks repo state even when a user-scoped skill is installed, and uses
the same ownership rules as install and uninstall. All managed paths reject
symlinked ancestors instead of following them.

Reader mode is the default. It emphasizes discovery and configures read-only
MCP. Curator mode adds explicit mutation guidance and a write-capable MCP
command:

```bash
factile skill install codex --scope repo --mode reader
factile skill install codex --scope repo --mode curator --profile software
```

Use doctor for focused diagnostics after installation or a Factile upgrade:

```bash
factile skill doctor codex --json
```

Doctor checks that installed skill content matches the current generator and
that the managed `AGENTS.md` and MCP blocks match its reader or curator mode
and optional profile. It also exercises local list and context commands. Rerun
`factile init` for normal repo repair, or rerun the install with explicit
options when the installation intent itself should change.

Remove only the managed install for the selected scope:

```bash
factile skill uninstall codex --scope repo
```

## Run MCP directly

```bash
factile mcp serve --stdio --read-only
factile mcp serve --stdio
```

Read-only mode exposes workspace discovery, reading, search, context, graph,
validation, mount-status, and Git-refresh operations. Refresh changes generated
cache state only. Write-capable mode additionally exposes document, mount, and
view mutations; source capabilities and revisions are still enforced by the
workspace.

MCP uses the same nearest-ancestor `factile.toml` resolver as the CLI. Starting
it from a secondary bundle does not change the logical `/`; outside a workspace
it returns `no_active_workspace`. An explicit launch may use
`--workspace <directory>` once in the process command.

Use read-only mode unless the session has explicit authority to curate. MCP
uses standard input/output as its protocol channel, so diagnostic prose must
not be written there by wrappers.

## Agent workflow

The installed skill owns the full workflow. Its default path is deliberately
small:

```bash
factile status --json
factile list / --brief --json
factile stat /architecture --json
factile context /architecture "task summary" --json
factile read /architecture/overview --json
```

If the task already names an exact Factile path, inspect or read it directly
after `status`. Use the full `list / --json` only when the complete tree matters,
and run context at the narrowest sensible path. Discover before assuming a path
is local or mounted. A view can narrow context but is not access control.
Existing-document edits require the revision from a fresh read.

The optional software profile supplies templates and recipe data to generated
guidance; it does not create another engine or executable recipe command. See
[Profiles and recipes](../reference/profiles.md).

## Local diagnostics

Set `FACTILE_TRACE_FILE` to append opt-in local JSONL usage events:

```bash
FACTILE_TRACE_FILE=.factile/usage.jsonl \
  factile context / "release process" --json
```

When that relative path is used from the workspace directory, tracing stays in
ignored local state. Tracing is local diagnostics, not hosted audit, analytics,
or billing, and it must not contain credentials.
