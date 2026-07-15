---
type: Reference
title: Command Reference
description: Compact syntax reference for the implemented Factile CLI surface.
tags: [factile, cli, commands, reference]
timestamp: 2026-07-15T00:00:00+02:00
---

# Command Reference

The current command shape is:

```text
factile [global options] (<command> [args] | <path>)
```

Run `factile --help` or `factile <command> --help` for executable usage.
Global options may appear before or after a command.

## Global options

| Option | Purpose |
|---|---|
| `--root <path>` | Select the exact Factile root containing `.factile/config.toml`. |
| `--mount-file <path>` | Use the legacy local-source registry. Git mounts still require an active root. |
| `--json` | Emit stable structured results. |
| `--format text\|json` | Select output explicitly; JSON is the compatibility-equivalent of `--json`. |
| `--color auto\|always\|never` | Control human terminal styling. |
| `--quiet` | Suppress successful text output. |
| `--version` | Print build version information. |
| `--help` | Print the full command overview. |

## Bootstrap and summary

```text
factile init [--here] [--agent <agent>]
factile
factile status
factile version
factile <path>
```

`init` creates or reuses a root; `--here` uses the current directory instead of
the default `docs/` root. `--agent codex` installs supported agent guidance as
part of initialization. A lone `/path` reads a document first and lists only
when no document exists at that path.

## Reader commands

```text
factile list     [path] [--brief] [--view <id>]
factile stat     <path>
factile read     <document-path>
factile search   <path> <query> [--view <id>]
factile context  <path> <query> [--max-tokens <n>] [--depth 0|1] [--view <id>]
factile graph    <path> [--depth 0|1] [--view <id>]
factile validate <path> [--view <id>]
factile ui       [--port <port>] [--no-open] [--dev-assets <url>] [--curator]
```

`ui` serves the embedded browser on loopback. Reader mode is the default;
`--curator` enables local write routes. `--dev-assets` loads browser assets from
the given development server while keeping the local workspace API.

## Mount and view commands

```text
factile mount <source> <mount-path>
  [--ref <ref> | --revision <40-hex-sha1>]
  [--writable] [--read-only]
  [--title <title>] [--description <text>]

factile refresh <mount-path>
factile unmount <mount-path>
factile mounts

factile view list
factile view inspect <id>
factile view set <id> --title <title> --path <path>
  [--description <text>]
factile view delete <id>
```

Repeat `--path` on `view set` to select more than one scope. Explicit mounts
default read-only; only a local source can use `--writable`. `--read-only` is a
deprecated compatibility flag.

## Directory and document writes

```text
factile mkdir <path> [--title <title>] [--log] [--overview] [--bundle]

factile create <document-path>
  --type <type> --title <title> --body <file>

factile write <document-path> --rev <rev> --body <file>

factile patch <document-path> --rev <rev> [patch options]

factile rename <old-path> <new-path> --rev <rev>
factile delete <document-path> --rev <rev>
factile deprecate <document-path> --rev <rev> --reason <text>
```

Patch options are:

```text
--set <key=value>
--delete-key <key>
--replace-section <heading> <file>
--append-section <heading> <file>
--replace-body <file>
```

The options may be repeated. All existing-document writes require the current
document revision.

## Bundle inspection

```text
factile bundle find [path]
factile bundle inspect <source>
```

These commands discover and inspect local OKF directories. They do not publish
or install remote bundles.

## Skills and MCP

```text
factile skill list
factile skill inspect codex
factile skill install codex --scope repo|user
  [--mode reader|curator] [--profile software]
factile skill uninstall codex --scope repo|user
factile skill doctor codex

factile mcp serve --stdio [--read-only]
```

## Exit codes

| Code | Class |
|---:|---|
| `0` | success |
| `1` | general failure or an unsuccessful doctor check |
| `2` | invalid path syntax, unsupported command, or command usage |
| `3` | validation or OKF parsing failure |
| `4` | missing root, mount, path, concept, or wrong path kind |
| `5` | existing destination, missing/stale revision, or missing patch section |
| `6` | read-only, unsafe, unsupported, or unavailable source/revision |
| `7` | partial failure |
| `8` | lock timeout |

Use JSON error codes rather than parsing human messages.
