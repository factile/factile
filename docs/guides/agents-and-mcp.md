---
type: Guide
title: Agents and Local MCP
description: Install Factile agent guidance and use the local stdio MCP server in reader or curator mode.
tags: [factile, agents, codex, mcp, skills]
timestamp: 2026-07-15T00:00:00+02:00
---

# Agents and Local MCP

Factile can install repository- or user-scoped Codex guidance and expose the
same explicit local workspace and root-bundle tree through stdio MCP. Neither surface connects to a hosted
Factile service.

## Inspect and install guidance

```bash
factile skill list
factile skill inspect codex
factile skill install codex --scope repo
factile skill doctor codex
```

Repository scope installs managed guidance and MCP configuration for one
checkout. User scope installs for the current user:

```bash
factile skill install codex --scope user
```

Reader mode is the default. It emphasizes discovery and configures read-only
MCP. Curator mode adds explicit mutation guidance and a write-capable MCP
command:

```bash
factile skill install codex --scope repo --mode reader
factile skill install codex --scope repo --mode curator --profile software
```

Check generated state after changes:

```bash
factile skill doctor codex --json
```

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

Agents should use explicit JSON commands:

```bash
factile list / --brief --json
factile stat /architecture --json
factile context / "task summary" --json
factile read /architecture/overview --json
```

Discover before assuming a path is local or mounted. A view can narrow context
but is not access control. Existing-document edits require the revision from a
fresh read.

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
