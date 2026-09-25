---
name: factile
description: Find and use repository knowledge through Factile. Use for architecture, design, and implementation decisions that need project context, and to create or edit Factile/OKF documents. Skip mechanical code edits that need no knowledge context.
metadata:
  version: {{VERSION}}
  factile-content-sha256: {{CONTENT_SHA256}}
---

# Factile local knowledge workflow

Use `--json` for agent results and virtual paths such as `/guides/setup`, without
`.md`. Use `factile <command> --help` for options.

## Read what you need

- If the workspace is unknown, run `factile status --json`. Use
  `--workspace <directory>` to select one explicitly.
- For a known path, call `factile read <path> --json` directly.
- Otherwise, use `factile list <scope> --brief --json` or
  `factile search <scope> '<query>' --json` to find it. Use
  `factile context <scope> '<task>' --json` when the task needs several documents;
  narrow with `--view <id>` when useful. These are alternatives, not a checklist.
- Apply relevant knowledge with current project facts and cite the paths used.

## Edit when requested

Prefer Factile CLI or MCP mutation commands for authorized document changes.
They preserve unrelated content and enforce source permissions and revisions.

Read the target once, then use its `concept.revision`:

```bash
factile read /guide --json
factile patch /guide --rev <observed-revision> --replace-text 'old' 'new' --brief --json
```

Batch related edits to that document in one ordered, atomic patch:

```bash
factile patch /guide --rev <observed-revision> --input - --brief --json <<'JSON'
{"operations":[{"op":"replace_text","old":"old","new":"new"},{"op":"set","key":"status","value":"stable"}]}
JSON
```

- Exact text must match once. Include more context for duplicate matches.
  Use `--set key=value` / `--delete-key key` for metadata; use section operations
  when changing a whole section. `--diff` returns the saved diff when needed.
- Use `factile create <path> --type <type> --title <title> --body -` for new documents and
  `factile write <path> --rev <rev> --body -` for whole-body replacement. Use `rename`,
  `deprecate`, or `delete` for those lifecycle changes.
- Reuse the successful mutation's revision. On a conflict, read and reconcile;
  do not bypass a rejected edit with a direct file write or a blindly refreshed revision.
- Content edits record the Factile tool in `generated` and preserve `verified`;
  reviewing content is a separate explicit action. Lifecycle is `draft`, `stable`,
  or `deprecated`. Store evidence in `sources` and cite stable source IDs with footnotes.
- `/index` and `/log` support the same revision-fenced workflow. Only a physical
  bundle-root index may have frontmatter, containing `okf_version: "0.2"` alone.
  Batches cover one document; each document needs its own observed revision.
- Brief receipts validate document frontmatter only. Run `factile validate <scope>`
  for broader bundle/link checks when relevant.
- MCP `factile_patch` accepts the same `operations`, `expected_revision`, `brief`,
  and `diff` fields. Use the available authorized transport.

## Workspace boundaries

The nearest `factile.toml` with `[workspace]` selects the root bundle and the
same logical `/` throughout the workspace. Other bundles are visible only when
mounted. `<name>.mount.toml` descriptors live in the root bundle; views live in
`factile.views.toml`. `.factile/` holds generated state, not authored knowledge.

The root bundle is writable; explicit local mounts are read-only unless opted
into writes, and Git sources are always read-only. Respect the installed mode
and project instructions. Change mounts, views, or setup only when requested;
`factile init` creates or repairs setup. If retrieval is unavailable, continue
with repository inspection and report the limitation.
