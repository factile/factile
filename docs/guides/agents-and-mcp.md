---
type: Guide
title: Agents and Local MCP
description: Install Factile agent guidance and use the local stdio MCP server in reader or curator mode.
tags: [factile, agents, codex, mcp, skills]
legacy_metadata:
  timestamp: "2026-07-21T00:00:00+02:00"
generated:
  at: "2026-09-11T09:01:41.929396139Z"
  by: factile/v0.6.0
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

## Skill versions and upgrades

The skill and integration templates are embedded in the native binary. Each
generated `SKILL.md` records the running binary's release in `metadata.version`
and a SHA-256 checksum in `metadata.factile-content-sha256`. The checksum covers
the entire generated file with its own value replaced by an empty string. It
detects local edits, including edits to the mode, profile, and version.

`skill inspect` and `skill install` report the embedded release as `version`.
`skill doctor --json` reports the running release and an `installations` array
for existing repo and user skills. Each entry includes its scope, path,
installed version when available, mode, profile, status, and repair guidance.
Statuses are `current`, `outdated`, `modified`, `unversioned`, `invalid`, or
`unrecognized`. `outdated` means the installed copy differs from this binary;
it can also describe a newer release or a different build with the same label.

Successful ordinary text commands print a short mismatch warning on stderr.
JSON output, `--quiet`, help, version commands, skill commands, and MCP serving
do not print that automatic warning. Checking guidance never rewrites it,
downloads instructions, changes MCP configuration, or prevents a knowledge
read. Doctor remains the explicit diagnostic command for agents using JSON.

After upgrading the binary, run `factile init` in the selected workspace to
refresh an untouched repo skill and its managed integration. Reader/curator
mode and profile are preserved. User guidance is refreshed explicitly with
`factile skill install codex --scope user` and its existing mode/profile options;
doctor prints the complete command. Both paths install the copy bundled with
the running binary, without independently fetching a newer skill.

Versioned skills with local edits or invalid metadata are preserved: init,
install, and uninstall reject them before mutation. Keep custom instructions
outside the generated skill; preserve your edits elsewhere and restore the
generated file, or move it aside before reinstalling. Legacy skills without
version/checksum metadata can still be upgraded by the explicit repair command,
but their local edits cannot be distinguished from old generated content;
review them first. Updating a file does not guarantee that an active agent
session reloads instructions it already read.

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

The installed skill keeps discovery conditional. For a known path, read it
directly. Run `factile status --json` when workspace selection is unclear;
use list, search, or focused context when the document or context is unknown.
A view narrows context, not access permissions.

For authorized document changes, prefer Factile CLI or MCP mutations. Read
once, patch with the observed revision, and reuse the returned revision for
the next edit. Batch related changes to one document with `patch --input -`
and an ordered `operations` array; use `--brief --json` for a compact receipt.
Each document has its own revision. The same concise editing recipe is included
in both skill modes; reader MCP remains read-only, while explicitly authorized
CLI edits still respect project instructions and source permissions.

See [Editing documents](editing-documents.md) for the examples, failure
handling, and validation scope. `factile help patch` describes every patch flag.

The optional software profile supplies templates and recipe data to generated
guidance; it does not create another engine or executable recipe command. See
[Profiles and recipes](../reference/profiles.md).

## Precise MCP editing

When authorized to edit a known document, call `factile_read` directly, then
`factile_patch` with its `concept.revision`:

```json
{
  "path": "/runbooks/cache-recovery",
  "expected_revision": "sha256:<revision-from-read>",
  "operations": [
    {"op": "replace_text", "old": "Restart the cache.", "new": "Restart the cache, then check its health."},
    {"op": "set", "key": "status", "value": "active"}
  ],
  "brief": true,
  "diff": true
}
```

The same shared core handles CLI, MCP, and the local UI bridge's
`/api/local/v1/writer/patch` endpoint. Ordered operations, preservation, reserved
index/log documents, revision errors, and receipts have identical semantics.
The bridge accepts the same request fields. MCP retains its text content and
`structuredContent` envelope; `brief` makes their value the compact receipt.
Reuse its revision for the next edit. Read-only MCP and UI modes still reject
writes. See [Editing documents](editing-documents.md) for operation fields
and the precise validation scope.

## Local diagnostics

Set `FACTILE_TRACE_FILE` to append opt-in local JSONL usage events:

```bash
FACTILE_TRACE_FILE=.factile/usage.jsonl \
  factile context / "release process" --json
```

When that relative path is used from the workspace directory, tracing stays in
ignored local state. Tracing is local diagnostics, not hosted audit, analytics,
or billing, and it must not contain credentials.
