# Contributing

Factile is a local-first Go CLI and MCP server for Open Knowledge Format bundles.
It supports local directories and read-only Git sources. Hosted `factile://`
sources, hosted MCP, billing, marketplace search, auth products, publication,
and cloud sync remain outside this repository.

Before opening a pull request:

- Keep the change small and focused.
- Prefer simple code over new abstractions.
- Add or update tests for behavior changes.
- Update README or command help when user-facing behavior changes.
- Keep Git source tests self-contained: construct temporary local repositories
  and bare `file://` remotes, and use test-local URL rewrites when canonical
  HTTPS or SCP spelling must be exercised. Tests must not require GitHub, SSH,
  credentials, or any live network.
- Preserve the Git boundary: no shell-built commands, interactive prompts,
  writable Git mounts, submodule initialization, automatic Git LFS downloads,
  or remote hook execution.

Run the local checks before sending a PR:

```bash
gofmt -w .
go test ./...
go vet ./...
factile validate /docs
./scripts/verify.sh
```

Git source tests require a system `git` executable on `PATH`. The full verify
script also checks Linux, macOS, and Windows builds; do not replace local remote
fixtures with platform-specific shell behavior.

If your Go cache is not writable, set `GOCACHE` to a writable directory:

```bash
GOCACHE=/tmp/factile-go-build-cache ./scripts/verify.sh
```

By submitting a contribution, you agree to license it under the Apache License,
Version 2.0. This does not grant rights to the Factile name, logo, brand assets,
or trademarks; see `TRADEMARKS.md`.
