---
type: Guide
title: Contributing
description: Build, test, document, and review changes to the public Factile CLI repository.
tags: [factile, cli, contributing, verification]
timestamp: 2026-07-15T00:00:00+02:00
---

# Contributing

The repository builds one Go binary and keeps CLI, MCP, and the embedded reader
as adapters over the same workspace behavior. Keep changes local, explicit, and
covered at the layer that owns them.

## Local build

Use the Go version declared by the repository. From the checkout:

```bash
go build -o factile ./cmd/factile
./factile version
```

Local-directory behavior needs no external service. Git-source development
uses the system `git` executable and local fixtures; tests do not require live
remotes or credentials.

## Where changes belong

- command parsing and output selection: `internal/cli`;
- human rendering: `internal/cli/render`;
- shared operations and result models: `pkg/factile`;
- root and mount resolution: `pkg/vfs`;
- local content and OKF behavior: `pkg/storage` and `pkg/okf`;
- Git acquisition and cache state: `pkg/gitsource`;
- local MCP and browser adapters: `pkg/mcpserver` and `pkg/uibridge`;
- public user and implementation guidance: `docs/`.

Keep source-capability, locking, and revision checks in the workspace rather
than duplicating them in adapters. Keep JSON results stable; text can improve as
presentation.

## Verification

Run the complete repository gate:

```bash
./scripts/verify.sh
```

It checks formatting, Go tests and vet, target builds, CLI behavior, skills,
workspaces, bundles, mounts, views, writes, npm packaging, and the public
Factile documentation bundle.

For a documentation-only change, also inspect the diff and run:

```bash
factile --workspace . validate /
```

Under Root Layout v2, the repository's `factile.toml` selects `docs/` as its
root bundle. Do not add a sibling checkout, private mount, credential, or host
path to make public verification pass.

Update command help, tests, and current docs together when public behavior
changes. Use `docs/log.md` for durable documentation changes; use the issue
tracker for delivery state instead of creating planning documents in the
knowledge root.
