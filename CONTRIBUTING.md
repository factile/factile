# Contributing

Factile is a local-first Go CLI and MCP server for Open Knowledge Format bundles.
Phase 1 is intentionally local-only: remote bundles, hosted MCP, billing,
marketplace search, auth, and cloud sync belong outside this repository for now.

Before opening a pull request:

- Keep the change small and focused.
- Prefer simple code over new abstractions.
- Add or update tests for behavior changes.
- Update README or command help when user-facing behavior changes.

Run the local checks before sending a PR:

```bash
gofmt -w .
go test ./...
go vet ./...
./scripts/verify.sh
```

If your Go cache is not writable, set `GOCACHE` to a writable directory:

```bash
GOCACHE=/tmp/factile-go-build-cache ./scripts/verify.sh
```

By submitting a contribution, you agree to license it under the Apache License,
Version 2.0. This does not grant rights to the Factile name, logo, brand assets,
or trademarks; see `TRADEMARKS.md`.
