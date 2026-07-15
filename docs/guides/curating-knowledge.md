---
type: Guide
title: Curating Roots, Mounts, and Views
description: Safely compose local and Git sources, refresh snapshots, manage views, and scaffold directories.
tags: [factile, cli, curator, mounts, views]
timestamp: 2026-07-15T00:00:00+02:00
---

# Curating Roots, Mounts, and Views

Curate only when you own the composition. Reader work normally needs no mount,
view, or root mutation.

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

When omitted, title and description can be derived from source-root metadata or
its `overview.md`; the mount path supplies a final title fallback. The resolved
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

Use credentials through normal Git credential helpers or SSH configuration.
Do not put credentials, query strings, or fragments in recorded source URIs.

## Inspect, refresh, and remove

```bash
factile mounts
factile refresh /public-docs
factile unmount /reference
```

`mounts` and `status` inspect cached state without fetching. `refresh` performs
an immediate Git check. A failed refresh may keep the last snapshot marked
stale; it never turns the source writable. `unmount` removes the descriptor,
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
scopes. Views are lenses only; never use one to hide private material.

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
