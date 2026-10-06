# Factile npm packages

This directory contains npm package templates and release helpers for the
Factile CLI.

The public user-facing install command is:

```bash
npm install -g factile
```

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
node packaging/npm/scripts/prepare-packages.mjs --build --out /tmp/factile-npm --version 0.6.2
node packaging/npm/scripts/smoke-test.mjs --packages-dir /tmp/factile-npm
```

The smoke installs the packed native binary into a clean npm project, confirms
workspace-free commands fail without creating state, inspects a detached
bundle, checks the packaged one-command onboarding guidance, initializes the v2
workspace layout, and exercises contextual reads.

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

Trusted publishing requires two-factor authentication on the npm owner account.
Configure every package, including all five platform packages, to trust the
GitHub repository `factile/factile`, workflow filename `release.yml`, with no
environment restriction and permission to run `npm publish`. With a current npm
CLI and an authenticated owner session, the command for each package is:

```bash
npm trust github <package> --repo=factile/factile --file=release.yml --allow-publish
```

If the GitHub release succeeds but npm publication fails before publishing any
package, repair the npm authentication settings and retry the existing release:

```bash
gh workflow run release.yml --repo factile/factile --ref main -f tag=v0.6.2
```

This downloads and verifies the existing release archives against `checksums.txt`,
then prepares, smoke-tests, and publishes the npm packages. It does not rebuild
or replace the GitHub release. If some npm packages already published, inspect
that partial publication before retrying; published npm versions are immutable.
