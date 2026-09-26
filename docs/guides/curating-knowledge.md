---
type: Guide
title: Curating Workspaces, Mounts, and Views
description: Safely compose bundle and Git sources, refresh workspace snapshots, manage views, and scaffold directories.
tags: [factile, cli, curator, mounts, views]
legacy_metadata:
  timestamp: "2026-07-15T00:00:00+02:00"
---

# Curating Workspaces, Mounts, and Views

Curate only when you own the composition. Reader work normally needs no mount,
view, or workspace-composition mutation.

## Mount a local source

```bash
factile mount ./reference /reference
factile mount ./working-notes /working-notes --writable
```

Explicit mounts are read-only unless a local source opts into `--writable`.
`--read-only` is still accepted for compatibility but is unnecessary. Optional
display metadata can be supplied at creation:

```bash
factile mount ./reference /reference \
  --title "Reference" \
  --description "Approved local reference material."
```

When omitted, title and description can be derived from source bundle
`factile.toml` metadata or its `overview.md`; the mount path supplies a final
title fallback. The resolved
values are written into the descriptor rather than inherited live.

## Mount a Git source

```bash
factile mount https://github.com/example/public-docs.git /public-docs
factile mount git@github.com:example/public-docs.git /public-docs-main --ref main
factile mount https://github.com/example/public-docs.git /public-docs-pin \
  --revision 0123456789abcdef0123456789abcdef01234567
```

Omitting a selector follows remote `HEAD`. `--ref` follows a branch or tag.
`--revision` pins one full 40-hex SHA-1 commit. The selectors are mutually
exclusive, and Git mounts cannot be writable.

The repository's selected commit must have a valid version 2 manifest. Use
one of these layouts:

```toml
# factile.toml at the repository root: bundle-only layout
version = 2
[bundle]
name = "coding-practice"
title = "Coding Practice"
```

```toml
# factile.toml at the repository root: nested bundle layout
version = 2
[workspace]
root = "docs"
```

For the nested layout, put the `[bundle]` manifest shown above in
`docs/factile.toml`. A combined root manifest with both sections and
`root = "."` also works. Both layouts expose bundle-relative paths directly;
`docs/practices/boundary-contracts.md` becomes
`/public-docs/practices/boundary-contracts` in the nested example. Source mounts
and views are not imported. Title and description defaults use the selected
bundle and are saved in the descriptor.

Manifestless repositories now fail instead of mounting raw content. Add the
manifest in the source repository and commit it before mounting. The root must
be a contained, normalized relative directory with a valid bundle manifest;
absolute, traversing, private, or crossed-workspace roots fail. Remove all
repository symlinks, including those outside the bundle. See
[Git selection failures](/guides/troubleshooting.md#git-source-failures) for
specific diagnostics. A failed mount leaves an existing descriptor unchanged.

For a reproducible mount, obtain the full SHA-1 of the commit containing the
manifest and content, for example with `git rev-parse HEAD` in that repository,
and pass all 40 hex characters to `--revision`. Abbreviated SHAs do not work.
A pin keeps that commit even when the remote advances or the cache is rebuilt.

Use credentials through normal Git credential helpers, an OS keychain, SSH
agent/key, or the process environment. Do not put credentials, query strings,
or fragments in workspace or bundle manifests, descriptors, state, or recorded
source URIs.

## Inspect, refresh, and remove

```bash
factile mounts
factile refresh /public-docs
factile unmount /reference
```

`mounts` and `status` inspect cached state without fetching. `refresh` performs
an immediate Git check. A failed refresh may keep the last snapshot marked
stale. An invalid bundle update reports `validation_failed` with
`last_error_reason`, retaining the prior paths and metadata. Correct the source
and refresh again, or select a valid full commit SHA. Refresh never moves an
exact pin. `unmount` removes the descriptor,
not the external source repository.

## Manage views

```bash
factile view list
factile view inspect onboarding
factile view set onboarding \
  --title "Onboarding" \
  --description "Small first-contribution scope." \
  --path /overview \
  --path /guides
factile view delete onboarding
```

`view set` creates or replaces one view. Repeat `--path` to select multiple
scopes in workspace-level `factile.views.toml`. Views are lenses only; never
use one to hide private material.

## Scaffold a directory

Use `mkdir` when a writable source needs navigation files:

```bash
factile mkdir /operations --title "Operations" --overview --log
factile mkdir /new-bundle --title "New Bundle" --bundle
```

`--overview` adds a typed overview concept, `--log` adds chronological history,
and `--bundle` adds the bundle-oriented scaffold. Factile refuses to overwrite
an existing path or create inside a read-only source.

After changing composition, run:

```bash
factile status
factile validate /
```
