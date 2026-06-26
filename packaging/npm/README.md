# Factile npm packages

This directory contains npm package templates and release helpers for the
Factile CLI.

Package roles:

- `factile` is the canonical npm package.
- `@factile/cli` is a scoped alias for users or tooling that need a scoped name.
- `@factile/cli-*` packages contain platform-specific native binaries.

The main packages expose both commands:

```bash
factile
ft
```

Local package smoke test:

```bash
node packaging/npm/scripts/prepare-packages.mjs --build --out /tmp/factile-npm --version 0.1.0
node packaging/npm/scripts/smoke-test.mjs --root /tmp/factile-npm
```

Release publishing runs from `.github/workflows/release.yml` after GoReleaser
creates the GitHub Release archives.

First publish bootstrap:

1. Create a short-lived npm token that can publish new public packages in the
   `factile` account and `@factile` organization.
2. Add it to the GitHub repository as `NPM_TOKEN`.
3. Push the first release tag. The release workflow publishes every package with
   `--provenance --access public`.
4. In npm, configure trusted publishing for each package against this GitHub
   release workflow.
5. Remove the `NPM_TOKEN` secret.

After bootstrap, trusted publishing should authenticate the same workflow through
GitHub OIDC without a long-lived npm token.
