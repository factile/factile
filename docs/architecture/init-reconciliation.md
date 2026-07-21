---
type: Design
title: Init Reconciliation Contract
description: Implemented human-first and repeatable contract for creating, repairing, and upgrading a Factile workspace.
tags: [factile, cli, init, workspace, reconciliation]
timestamp: 2026-07-21T00:00:00+02:00
---

# Init Reconciliation Contract

`factile init` is the one normal command for repository onboarding and later
repair. It discovers the intended workspace, builds a complete plan, reconciles
only Factile-owned state, verifies the result locally, and reports what changed.
Running it again is safe and useful.

## Command shape

```text
factile init
  [--root <relative-directory>]
  [--name <bundle-name>]
  [--title <bundle-title>]
  [--description <text>]
  [--agent auto|codex|none]
  [--yes]
```

The global `--workspace <directory>`, `--json`, `--format`, `--quiet`, and
`--color` options also apply. There is no compatibility shortcut for a
combined workspace and bundle; use the two explicit directory options.

For `init` only, `--workspace` names the exact existing directory to reconcile
or turn into a workspace. It does not search that directory's ancestors.
Every other workspace-aware command keeps the stricter meaning: an explicit
workspace must already contain a valid `[workspace]` manifest.

## Workspace and root resolution

Resolution has two independent axes. The workspace is the boundary; its
selected root bundle supplies logical `/` everywhere inside that boundary.

| Situation | Workspace directory | Selected root bundle |
|---|---|---|
| Bare `init` inside a workspace | Nearest ancestor workspace | Existing `workspace.root`, unless `--root` is explicit |
| Bare `init` outside a workspace | Current directory | `--root`, otherwise `docs` |
| `init --workspace <dir>` | Exactly `<dir>` | `--root`, existing `workspace.root`, or `docs`, in that order |
| New exact target already containing only `[bundle]` | Exactly the target | `.` when `--root` is omitted |
| `--root .` | Resolved workspace | Workspace directory itself, using one combined manifest |

This means running `init` from a secondary bundle inside a workspace still
reconciles the containing workspace and its selected root. It does not make the
secondary bundle a temporary logical root or hide knowledge from other
mounted bundles.

`--root` is always interpreted relative to the resolved workspace. It must be
`.` or a normalized relative directory that stays inside the workspace.
Absolute paths, parent traversal, `.git`, `.factile`, file collisions,
symlinked path components, malformed manifests, and layouts that would make a
separate root another workspace are rejected before any write. Missing ordinary
directories may be created after the complete plan passes validation.

The selected root must remain in exactly one workspace boundary. Every existing
path component between the selected workspace and root is inspected; an
intermediate manifest containing `[workspace]`, a symlink, or a non-directory
makes the layout invalid. This also applies when such a component appears after
planning. Explicitly creating a workspace inside an older workspace remains
valid when its selected root stays inside the new boundary.

An explicit root that differs from an existing `workspace.root` is an
intentional root change. Init creates or reconciles the new target and updates
the workspace selector, but never moves, copies, edits, or deletes content from
the old root. Interactive use confirms the change before writing. A combined
workspace cannot be changed automatically to a separate root because doing so
would remove the existing manifest's bundle identity; init rejects that case
instead of silently changing what the directory means.

## Metadata defaults and updates

For a new bundle, init accepts machine identity separately from human-facing
metadata:

- `name` defaults to a normalized slug of the workspace directory name, with
  `project` as the final fallback;
- `title` defaults to a humanized form of that name; and
- `description` defaults to `Documentation and knowledge for <title>.`.

`--name`, `--title`, and `--description` override those defaults. The bundle
name is stable identity: a later explicit conflicting name is an error. A later
explicit title or description updates that field. Omitted values preserve
existing metadata.

Slug normalization lowercases ASCII letters, replaces each run of characters
other than letters or digits with one hyphen, and trims surrounding hyphens.
Title humanization replaces hyphens and underscores with spaces and capitalizes
the resulting words.

Metadata belongs in the selected root's `factile.toml`. New starter documents
use the resolved metadata, but existing Markdown remains authored content and
is never rewritten to mirror manifest changes.

## Human and non-interactive operation

On a new workspace with an interactive terminal, bare init asks only for
unresolved human choices:

1. root bundle directory, defaulting to `docs`;
2. bundle title, defaulting from the workspace name; and
3. description, defaulting from the title.

It then shows the detected agent action and the complete plan for confirmation.
The bundle name is derived unless `--name` is supplied. Pressing Enter accepts
each displayed default.

An existing workspace does not repeat the setup questionnaire. A bare rerun
reconciles it directly; an explicit root change receives one confirmation.
EOF, cancellation, or a declined confirmation leaves the filesystem unchanged.

`--yes`, JSON output, a non-interactive input stream, or all five init choices
(`--root`, `--name`, `--title`, `--description`, and `--agent`) supplied
explicitly never prompts. Missing new-workspace values in non-interactive modes
use the same defaults as the interactive flow. Automation can therefore use a
minimal deterministic command such as:

```bash
factile init --workspace . --yes --json
```

Prompting requires both interactive input and output to be actual terminals.
Character devices such as `/dev/null`, pipes, and regular files are
non-interactive and use defaults. A value-taking option followed by another
recognized option is a missing-value error, never data. Global JSON selection
applies to syntax errors regardless of whether it appears before or after
`init`, and every such error occurs before planning or mutation.

## Reconciliation ownership

Init distinguishes desired configuration, generated integration, and authored
content:

| Surface | Ownership | Repeat behavior |
|---|---|---|
| Workspace `factile.toml` | Tracked Factile configuration | Reconcile supported workspace fields; preserve unrelated valid fields |
| Root-bundle `factile.toml` | Tracked Factile configuration | Reconcile supported bundle metadata; preserve `when_to_use`, defaults, and unrelated valid fields |
| Root `index.md` and `overview.md` | Authored project knowledge | Create when missing; never overwrite |
| Workspace `.gitignore` | Shared tracked file | Ensure the anchored `/.factile/` rule without disturbing other entries |
| Repo Factile skill, managed `AGENTS.md` block, and MCP block | Factile-generated integration | Install or refresh current generated content while preserving installation intent |
| Workspace `.factile/` | Ignored local state | Never store authored knowledge, configuration, or credentials |

All predictable validation and planning completes before the first mutation. A
rejected plan, unsafe layout, or ownership collision leaves the workspace
unchanged. Post-write health validation is different: it reports the complete
reconciled result and may return exit code `3`.

Apply revalidates the workspace, root, manifests, output types, and agent paths
after confirmation and before its first mutation. It also preflights every
predictable agent read, write, and legacy cleanup before changing bundle files.
Each init-owned file is published as one complete atomic replacement from the
same directory; new starter Markdown is published only when absent. An
unexpected I/O interruption may leave earlier files at their complete old or
new versions, but never truncated or partially written. Init does not provide a
transaction across files. Rerunning after an interruption converges without
overwriting authored Markdown.

## Agent behavior

`--agent auto` is the default. It selects Codex when an existing repo-scoped
Factile install, `.codex/`, `.agents/skills/`, or `AGENTS.md` is present;
otherwise it skips agent installation. `--agent codex` requests it explicitly.
`--agent none` skips repo-agent reconciliation and does not uninstall an
existing integration.

A new Codex integration uses reader mode with no profile. On a repeat run, init
refreshes the canonical skill and its managed `AGENTS.md` and MCP blocks while
preserving the installed reader or curator mode and optional profile. Init never
changes user-scoped skills or configuration.

Generated ownership must be proven, not inferred from a canonical path. Auto
and explicit Codex reconciliation refuse to overwrite an unrecognized repo
skill and leave the whole plan unchanged; the user resolves that collision.
`--agent none` preserves and skips it without claiming managed health. Managed
blocks have four structural states: absent; one complete, non-nested pair;
several complete, non-nested pairs; or malformed. Install may append an absent
block, replace one pair, or collapse several complete pairs to one while
preserving every byte outside owned regions. Orphan, reversed, nested, or
otherwise ambiguous markers fail before mutation. Doctor evaluates repo state
independently of a user-scoped skill and uses the same ownership and marker
rules as install and uninstall. Repo- and user-scope operations validate every
managed ancestor and never follow a symlink for installation, legacy cleanup,
or removal.

## Verification and result

After writing, init performs five bounded in-process checks in a fixed order:
workspace layout, bundle metadata, required documents, local root validation,
and agent integration. Text output gives a compact human summary with
workspace, root, metadata, file and agent actions, health, and next commands.
JSON exposes the same result as stable structured fields; future fields may be
added without changing existing meanings. Warnings return success. Failed
health returns exit code `3` after the complete text or JSON result is emitted.

Next commands are executable from the directory where init was invoked. When
ordinary upward discovery would not select the newly initialized workspace,
the commands include an explicit, shell-safe `--workspace` selection.

Verification does not refresh Git sources, access hosted Factile, inspect or
store credentials, mutate user scope, or depend on another `factile` executable
being available on `PATH`.

## Non-goals

Init does not:

- migrate, merge, move, or delete existing knowledge;
- discover a nearby `docs` directory outside the workspace model;
- install or uninstall user-scoped agent guidance;
- refresh remote Git mounts or call hosted services;
- manage authentication or credentials;
- provide a force option for ownership collisions or malformed managed state;
- provide rollback or transactionality across several files;
- publish, commit, or push repository changes.

The implementation, human terminal flow, non-interactive JSON flow, repeat
repair path, and failure cases are covered by the repository verification
matrix. Current user guidance lives in
[Getting Started](../guides/getting-started.md),
[Agents and Local MCP](../guides/agents-and-mcp.md), and the
[command reference](../reference/commands.md).
