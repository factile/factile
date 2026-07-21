# Factile CLI

This package installs the `factile` command line tool from the Factile release
binaries.

```bash
npm install -g factile
factile version
```

The `ft` command is also installed as a shorter alias.

Installing this package only installs the CLI binary. Repository setup remains
explicit:

```bash
factile init
```

Normal repository onboarding and repair use one command. Init creates or
reconciles the workspace and root-bundle manifests, starter knowledge, the
ignore rule for local state, and detected repo-scoped agent guidance. It is safe
to rerun after a Factile upgrade or interrupted setup. Use `skill install` only
for advanced scope, mode, or profile reconfiguration.

Inspect the resulting workspace:

```bash
factile status
factile list /
```

Initialization creates tracked workspace configuration at `factile.toml`, a
tracked root-bundle manifest at `docs/factile.toml`, and an anchored
`/.factile/` ignore rule for generated local state. It does not create the
state directory until a command actually needs it.
