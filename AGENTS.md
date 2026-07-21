# Documentation rules

All files under `docs` must follow Open Knowledge Format v0.1.

When creating or editing `docs/**/*.md`:

- Treat `docs` as one OKF bundle.
- Every non-reserved `.md` file must start with YAML frontmatter.
- Frontmatter must include a non-empty `type`.
- Prefer these fields: `title`, `description`, `tags`, `timestamp`.
- Use `docs/index.md` for navigation.
- Use `docs/log.md` for chronological documentation changes.
- Use bundle-relative links, for example `/architecture/auth.md`.
- Use Mermaid fenced code blocks for diagrams, for example ` ```mermaid `.
- Do not create plain Markdown files in `docs`.
- Do not create `README.md` under `docs`; use `index.md`.

Before creating a new `docs/**/*.md` file:

1. Confirm that the CLI repository owns the truth being documented.
2. Choose a short, descriptive non-empty `type`; OKF accepts domain-specific
   values and has no central type registry.
3. Place current architecture, guides, concepts, and reference material under
   the existing shallow structure.
4. Update `/docs/index.md` and `/docs/log.md` when navigation or durable current
   guidance changes.

# Factile Agent Instructions

Factile is a local-first open-source OKF tool with local directory and
read-only Git sources.

## Product rules

- Build one native Go binary named `factile`.
- Keep CLI and MCP as thin adapters over the core workspace API.
- Keep read-only Git acquisition local to the workspace. Do not add hosted `factile://` resolution, writable Git, auth products, subscriptions, billing, hosted MCP, marketplace search, publisher portals, publication, or cloud sync.
- Public knowledge operations use virtual Factile paths, for example `/product-docs/workflows/invoice-import`.
- Mount commands accept local source paths and native Git remote syntax; `git+` remains compatibility syntax rather than a requirement.
- Every explicit mount defaults to read-only. Only local mounts may opt into `--writable`; Git mounts are always read-only and the workspace root bundle remains writable in curator mode.
- Floating Git sources check at most once per 24 hours unless explicitly refreshed. Generated cache state stays under the workspace's `.factile/cache/` directory.
- JSON output is the stable agent contract.
- Text output is presentation only.
- Preserve human-readable Markdown.
- Do not rewrite unrelated Markdown sections during patch operations.
- Existing concept writes require `--rev`.
- Writes lock before reading mutable state, then check revision under the lock.
- Source capabilities are enforced in the workspace/write layer, not only in CLI or MCP.
- Use `path` as the public addressing term.
- Public concept paths omit `.md`, but CLI may accept and normalize `.md`.
- `rename` does not update links in Phase 1; it reports backlink warnings instead.
- Comments use simple present tense.

## Architecture

Use this package shape unless implementation proves a better local refactor is needed:

```text
cmd/factile/
internal/cli/
pkg/factile/
pkg/mcpserver/
pkg/vfs/
pkg/gitsource/
pkg/storage/
pkg/okf/
pkg/search/
pkg/contextpack/
pkg/graph/
pkg/patch/
pkg/profile/
pkg/revision/
pkg/skill/
pkg/trace/
pkg/uibridge/
```

## CLI contract

Implement the filesystem-like command style:

```text
factile <command> <path> [args/options]
```

Core commands:

```text
factile list <path>
factile stat <path>
factile read <document-path>
factile search <path> <query>
factile context <path> <query>
factile graph <path>
factile validate <path>
factile ui

factile mkdir <path> [--log] [--overview] [--bundle]
factile create <document-path> --type <type> --title <title> --body <file>
factile write <document-path> --rev <rev> --body <file>
factile patch <document-path> --rev <rev> [options]
factile rename <old-path> <new-path> --rev <rev>
factile delete <document-path> --rev <rev>
factile deprecate <document-path> --rev <rev> --reason <text>

factile mount <source> <mount-path> [--ref <ref> | --revision <40-hex-sha1>] [--writable]
factile refresh <mount-path>
factile unmount <mount-path>
factile mounts
factile view list|inspect|set|delete
factile bundle find [path]
factile bundle inspect <source>

factile mcp serve --stdio
```

## Verification

Every task ends by running:

```bash
gofmt -w .
go test ./...
go vet ./...
factile validate /
./scripts/verify.sh
```

If a command is not available yet, update `scripts/verify.sh` so it verifies the implemented slice without masking failures.

## Test strategy

Prefer self-verifying tests over manual inspection.

Each implementation slice includes:

- unit tests
- golden JSON output tests where applicable
- CLI smoke tests
- fixture OKF bundles under `testdata/bundles`
- negative tests for invalid paths, bad frontmatter, missing revisions, and revision mismatch
- temporary local Git remotes for Git behavior; tests never require live network, credentials, or SSH access

## Done means

A task is done only when:

1. tests exist for the new behavior
2. implementation passes all verification commands
3. CLI JSON output is stable
4. no hosted, writable-Git, background-sync, or publication behavior is implemented accidentally
5. docs or command help are updated when behavior changes

<!-- factile:codex:start -->
## Local knowledge

For architecture, design, domain, workflow, policy, documentation, review, or
implementation-choice tasks that need repository knowledge, use the installed
`factile` skill. It owns the discovery workflow and workspace model; do not
duplicate that guidance here.

Mode: reader. Do not mutate Factile manifests, views, mount descriptors, or OKF documents unless the user explicitly asks to curate knowledge; the configured MCP server must remain read-only.

Skip Factile for mechanical renames, formatting, syntax fixes, and obvious local edits.

If Factile is unavailable, continue with normal repository inspection.
<!-- factile:codex:end -->


<!-- BEGIN BEADS CODEX SETUP: generated by bd setup codex -->
## Beads Issue Tracker

Use Beads (`bd`) for durable task tracking in repositories that include it. Use the `beads` skill at `.agents/skills/beads/SKILL.md` (project install) or `~/.agents/skills/beads/SKILL.md` (global install) for Beads workflow guidance, then use the `bd` CLI for issue operations.

### Quick Reference

```bash
bd ready                # Find available work
bd show <id>            # View issue details
bd update <id> --claim  # Claim work
bd close <id>           # Complete work
bd prime                # Refresh Beads context
```

### Rules

- Use `bd` for all task tracking; do not create markdown TODO lists.
- Run `bd prime` when Beads context is missing or stale.
<!-- END BEADS CODEX SETUP -->

<!-- coding-practice:codex:start -->
## Shared coding practice

For non-trivial work where authority, process weight, durable coordination,
completion evidence, or a shared technical practice matters, use the
`$coding-practice` skill. It reads one exact concept for known tasks and uses
narrowly scoped context only when the category is unclear. That exact read
satisfies Factile retrieval for this bundle; do not add a second discovery or
context pass. Explicit requests, closer project instructions, and verified facts
take precedence. Keep trivial work direct and never use shared guidance to
broaden authority.
<!-- coding-practice:codex:end -->
