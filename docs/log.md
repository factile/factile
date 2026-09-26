# Documentation Log

## 2026-09-26

- Documented repository-root and nested Git bundle layouts, strict manifest
  selection across acquisition/cache/refresh, stable selection diagnostics,
  failure-before-descriptor persistence, and full-SHA pinning (ft-diw.2).

## 2026-09-11

- Added native OKF 0.2 metadata diagnostics and reserved-index validation, with
  plain/version-only root health and shared CLI/Server conformance checks.
- Fixed frontmatter key-order collection at the projection budget boundary and
  explicit Core tag validation (ft-0zn). Added parser and CLI JSON regressions;
  verified the shared EOF/tag and schema-definition cases against Server.

## 2026-09-10

- Aligned top-level and editing-command help with exact and batched patches,
  added help command routes, and shortened agent guidance around authorized
  read/patch workflows with revision reuse.

- Versioned generated agent skills with the binary release and a content checksum;
  added read-only mismatch diagnostics, explicit repair guidance, and protection
  for local edits while retaining the legacy upgrade path.
- Added shared ordered edits, exact text replacement, and compact receipts/diffs
  to CLI, MCP, and the local UI bridge; retained default JSON contracts.
- Preserved unrelated Markdown/frontmatter bytes, fixed fenced and ambiguous
  section matching, and enabled plain reserved index/log reads and edits.
- Documented revision reuse, explicit conflict recovery, and the shortest
  authorized curator workflow with a reproducible editing payload comparison.

## 2026-07-27

- Clarified that public Factile paths use single forward-slash separators;
  repeated slashes and backslashes are rejected before source resolution.
- Made natural-language search discard conservative prose noise when useful
  terms remain and match plain words as complete Unicode tokens, while
  preserving literal identifiers, fallback-only queries, and the original
  query.
- Ranked multi-term search by distinct query coverage, capped per-field
  repetition after three occurrences, and added fixed description and body
  phrase evidence.
- Limited body ranking to the best heading section or headingless paragraph and
  derived each snippet from that passage while keeping Context documents whole.
- Added standard-input bodies for `create` and `write` through `--body -`,
  while retaining ordinary files and `./-` for a literal dash filename.
- Added one standard-input content operand per `patch` invocation, with
  complete argument validation before input consumption or document mutation.

## 2026-07-21

- Reopened the `factile init` delivery epic after adversarial review exposed
  gaps in workspace-boundary enforcement, plan freshness, generated-state
  ownership, managed-block structure, atomic publication, option parsing,
  terminal detection, and external-workspace handoff.
- Kept the accepted human-first contract intact, marked the affected hardening
  guarantees as pending, and recorded their exact failure and recovery
  semantics before implementation resumes.
- Completed and adversarially verified the boundary, plan-freshness,
  generated-ownership, marker-structure, per-file publication, option-parsing,
  terminal-detection, and external-handoff hardening; removed the temporary
  pending labels from the implemented contract.
- Aligned CLI help, the generated Factile skill, npm package onboarding, and
  public guides with one repeatable `factile init` workflow and advanced-only
  `skill install` reconfiguration.
- Corrected the release note: v0.4.0 already includes Root Layout v2, but
  predates the newer human-first init reconciler.

## 2026-07-20

- Implemented the human-first `factile init` reconciliation contract, including
  workspace and root resolution, interactive and non-interactive defaults,
  repeat repair, safe metadata updates, preserved authored knowledge and agent
  intent, and bounded in-process health checks.
- Simplified installed Codex guidance to one canonical skill plus a concise
  `AGENTS.md` router, retired the redundant discovery helper, and made discovery
  prefer one brief or exact path before narrowly scoped context.
- Made `skill doctor` verify generated skill content and reader/curator
  agreement across the skill, managed agent guidance, and MCP configuration.
- Reconciled every public CLI, generated-guidance, and command-reference claim
  with the implemented Root Layout v2 behavior. Removed pre-implementation
  target warnings while retaining the explicit v1 migration table and a single
  published-release caveat in installation guidance.

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

## 2026-09-11

- Embed the schema-aware shared reader and curator. Verify field diagnostics, honest scope and coverage, legacy responses, and operational failures through real no-Node loopback runtimes on desktop and mobile. Record UI source identity in the embedded asset manifest.

- Clarify that the Concept Schema Go integration and dependencies remain planned until their implementation tasks pass local verification. Hosted CI is deferred separately.

- Replace handwritten frontmatter parsing with explicit YAML 1.2 Core node projection and exact finite JSON numbers. Reject duplicate/non-string keys, cycles and unsupported tags; bound parsing and alias expansion. Preserve string types through serialization and unrelated metadata bytes through patches. Require a non-empty string type for concepts.

- Add the isolated Concept Schema v1 engine with strict definition stages, in-document references, collision handling, exact reports and safe field diagnostics. Bound candidate/input sizes, numeric arithmetic, reference depth, possible evaluation work and regex matching. Qualify the configured Go evaluator against 1,284 applicable official tests and exactly replay all 22 private contract cases. Workspace integration remains the next step.

- Apply concept schemas automatically in workspace validation, isolated by physical root and mount source including cached Git. Report base OKF separately, distinguish complete/scoped/skipped coverage, exclude base-invalid concepts, and deduplicate overlapping views. Preserve existing write and bootstrap health-check policy.

- Expose base/schema coverage and actionable field diagnostics in CLI text and JSON, with matching local MCP and reader/curator bridge results. Add seven JSON/text golden scenarios, operational-error checks and a public schema authoring example. Older base-only results remain explicitly unevaluated.

## 2026-09-11: Review and Freshness

Added explicit revision-checked Factile process review, optional review-state reads and search filters with exclusion reasons. Review history survives content changes. Evaluation preserves timezone offsets and fractional precision, with freshness due at the declared deadline. Human authentication remains a separate hosted action. Shared OKF review cases and local mutation/filter tests verify these semantics.
