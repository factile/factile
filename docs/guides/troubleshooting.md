---
type: Guide
title: Troubleshooting
description: Diagnose workspace, bundle, path, revision, writability, Git, validation, skill, and MCP failures.
tags: [factile, cli, troubleshooting, errors]
legacy_metadata:
  timestamp: "2026-07-21T00:00:00+02:00"
---

# Troubleshooting

Start with structured state:

```bash
factile status --json
factile mounts --json
factile validate / --json
```

JSON errors contain a stable `code`, message, and optional details.

## No active workspace

`no_active_workspace` means implicit discovery found no ancestor
`factile.toml` containing `[workspace]`.

```bash
factile init
factile status
```

Outside any workspace, bare `init` establishes one in the current directory. An
existing bundle there becomes its root directly; an ordinary directory gets a
`docs/` root by default. Inside an existing workspace it repairs that nearest
boundary instead. To deliberately start an independent workspace in the
current directory, select it exactly:

```bash
factile init --workspace .
```

Add `--root .` if that same directory should also be the root bundle.

Reader discovery does not search a nearby `docs/` directory, stop at or infer a
Git root, or promote a bundle-only `factile.toml`. Init is the explicit command
that can turn the current bundle into a combined workspace. If only
`.factile/config.toml` exists, migrate it explicitly; v2 does not treat
`--root` as a `--workspace` alias.

`invalid_workspace` means a discovered boundary is malformed or unsafe. For
commands other than `init`, it also means the exact directory supplied through
`--workspace` has no valid `[workspace]`; `init` may establish that boundary in
an existing directory.
`invalid_bundle` means the selected root or mounted bundle lacks a valid v2
`[bundle]` manifest.

## Init needs attention

Init writes its complete text or JSON result before returning exit code `3`
when a post-write local health check fails. Inspect the named failed check:

- `workspace_layout`: the workspace and selected root no longer resolve as
  planned;
- `bundle_metadata`: root bundle metadata is missing or inconsistent;
- `required_documents`: the root cannot be listed or its overview cannot be
  read;
- `local_root_validation`: authored local knowledge has an OKF or link error;
  or
- `agent_integration`: a managed repo integration is missing or drifted.

Init does not overwrite an invalid authored document. Fix the reported content
and rerun it. If agent reconciliation was deliberately skipped, rerun with
`--agent codex` to repair managed guidance. Warnings are reported in the same
result but still exit successfully.

An unexpected I/O interruption can leave earlier init-owned files at complete
new versions while later files retain complete old versions. Init publishes
each file atomically but does not roll back or provide one transaction across
the workspace. Rerun the same command: reconciliation converges without
overwriting authored Markdown.

## Init rejects generated ownership

Init and `skill install` refuse to overwrite a skill at the canonical path when
its generated ownership cannot be proven. They also reject orphan, reversed,
nested, or incomplete managed markers, and any managed path with a symlinked
ancestor. These failures happen before workspace files are changed, and there
is no force option. Resolve the collision or malformed block explicitly, then
rerun. Several complete non-nested copies of a managed block are safe: install
collapses them to one while preserving content outside the blocks.

Use `factile skill doctor codex --json` for the exact repo-scoped diagnosis;
doctor does not let a valid user-scoped skill hide broken repo state. If agent
integration should be left untouched while repairing the workspace, run
`factile init --agent none`.

## Missing or ambiguous paths

- `mount_not_found`: the requested folder or mount does not exist.
- `concept_not_found`: no document exists at the path.
- `path_is_not_concept`: an operation requiring a document received a folder.
- `path_is_not_bundle`: `list` received a document.
- `ambiguous_target`: local content and a mount collide at one logical path.
- `invalid_path`: the path is malformed or enters reserved private segments.

Inspect the nearest parent with `list` and inspect mounts before changing
anything:

```bash
factile list / --brief
factile mounts
```

Resolve collisions by giving each logical path one owner; do not hide them with
a view.

## Revision failures

`revision_required` means an existing-document mutation omitted `--rev`.
`revision_mismatch` means the document changed after the revision was read.

Read the document again with `--json`, reconcile the new content, and retry
with the newly returned revision. Do not substitute a Git commit for a document
revision.

## Read-only sources

`source_read_only` is expected for Git mounts and ordinary explicit mounts.
Edit the authoritative source instead. Only a local mount intentionally created
with `--writable` can be curated through the consuming workspace.

## Git source failures

- `remote_source_unavailable`: acquisition or refresh failed and no usable
  snapshot exists.
- `revision_not_available`: the selected ref or commit is absent.
- `unsupported_source`: the source form or operation is unsupported.
- `validation_failed`: the selector, descriptor, or selected Git bundle is invalid.

Check the descriptor and cached status:

```bash
factile mounts --json
factile refresh /mount-path --json
```

A stale result means the last snapshot remains readable after a refresh failure.
Check network, Git, SSH, and credential helpers outside the descriptor. Never
embed a password, token, query, or fragment in the source URI.

Bundle-selection errors include a stable `details.reason` in CLI and MCP JSON:

| Reason | Fix in the source repository |
|---|---|
| `missing_manifest` | Add repository-root `factile.toml` with `version = 2` and `[bundle]`, or `[workspace]` selecting a bundle. Manifestless Git sources are no longer accepted. |
| `invalid_manifest` | Fix the repository-root manifest's TOML, version, required fields, or unknown keys. Combined manifests require `workspace.root = "."`. |
| `invalid_root` | Use a normalized relative root inside the repository. Remove traversal, absolute/private paths, and intermediate workspace boundaries. |
| `missing_bundle` | Create the selected directory and its `factile.toml`. |
| `invalid_bundle` | Give the selected directory a valid version 2 `[bundle]` manifest. |
| `symlink` | Replace symlinks with ordinary files throughout the commit, including outside the selected bundle. |

A failed initial mount does not write or replace its descriptor. An existing
invalid descriptor remains listed, but reads fail and validation reports the
reason at its mount path. An invalid floating refresh retains the previous
usable snapshot and reports stale status with `last_error_code =
"validation_failed"` and `last_error_reason`. Cached sources are validated by
the same rules. Commit the fix, then refresh a floating source; an exact pin
requires explicitly mounting the corrected full SHA-1.

Git mounts support SHA-1 repositories and full 40-hex pinned commits. A
repository-root workspace manifest may select a contained bundle such as
`docs`; there is no caller-selected subdirectory option. SHA-256 object format,
submodule initialization, Git LFS downloads, and repository symlinks are not
supported.

## Validation failures and warnings

Malformed or missing frontmatter and an empty `type` are errors. Broken visible
Markdown links are warnings. Scope validation to isolate a problem:

```bash
factile validate /guides --json
```

Unknown non-empty concept types and unknown frontmatter keys are accepted.

## Agent or MCP setup

```bash
factile init --agent codex
factile skill doctor codex --json
factile skill inspect codex --json
factile mcp serve --stdio --read-only
```

Rerun init for normal repo-scoped repair; it preserves an existing reader or
curator mode and optional profile. Use `skill install` only for an intentional
scope, mode, or profile reconfiguration. For MCP, ensure the client launches
the binary with stdio transport and does not mix log output into the protocol
stream.

Use the [command reference](../reference/commands.md) to confirm syntax before
diagnosing an operational failure.
