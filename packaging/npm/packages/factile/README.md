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
factile status
factile skill install codex --scope repo
```

Initialization creates tracked workspace configuration at `factile.toml`, a
tracked root-bundle manifest at `docs/factile.toml`, and an anchored
`/.factile/` ignore rule for generated local state. It does not create the
state directory until a command actually needs it.
