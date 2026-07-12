# Documentation rules

All files under `/docs` must follow Open Knowledge Format v0.1.

When creating or editing `/docs/**/*.md`:

- Treat `/docs` as one OKF bundle.
- Every non-reserved `.md` file must start with YAML frontmatter.
- Frontmatter must include a non-empty `type`.
- Prefer these fields: `title`, `description`, `tags`, `timestamp`.
- Use `/docs/index.md` for navigation.
- Use `/docs/log.md` for chronological documentation changes.
- Use bundle-relative links, for example `/architecture/auth.md`.
- Use Mermaid fenced code blocks for diagrams, for example ` ```mermaid `.
- Do not create plain Markdown files in `/docs`.
- Do not create `README.md` under `/docs`; use `index.md`.

Before creating a new `/docs/**/*.md` file:

1. Read `/docs/meta/document-types.md`.
2. Reuse an existing `type` if one fits.
3. If no type fits, add a new row to `/docs/meta/document-types.md` first.
4. Use the exact `Type` value from the registry in the document frontmatter.
5. Do not invent unregistered `type` values.

# Factile Agent Instructions

Factile Phase 1 is a local-only open-source OKF tool.

## Product rules

- Build one native Go binary named `factile`.
- Keep CLI and MCP as thin adapters over the core workspace API.
- Do not implement remote bundles, auth, subscriptions, billing, hosted MCP, marketplace search, publisher portals, or cloud sync in Phase 1.
- Public knowledge operations use virtual Factile paths, for example `/product-docs/workflows/invoice-import`.
- Bundle management commands may accept local source paths.
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
pkg/storage/
pkg/okf/
pkg/search/
pkg/contextpack/
pkg/graph/
pkg/patch/
pkg/revision/
```

## CLI contract

Implement the filesystem-like command style:

```text
factile <command> <path> [args/options]
```

Core commands:

```text
factile list <path>
factile read <document-path>
factile search <path> <query>
factile context <path> <query>
factile graph <path>
factile validate <path>

factile create <document-path> --type <type> --title <title> --body <file>
factile write <document-path> --rev <rev> --body <file>
factile patch <document-path> --rev <rev> [options]
factile rename <old-path> <new-path> --rev <rev>
factile delete <document-path> --rev <rev>
factile deprecate <document-path> --rev <rev> --reason <text>

factile mount <source> <mount-path>
factile unmount <mount-path>
factile mounts
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

## Done means

A task is done only when:

1. tests exist for the new behavior
2. implementation passes all verification commands
3. CLI JSON output is stable
4. no Phase 2 remote behavior is implemented accidentally
5. docs or command help are updated when behavior changes

<!-- factile:codex:start -->
## Local knowledge

This repository may have local Factile knowledge available by path.

Reader commands work by path. A path may be backed by root-local Markdown files or mounted sources; do not classify it before navigating it.

A Factile root is marked by `.factile/config.toml`. Mount descriptors are `<name>.mount.toml` files in the physical parent directory. Views live in `.factile/views.toml`.

Use `--view <id>` on reader commands when a named view matches the task; views narrow scope without changing document paths.

For tasks involving architecture, design, implementation choices, domain concepts, runbooks, standards, policies, documentation, reviews, or decisions:

1. Discover available knowledge with `factile list / --json`.
2. Inspect compact cards with `factile list / --brief --json` or `factile stat <path> --json`.
3. If a named view appears relevant, inspect it with `factile view inspect <id> --json` and pass `--view <id>` to scope-scanning reader commands.
4. Get focused task context with `factile context / '<task summary>' --json`, adding `--view <id>` when using a view.
5. Read specific concepts with `factile read <path> --json` only when more detail is needed.
6. Prefer Factile context over ad-hoc guessing when project knowledge is relevant.

Mode: reader. Do not edit Factile/OKF knowledge or catalog state unless the user explicitly asks to curate knowledge.

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

For non-trivial software design, planning, review, diagnosis, implementation,
verification, or delivery work, use the `$coding-practice` skill. Prefer the
read-only `/coding` Factile path when available; otherwise use its canonical
root fallback. Explicit user requests, closer project instructions, and
verified repository facts take precedence. Keep trivial mechanical work direct.
Never use shared guidance to broaden authority or write to the shared source.
<!-- coding-practice:codex:end -->
