# Factile

[![Verify](https://github.com/factile/factile/actions/workflows/verify.yml/badge.svg)](https://github.com/factile/factile/actions/workflows/verify.yml)
[![Release](https://github.com/factile/factile/actions/workflows/release.yml/badge.svg)](https://github.com/factile/factile/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/factile/factile.svg)](https://pkg.go.dev/github.com/factile/factile)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Factile turns docs you own into structured context agents can trust.

Factile is a local-first command line tool for Open Knowledge Format
directories. It exposes Markdown documents through stable Factile paths and
serves the same reader operations through a native Go CLI and a local stdio MCP
server.

Status: early local-first v0.3.0. JSON output is intended as the stable
agent/script contract; CLI text and command ergonomics may still evolve before
v1.0.

Factile reads root-local and explicitly mounted local directories, and can
materialize read-only Git repositories into a generated per-root cache. It does
not implement hosted `factile://` source resolution, hosted MCP, subscriptions,
billing, auth products, marketplace search, publisher portals, remote caches,
or cloud sync in this repository.

Public user and contributor guidance lives in this README, `docs/`, command
help, and self-contained implementation tests. Building, testing, installing,
and using the CLI never requires a separate specification checkout.

## Install

Factile is one Go binary named `factile`.

Local roots and directory mounts need no external runtime. Git mounts require a
system `git` executable on `PATH`; SSH remotes also require the normal SSH
client and agent or key configuration used by Git.

The recommended install method is npm:

```bash
npm install -g factile
factile version
```

The npm package installs the native binary for your platform and also installs
`ft` as a shorter alias. It only installs the binary; repository setup remains
explicit with `factile init`.

If you prefer the scoped package name, it is available as an alias:

```bash
npm install -g @factile/cli
```

Other install methods are available when npm is not the right fit.

Build or install from source:

```bash
go install github.com/factile/factile/cmd/factile@latest
```

GitHub Release archives are published with `checksums.txt` for Linux, macOS,
and Windows. Download the archive for your platform, unpack it, and put
`factile` on your `PATH`.

The installer script supports Linux and macOS. Pin the release tag you want:

```bash
curl -fsSL https://raw.githubusercontent.com/factile/factile/v0.3.0/install.sh | bash
```

From a checkout, build directly:

```bash
go build -o factile ./cmd/factile
./factile version
```

The local browser reader is embedded in the binary. To refresh the embedded
snapshot from a sibling `factile-ui` checkout:

```bash
cd ../factile-ui
npm run build
cd ../factile-cli
make ui-assets
go build -o factile ./cmd/factile
./factile ui --no-open
```

`factile ui --dev-assets http://127.0.0.1:5173` keeps using Vite assets during
UI development. Release binaries do not require Node or npm at runtime.

To smoke-test the embedded UI bridge from a checkout:

```bash
make ui-assets
make smoke-ui
```

The smoke builds the binary, serves the embedded app on loopback in reader and
curator modes, and exercises source metadata, views, lazy lists, read, search,
context, graph, validation, deep SPA fallback, and static assets. It verifies
that reader mode rejects writer operations and that curator mode does not alter
reader results. The server process runs with an empty executable search path,
which proves the embedded app has no Node or npm runtime dependency.

## Quickstart

Initialize Factile in a repository:

```bash
factile init
factile
factile list /
factile list / --brief
factile stat /overview
factile context / "project overview"
```

By default, `factile init` creates a docs-rooted Factile tree:

```text
docs/
  .factile/
    config.toml
  index.md
  overview.md
```

The directory containing `.factile/config.toml` is the active Factile root. A
repository can keep Factile content in `docs/` without making the repository
root itself part of the knowledge tree. Use `factile init --here` when the
current directory should be the root.

Bare `factile` prints a concise workspace summary: the active root, visible
paths, shallow health, and useful next commands. Use `factile --help` for the
full command reference or `factile status --json` for the stable structured
summary.

Use an explicit agent install when needed:

```bash
factile init --agent codex
factile skill install codex --scope repo
```

## Paths

Factile paths are logical paths, not filesystem paths. Public document paths
omit `.md`:

```text
/                         active root
/overview                 docs/overview.md
/runbooks                 docs/runbooks/ or docs/runbooks/index.md
/runbooks/release         docs/runbooks/release.md
```

Reader commands use paths without requiring the caller to classify a path:

```bash
factile list /
factile list / --brief
factile stat /overview
factile read /overview
factile search / "deployment checklist"
factile context / "what should I know before editing?"
factile graph /
factile validate /
```

## Mounts

A mount attaches another source as a child path. `factile mount` writes a
descriptor beside the logical child path:

```bash
factile mount ./reference /reference
factile mount ./working-notes /working-notes --writable
factile mount https://github.com/senseware/coding-practice.git /coding
factile mount git@github.com:senseware/coding-practice.git /coding-ssh --ref main
factile mounts
factile list /reference
factile list /coding
```

Every explicit mount is read-only by default. Only a local directory can opt
into writes with `--writable`; Git mounts are always read-only. The implicit
active-root mount at `/` remains writable in curator mode. `--read-only` remains
accepted as a deprecated compatibility flag.

Native `https://`, `http://`, `ssh://`, `git://`, `file://`, and SCP-style
`user@host:path` Git remotes are accepted as written. A single `git+` prefix is
also supported for compatibility, but is not required. Omit a selector to
follow remote `HEAD`, use `--ref <branch-or-tag>` for a floating named ref, or
use `--revision <40-hex-sha1>` for an immutable pin. Git mounts support SHA-1
object-format repositories; SHA-256 repositories and 64-hex pins are not
supported. `--ref` and `--revision` cannot be combined.

Factile resolves Git content into immutable snapshots under the active root's
`.factile/cache/git/` directory. Floating mounts check for updates when needed
after the previous check is at least 24 hours old. Check immediately with:

```bash
factile refresh /coding
factile mounts
factile status
```

`factile mounts` and `factile status` inspect cached source state without
fetching. If a refresh fails after a successful acquisition, readers keep using
the last snapshot and report it as stale. Without a usable snapshot, the read
fails with `remote_source_unavailable`.

When `--title` or `--description` is omitted, Factile fills each missing field
from the source root's `.factile/config.toml`, then from the root
`overview.md` concept. If no title is available, it humanizes the mount path,
for example `/shared-reference` becomes `Shared Reference`. An unavailable
description remains empty. Explicit flags always win.

Descriptor filenames use `<name>.mount.toml` and are named after the mounted
child:

```text
docs/
  reference.mount.toml
```

Example descriptor:

```toml
source = "https://github.com/senseware/coding-practice.git"
writable = false
title = "Coding Practice"
ref = "main"
```

The mount path comes from the descriptor location. `docs/reference.mount.toml`
creates `/reference`; `docs/engineering/django.mount.toml` creates
`/engineering/django`. Local relative sources resolve from the descriptor file's
directory. Metadata defaults are resolved once when the descriptor is written;
they are not live inherited values.

Remove a descriptor-backed mount with:

```bash
factile unmount /reference
```

## Views

Views are named lenses over existing paths. They live in `.factile/views.toml`
and only narrow reader commands when selected:

```bash
factile view set onboarding --title "Onboarding" \
  --path /overview \
  --path /runbooks
factile list / --view onboarding
factile context / "how do I get started?" --view onboarding
factile validate / --view onboarding
```

Views do not create folders, grant access, change document identity, or change
source writability. `read` remains path-only.

## JSON

Every data-returning command supports stable JSON:

```bash
factile read /overview --json
factile list / --brief --json
factile mounts --json
```

`--format json` remains accepted as a compatibility alias for existing scripts.
Text output is for humans.

## MCP

Run the local stdio MCP server:

```bash
factile mcp serve --stdio --read-only
factile mcp serve --stdio
```

The MCP adapter uses the same workspace API and JSON models as the CLI.
Read-only mode exposes reader tools such as `factile_list`, `factile_stat`,
`factile_read`, `factile_search`, `factile_context`, `factile_graph`,
`factile_validate`, `factile_mounts`, and `factile_refresh`. Refresh only
updates generated Git cache state; it never makes the source writable.
Write-capable mode adds document, mount, unmount, and view mutation tools. Use
`--read-only` for default agent reading.

## Curating Knowledge

Curator workflows manage local paths, descriptors, views, and documents:

```bash
factile mount ./reference /reference --title "Reference"
factile mount ./working-notes /working-notes --writable
factile mounts
factile unmount /reference

factile view list
factile view inspect onboarding
factile view set onboarding --title "Onboarding" --path /overview --path /runbooks
factile view delete onboarding
```

Document write commands require explicit revisions for existing documents:

```bash
factile create /runbooks/example --type Runbook --title "Example" --body ./body.md
factile write /runbooks/example --rev <rev> --body ./body.md
factile patch /runbooks/example --rev <rev> --set title="Updated title"
factile rename /runbooks/example /runbooks/new-example --rev <rev>
factile delete /runbooks/new-example --rev <rev>
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
Curator mode installs write guidance and a write-capable MCP command.

The first profile seed lives under `profiles/software/` as data: a profile
manifest, Markdown templates, and JSON recipes. Recipes are guidance data in
v0.3.0; there is no recipe runner or `factile recipe` command.

## Local Trace

Set `FACTILE_TRACE_FILE` to append local JSONL usage records for CLI and MCP
calls:

```bash
FACTILE_TRACE_FILE=.factile/usage.jsonl factile context / "invoice import" --json
```

Trace logging is local-only and disabled unless the environment variable is
set. These records are opt-in diagnostics, not a hosted audit or billing
ledger.

## Known Limitations

Factile v0.3.0 is intentionally local-first:

- There is no hosted service, hosted `factile://` source resolution, auth
  product, marketplace, billing, publication workflow, or cloud MCP in this
  repository.
- Git support is pull/read only. There are no Git writes, repository
  subdirectory mounts, submodule initialization, Git LFS downloads, background
  refresh daemon, or global shared cache.
- Git snapshots reject repository symlinks. Credentials must come from Git's
  external credential or SSH mechanisms, not from the recorded source URI.
  Literal query or fragment delimiters are rejected even when empty;
  percent-encoded path characters remain valid.
- Recipes are seed guidance data, not executable workflows.
- Text output is a human interface; use JSON for scripts and agents.
- Rename reports backlink warnings; it does not rewrite links automatically.
- Legacy compatibility code remains during the v2 cleanup window, but new local
  workflows should use roots, descriptor mounts, and views.

## Supported Platforms

npm packages and release archives target:

- Linux amd64 and arm64
- macOS amd64 and arm64
- Windows amd64

Source builds require Go 1.26 or newer.
Git mounts additionally require the system Git executable; local-only use does
not.

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

## Prepare a release

CLI releases use the checked-in embedded UI snapshot. Release
artifacts contain the compiled browser assets, not the sibling `factile-ui`
source workspace or its packages.

Before releasing:

```bash
make ui-assets
git diff --exit-code -- pkg/uibridge/static
make pre-release
git status --short
```

Review the candidate and confirm that `main` is clean and synchronized with
`origin`, then run one explicit release target:

```bash
make release-fix      # patch
make release-feature  # minor
make release-major    # major, with confirmation
```

Each release target bumps `VERSION`, synchronizes checked-in version metadata,
runs the release gate, creates a release commit and annotated `vX.Y.Z` tag, and
atomically pushes both to `origin`. The tag starts the GitHub release workflow.

## Project Governance

Source code and ordinary project documentation are licensed under the
[Apache License, Version 2.0](LICENSE). The Factile name, logo, brand assets,
product identity, and related trademarks are reserved; see [NOTICE](NOTICE) and
[TRADEMARKS.md](TRADEMARKS.md).

For contributions and project conduct, see [CONTRIBUTING.md](CONTRIBUTING.md),
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), and [SECURITY.md](SECURITY.md).
