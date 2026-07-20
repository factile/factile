---
type: Architecture
title: Human Text and JSON Interfaces
description: Current behavior of Factile human output, JSON output, path shorthand, color, errors, and quiet mode.
tags: [factile, cli, text, json, ux]
timestamp: 2026-07-15T00:00:00+02:00
---

# Human Text and JSON Interfaces

Factile serves two command-line audiences from the same results:

- text output helps a person scan and act; and
- JSON output gives scripts and agents stable structured data.

Text is presentation. JSON is the automation contract.

## Choosing output

Text is the default. Use either form for JSON:

```bash
factile list / --json
factile list / --format json
```

Global options may appear before or after the command. `--format text` selects
text explicitly. JSON success values go to standard output. JSON errors go to
standard error as an `error` object containing at least `code` and `message`.

`--quiet` suppresses successful text output. It does not suppress JSON results
or errors. This makes it useful when only an exit status matters without
changing structured automation behavior.

## Workspace summary

Bare `factile` and `factile status` show the same concise summary:
`workspace_dir`, `root_bundle_dir`, `state_dir`, visible knowledge, views,
sources, health, and useful next commands. `current_bundle_dir` may be shown as
informational context, but it never changes the selected logical `/`.

The summary is a starting point, not a recursive dump. Use `list`, `stat`, or
`context` to inspect more.

## Path shorthand

A single argument beginning with `/` is read-first shorthand:

```bash
factile /overview
factile /guides
```

Factile first tries to read the path as a document. If no concept exists there,
it lists the path as a folder. Other failures are returned directly. Explicit
commands remain preferable in scripts because they state the expected result
kind.

## Human rendering

Human output favors shallow navigation:

- `list` prints visible folders and documents;
- `list --brief` prints compact document cards;
- `stat` prints one compact card;
- `read` prints the document Markdown;
- `search` and `context` show ranked or assembled knowledge;
- `validate` prints `valid` or `invalid` plus issues where applicable; and
- mutation commands print short confirmations.

Use `--color auto|always|never` to control terminal styling. `auto` enables
color only for a suitable terminal and respects `NO_COLOR` and `TERM`.

## Help and usage

`factile --help` shows the complete command families. Every command accepts
`--help` and prints its implemented syntax. Invalid syntax returns a non-zero
usage result; operational failures use stable application error codes.

Use the [command reference](../reference/commands.md) for the current syntax and
exit-code classes.
