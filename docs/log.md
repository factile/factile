# Documentation Log

## 2026-07-19

- Published the accepted Root Layout v2 target before implementation: explicit
  repository workspaces, portable bundle manifests, one CWD-invariant logical
  root bundle, separate spatial mount descriptors, and no docs or Git fallback.
- Documented workspace-level `factile.views.toml`, ignored `.factile/` state,
  workspace-local immutable Git snapshots, external credential handling,
  `--workspace`, `no_active_workspace`, and stateless bundle inspection.
- Added prominent transition notes so v2 examples are not mistaken for the
  released v0.3.1 `.factile/config.toml`, `--root`, and `no_active_root`
  behavior while implementation is in progress.

## 2026-07-15

- Aligned contributor and agent instructions with the self-contained `docs`
  root and corrected the documentation validation command to target `/`.
- Established `factile-cli/docs` as the self-contained public authority for
  current CLI architecture, concepts, workflows, command behavior, profiles,
  agents, MCP, and troubleshooting.
- Rewrote retained guidance from current command help, implementation, and
  tests instead of copying the platform archive; excluded speculative research,
  historical execution plans, refinement evidence, and duplicate contract
  prose.
- Removed the obsolete document-type registry requirement from repository
  guidance. OKF documents require a non-empty type but accept domain-specific
  values without a central allowlist.

## 2026-07-14

- Kept the public CLI self-contained by separating cross-repository
  specifications and conformance from ordinary builds, tests, installation,
  release checks, and user guidance.
- Replaced the embedded UI smoke's specification fixture with a small dedicated
  implementation fixture under `testdata/ui-smoke`.

## 2026-07-13

- Documented native Git remote detection,
  read-only mounts, cached revision resolution, 24-hour refresh, stale offline
  reads, and CLI/MCP compatibility.
- Added deterministic implementation coverage with ordinary revision fixtures
  and no live-network dependency.
- Made read-only the normative default for explicit mount creation while
  retaining explicit writable-local and legacy capability inputs.
- Tightened automatic-refresh, credential rejection, SCP classification,
  selector validation, status-surface, and compatibility rules after review.
- Implemented native URI and SCP-style Git mounts through the workspace, CLI,
  local MCP, immutable per-root snapshots, explicit refresh, and offline status.
- Added security hardening and local-only adversarial fixtures for credential
  redaction, cache and repository symlinks, remote hooks, submodules, Git LFS,
  cancellation, concurrency, and read-only mutation enforcement.
- Reserved `.factile` and `.git` as non-public path segments, hardened cache
  state and interrupted-snapshot handling against symlinks, and made source
  status inspection side-effect free.
- Preserved explicit selector presence across descriptors, CLI, and MCP;
  distinguished unavailable revisions from unreachable remotes; and made Git
  validation issues path- and view-scoped.
- Added production-backed coverage for Git source behavior,
  including empty selectors and unavailable refs and revisions.
- Restricted the legacy `--mount-file` registry to non-Git compatibility use
  and made omitted registry writability read-only.
- Reconciled user, contributor, security, command-help, MCP, and agent
  guidance with read-only-by-default explicit mounts and writable-local opt-in.
- Limited Git support to its implemented SHA-1 repository format and 40-hex
  pins, rejecting 64-hex SHA-256 pins before acquisition or descriptor writes.
- Rejected empty as well as non-empty Git URI query and fragment delimiters for
  native and `git+` sources while preserving percent-encoded path data.

## 2026-07-12

- Defined local mount metadata defaults: explicit values first, then source
  root configuration, then root overview metadata, with a mount-path title
  fallback.

## 2026-07-11

- Prepared v0.3.0 with the Excellent Reader embedded UI, complete local bridge
  smoke coverage, and native no-Node runtime verification.
- Consolidated public reader, writer, OKF, and root-layout behavior coverage in
  the open-source `factile` implementation.
- Added the v0.2.0 release-candidate gate, including embedded UI smoke coverage,
  version consistency, npm packaging, cross-platform builds, and public docs
  validation. The private `factile-ui` source remains unpublished.
