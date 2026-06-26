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
factile skill install codex --scope repo
```
