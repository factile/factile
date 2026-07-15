---
type: Guide
title: Troubleshooting
description: Diagnose active-root, path, revision, writability, Git, validation, skill, and MCP failures.
tags: [factile, cli, troubleshooting, errors]
timestamp: 2026-07-15T00:00:00+02:00
---

# Troubleshooting

Start with structured state:

```bash
factile status --json
factile mounts --json
factile validate / --json
```

JSON errors contain a stable `code`, message, and optional details.

## No active root

`no_active_root` means Factile found no `.factile/config.toml` from the selected
working directory or `--root` location.

```bash
factile init
factile status
```

Use `factile init --here` only if the current directory should be the root.
Otherwise allow the default `docs/` root.

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
with `--writable` can be curated through the consuming root.

## Git source failures

- `remote_source_unavailable`: acquisition or refresh failed and no usable
  snapshot exists.
- `revision_not_available`: the selected ref or commit is absent.
- `unsupported_source`: the source form or operation is unsupported.
- `validation_failed`: the recorded selector or descriptor is invalid.

Check the descriptor and cached status:

```bash
factile mounts --json
factile refresh /mount-path --json
```

A stale result means the last snapshot remains readable after a refresh failure.
Check network, Git, SSH, and credential helpers outside the descriptor. Never
embed a password, token, query, or fragment in the source URI.

Git mounts support SHA-1 repositories and 40-hex pinned commits. SHA-256 object
format, subdirectory mounts, submodule initialization, Git LFS downloads, and
repository symlinks are not supported.

## Validation failures and warnings

Malformed or missing frontmatter and an empty `type` are errors. Broken visible
Markdown links are warnings. Scope validation to isolate a problem:

```bash
factile validate /guides --json
```

Unknown non-empty concept types and unknown frontmatter keys are accepted.

## Agent or MCP setup

```bash
factile skill doctor codex --json
factile skill inspect codex --json
factile mcp serve --stdio --read-only
```

Reinstall the managed guidance for the intended scope if doctor reports drift.
For MCP, ensure the client launches the binary with stdio transport and does not
mix log output into the protocol stream.

Use the [command reference](../reference/commands.md) to confirm syntax before
diagnosing an operational failure.
