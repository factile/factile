---
type: Domain Concept
title: Workspaces, Bundles, Paths, Sources, and Views
description: Accepted Root Layout v2 model for locating and composing local Factile knowledge.
tags: [factile, workspaces, bundles, paths, mounts, sources, views]
legacy_metadata:
  timestamp: "2026-07-27T00:00:00+02:00"
---

# Workspaces, Bundles, Paths, Sources, and Views

Factile presents one logical tree assembled from ordinary Markdown bundles.
Five ideas explain the model: workspace, bundle, Factile path, mounted source,
and view.

## Workspace

A workspace is the explicit boundary for composition and mutable local state.
It is a directory containing `factile.toml` with `[workspace]`:

```toml
version = 2

[workspace]
root = "docs"
```

Implicit discovery walks upward to the nearest such file, including across Git
boundaries. An explicit `--workspace <directory>` selects exactly that
directory. `factile init` may establish a workspace in that existing directory;
every other workspace-aware command requires `[workspace]` there. Discovery
does not search nearby `docs/` directories, infer a Git root, or promote a
bundle when no workspace exists. Missing context returns
`no_active_workspace`; init is the explicit onboarding and repair command.

One workspace selects one root bundle and therefore one logical `/`. This is
stable from the workspace root, the root bundle, a mounted bundle, or an
unmounted secondary bundle. A nearer nested workspace intentionally starts a
different logical tree.

## Bundle and root bundle

A bundle is portable OKF content plus a `factile.toml` containing `[bundle]`:

```toml
version = 2

[bundle]
name = "project-docs"
title = "Project Documentation"
description = "Architecture, guides, and runbooks."

[defaults]
format = "okf"
```

The bundle selected by `[workspace].root` is the root bundle. Its local content
is the implicit writable source at `/`. Other contained bundles are not visible
unless the root bundle explicitly mounts them.

A standalone knowledge repository can combine both roles. This is the only
valid reason for one manifest to contain both sections:

```toml
version = 2

[workspace]
root = "."

[bundle]
name = "handbook"
title = "Handbook"
```

The terms are deliberate: *workspace* is the boundary, *bundle* is portable
content, *root* means logical `/`, and *root bundle* is the bundle currently
serving `/`.

## Factile paths

Factile paths describe logical knowledge, not checkout locations:

| Factile path | Typical root-bundle file |
|---|---|
| `/` | selected root bundle |
| `/overview` | `overview.md` |
| `/guides` | `guides/` and optional `guides/index.md` |
| `/guides/setup` | `guides/setup.md` |

Public document paths omit `.md`; input containing `.md` can be normalized.
Paths begin with `/` and use one forward slash between segments. Repeated
slashes and backslashes are invalid. `.factile` and `.git` are private
implementation segments and cannot become public knowledge paths.

`index.md` is directory navigation and `log.md` is chronological history. They
are reserved files rather than document concepts. Other Markdown documents use
YAML frontmatter with a non-empty `type`. Type values are open; there is no
central allowlist.

## Sources and mounts

A descriptor named `<name>.mount.toml` creates a mount path from its physical
location inside the root bundle:

```text
docs/
  reference.mount.toml        -> /reference
  engineering/
    django.mount.toml         -> /engineering/django
```

The descriptor records a source bundle, writability, optional display metadata,
and an optional Git ref or exact revision. Relative local sources resolve from
the descriptor directory. Descriptors outside the root bundle do not compose
the workspace.

Every explicit mount defaults to read-only. A local bundle can opt into writes;
a Git source cannot. Workspace containment is not visibility: mounting is the
only operation that projects a secondary bundle into the logical tree.

## Git bundle selection

A Git source must contain a valid version 2 `factile.toml` at the repository
root of the selected commit. Two layouts are supported:

| Repository layout | Mounted bundle |
|---|---|
| Repository-root `[bundle]` | The repository root. A combined `[workspace]` and `[bundle]` with `root = "."` also works. |
| Repository-root `[workspace]` with `root = "docs"`, plus `docs/factile.toml` containing `[bundle]` | The `docs` bundle. Other contained relative roots work the same way. |

For either layout, mounting at `/coding` exposes the selected bundle's
`practices/boundary-contracts.md` at `/coding/practices/boundary-contracts`.
Title and description defaults come from that bundle. Files outside it remain
outside the mount, and the source workspace's mounts and views are not
activated.

Selection uses strict manifest and workspace-root validation inside the complete
Git snapshot. Missing or malformed manifests, unsafe roots, missing bundles,
and crossed workspace boundaries fail. Symlinks anywhere in the commit are
rejected, even outside the selected bundle. There is no raw-repository fallback
or caller-selected subdirectory override.

This tightens earlier Git behavior: manifestless repositories are no longer
accepted, and workspace repositories expose their selected bundle directly.
Add a valid manifest to the source repository and update any old paths that
included the bundle directory. The same checks apply to existing cached
snapshots, fresh acquisition, reconstruction, validation, and refresh.

## Git source state and credentials

Git sources are materialized as immutable snapshots below the workspace's
`.factile/cache/git/<mount-key>/` directory. `.factile/` is ignored local state,
not tracked configuration or bundle content.
Initialization and migration keep it untracked with the anchored
workspace-root `.gitignore` rule `/.factile/`; adding that rule does not create
state.

A Git mount may follow remote `HEAD`, follow a branch or tag through `--ref`, or
pin one 40-hex SHA-1 commit through `--revision`. Floating sources check at most
once per 24 hours during ordinary use. `factile refresh <mount-path>` checks
immediately. A failed refresh can retain the last usable snapshot as stale.
An invalid candidate never replaces that snapshot or its mounted paths and
metadata. Exact pins never advance, including after cache reconstruction.
Initial selection failures do not create or replace a mount descriptor.

Credentials come from Git credential helpers, an OS keychain, SSH agent/key, or
the process environment. They never belong in `factile.toml`,
`factile.views.toml`, `<name>.mount.toml`, `.factile/` state, status, logs, or
errors. Hosted Factile authentication is not implemented by this layout.

## Views

A view is a named list of logical Factile paths stored in optional
workspace-level `factile.views.toml`:

```toml
[[views]]
id = "onboarding"
title = "Onboarding"
paths = ["/overview", "/guides"]
```

Selecting a view narrows supported reader commands:

```bash
factile list / --view onboarding
factile context / "first contribution" --view onboarding
```

Views do not travel with a bundle, move documents, change paths or writability,
create folders, or grant access. `read` addresses one path directly and does
not take a view.

## Contextual and stateless commands

Reader, curator, writer, mount, refresh, view, UI, MCP, and status operations
require a workspace. The current workspace-free operations are explicitly
physical and require a valid bundle manifest:

```bash
factile bundle find ./some-directory
factile bundle inspect ./some-bundle
```

They find or inspect physical bundles without establishing a logical `/` or
creating workspace state. Raw OKF validity remains independent of a Factile
bundle manifest.

## Revisions

Two revisions answer different questions:

- a document revision identifies the content last read and is required for a
  safe update; and
- a Git source revision identifies the repository snapshot backing a mount.

Do not substitute one for the other. Read a document immediately before a
write, and use a full Git commit when a reproducible source snapshot matters.
