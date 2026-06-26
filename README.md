# Factile

[![Verify](https://github.com/factile/factile/actions/workflows/verify.yml/badge.svg)](https://github.com/factile/factile/actions/workflows/verify.yml)
[![Release](https://github.com/factile/factile/actions/workflows/release.yml/badge.svg)](https://github.com/factile/factile/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/factile/factile.svg)](https://pkg.go.dev/github.com/factile/factile)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Factile turns docs you own into structured context agents can trust.

Factile is a local-first command line tool for Open Knowledge Format bundles. It
mounts local Markdown knowledge, gives it stable virtual paths, and exposes the
same reader contract through a native Go CLI and a local stdio MCP server.

Status: early local-first v0.1.1. JSON output is intended as the stable
agent/script contract; CLI text and command ergonomics may still evolve before
v1.0.

Factile does not implement remote bundles, hosted MCP, subscriptions, billing,
auth, marketplace search, or cloud sync in this repository.

## Install

Factile is one Go binary named `factile`.

Install with npm:

```bash
npm install -g factile
factile version
```

The npm package also installs `ft` as a shorter alias. It only installs the
binary; repository setup remains explicit with `factile init`.

Build or install from source:

```bash
go install github.com/factile/factile/cmd/factile@latest
```

Release builds are published as GitHub Release archives with `checksums.txt` for
Linux, macOS, and Windows. Download the archive for your platform, unpack it,
and put `factile` on your `PATH`.

The installer script supports Linux and macOS. Pin the release tag you want:

```bash
curl -fsSL https://raw.githubusercontent.com/factile/factile/v0.1.1/install.sh | bash
```

From a checkout, build directly:

```bash
go build -o factile ./cmd/factile
./factile version
```

## Quickstart

Initialize Factile in a repository:

```bash
factile init
factile list /
factile list / --brief
factile stat /project
factile context /project "project overview"
```

By default, `factile init` creates a local knowledge bundle at
`./.factile/knowledge/`, catalogs it under `./.factile/`, and mounts it at
`/project`. If `--agent` is not supplied, Factile installs guidance for
supported agents it detects in the repository.

Use a custom knowledge location or explicit agent:

```bash
factile init --knowledge-base ./knowledge --agent codex
```

Try the bundled fixtures without changing your repository:

```bash
factile --mount-file ./testdata/mounts.toml list /
factile --mount-file ./testdata/mounts.toml list /product-docs
factile --mount-file ./testdata/mounts.toml read /product-docs/workflows/invoice-import
factile --mount-file ./testdata/mounts.toml search /product-docs invoice
factile --mount-file ./testdata/mounts.toml context /product-docs "invoice import workflow"
factile --mount-file ./testdata/mounts.toml graph /product-docs/workflows/invoice-import
factile --mount-file ./testdata/mounts.toml validate /product-docs
```

## Paths

Reader commands use Factile paths instead of filesystem paths. A reader can
navigate `/`, `/project`, `/engineering`, and deeper folders without knowing
whether each path is a catalog, bundle link, folder, or concept.

Common reader commands:

```bash
factile list /
factile list / --brief
factile stat /project
factile read /project/overview
factile search /project "deployment checklist"
factile context /project "what should I know before editing?"
factile graph /project
factile validate /project
```

Use `--view <id>` with `list`, `search`, `context`, and `graph` when a Knowledge
Base defines named Views. Views narrow which bundles are read for that command
without changing canonical document paths.

## JSON

Every data-returning command supports stable JSON:

```bash
factile --mount-file ./testdata/mounts.toml read /product-docs/workflows/invoice-import --json
```

`--format json` remains accepted as a compatibility alias for existing scripts.
Text output is for humans.

## MCP

Run the local stdio MCP server:

```bash
factile --mount-file ./testdata/mounts.toml mcp serve --stdio
factile --mount-file ./testdata/mounts.toml mcp serve --stdio --read-only
```

The MCP adapter uses the same workspace API and JSON models as the CLI. Reader
mode exposes `factile_list`, `factile_stat`, `factile_read`, `factile_search`,
`factile_context`, `factile_graph`, `factile_validate`, and read-only Knowledge
Base inspection tools. Write-capable mode adds catalog and document mutation
tools; use `--read-only` for default agent reading.

## Curating Knowledge

Catalog commands manage local Knowledge Bases:

```bash
factile kb list
factile kb inspect /project
factile kb create /engineering --title "Engineering"
factile kb link /engineering ./testdata/bundles/product-docs /engineering/docs --title "Docs" --read-only
factile kb view set /engineering reader --bundle /engineering/docs
factile kb view delete /engineering reader
factile kb unlink /engineering/docs
```

Document write commands require explicit revisions for existing concepts:

```bash
factile create /project/runbooks/example --type Runbook --title "Example" --body ./body.md
factile write /project/runbooks/example --rev <rev> --body ./body.md
factile patch /project/runbooks/example --rev <rev> --set title="Updated title"
factile rename /project/runbooks/example /project/runbooks/new-example --rev <rev>
factile delete /project/runbooks/new-example --rev <rev>
```

## Agent Guidance

Install local Codex discovery guidance into a repository:

```bash
factile skill install codex --scope repo
factile skill install codex --scope repo --mode curator --profile software
factile skill doctor codex --json
```

Repo-scope install creates local agent guidance and MCP configuration in that
repository. Reader mode is the default and configures MCP with `--read-only`.
Curator mode installs catalog/write guidance and a write-capable MCP command.

The first profile seed lives under `profiles/software/` as data: a profile
manifest, Markdown templates, and JSON recipes. Recipes are guidance data in
v0.1.1; there is no recipe runner or `factile recipe` command.

## Local Trace

Set `FACTILE_TRACE_FILE` to append local JSONL usage records for CLI and MCP
calls:

```bash
FACTILE_TRACE_FILE=.factile/usage.jsonl factile context / "invoice import" --json
```

Trace logging is local-only and disabled unless the environment variable is set.

## Known Limitations

Factile v0.1.1 is intentionally local-only:

- There is no hosted service, remote bundle sync, auth, marketplace, billing, or
  cloud MCP in this repository.
- Recipes are seed guidance data, not executable workflows.
- Text output is a human interface; use JSON for scripts and agents.
- Rename reports backlink warnings; it does not rewrite links automatically.
- The broader documentation bundle is not published in this repository yet; the
  README covers the supported v0.1.1 surface.

## Supported Platforms

Release archives target:

- Linux amd64 and arm64
- macOS amd64 and arm64
- Windows amd64

Source builds require Go 1.26 or newer.

## Verify

Run the repository checks:

```bash
./scripts/verify.sh
```

The verification script runs formatting checks, tests, vet, cross-platform
builds, and CLI smoke tests against fixture bundles.

When GoReleaser is installed locally, validate the release config with:

```bash
goreleaser check
```

If `goreleaser` is not installed, normal repository verification is still
covered by `./scripts/verify.sh`.

## Project Governance

Source code and ordinary project documentation are licensed under the
[Apache License, Version 2.0](LICENSE). The Factile name, logo, brand assets,
product identity, and related trademarks are reserved; see [NOTICE](NOTICE) and
[TRADEMARKS.md](TRADEMARKS.md).

For contributions and project conduct, see [CONTRIBUTING.md](CONTRIBUTING.md),
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), and [SECURITY.md](SECURITY.md).
