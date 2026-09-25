---
type: Reference
title: Root Layout v2 Command Reference
description: Accepted command syntax for the explicit Factile workspace and bundle model.
tags: [factile, cli, commands, reference]
generated:
  at: "2026-09-11T09:43:38.462944424Z"
  by: factile/v0.6.0
legacy_metadata:
  timestamp: "2026-07-27T00:00:00+02:00"
---

# Root Layout v2 Command Reference

The Root Layout v2 command shape is:

```text
factile [global options] (<command> [args] | <path>)
```

Run `factile help` (or `--help`) for the overview. Use `factile help patch`,
`factile patch help`, or `factile patch --help` for command options and examples.
Global options may appear before or after a command.

## Global options

| Option | Purpose |
|---|---|
| `--workspace <directory>` | Select one exact existing directory without searching upward. `init` may establish a workspace there; every other workspace-aware command requires an existing `[workspace]`. |
| `--json` | Emit stable structured results. |
| `--format text\|json` | Select output explicitly; JSON is the compatibility-equivalent of `--json`. |
| `--color auto\|always\|never` | Control human terminal styling. |
| `--quiet` | Suppress successful text output. |
| `--version` | Print build version information. |
| `--help` | Print the full command overview. |

## Bootstrap and summary

```text
factile init
  [--root <directory>]
  [--name <name>]
  [--title <title>]
  [--description <text>]
  [--agent auto|codex|none]
  [--yes]
factile
factile status
factile version
factile <path>
```

`init` is both repository onboarding and the repeat repair command. It uses the
nearest ancestor workspace or, when none exists, the current directory. For
`init` only, global `--workspace <directory>` selects the exact existing or new
boundary. `--root` selects a directory inside that workspace; it defaults to
the existing workspace root, an existing bundle at `.`, or `docs`, in that
order. Use `--workspace . --root .` for one combined workspace and bundle
manifest in the current directory. The selected workspace directory must
already exist.

`--name` sets stable bundle identity. `--title` and `--description` set
human-facing metadata. New bundles derive omitted values from the workspace
directory; existing bundles preserve omitted values. Existing `index.md` and
`overview.md` are authored content and are never overwritten.

`--agent auto` upgrades a managed repo integration or detects Codex from
`.codex/`, `.agents/skills/`, or `AGENTS.md`; otherwise it skips installation.
`codex` requests repo guidance, while `none` skips reconciliation without
uninstalling anything. Repeat runs preserve an installed reader/curator mode
and optional profile, and never modify user scope.

In an interactive terminal, a new workspace asks for unresolved root, title,
and description values, shows the complete plan, and asks for confirmation.
Existing workspaces skip those setup questions; an explicit root change has a
separate default-no confirmation and leaves the previous root untouched. Use
`--yes`, JSON output, or explicit values for non-interactive operation:

```text
factile init --workspace . --root docs --name project-docs \
  --title "Project Docs" \
  --description "Documentation and knowledge for this project." \
  --agent none --yes --json
```

Prompting requires both standard input and output to be actual terminals.
Pipes, regular files, and character devices such as `/dev/null` use defaults.
A value-taking option followed by another recognized option is a missing-value
error, not option data. Global JSON selection applies to syntax errors whether
it appears before or after `init`; these errors occur before planning or writes.

The result includes `workspace_path`, `root_bundle_path`, `agent_selection`,
resolved `bundle` metadata, file actions, agent actions, and ordered `health`
checks for workspace layout, bundle metadata, required documents, local root
validation, and agent integration. Warnings return `0`; failed health returns
`3` after emitting the complete result. Init never refreshes mounted remotes or
accesses hosted services or credentials.

Before writing, init validates the complete layout, output types, and generated
ownership. It refuses unrecognized canonical skills and malformed managed
markers, with no force override. Each file is published atomically, but there
is no transaction across files; rerunning after interruption converges without
overwriting authored Markdown. Next commands are executable from the original
caller directory and include a shell-safe explicit `--workspace` selection when
ordinary discovery would choose another boundary.

A lone `/path` reads a document first and lists only when no document exists at
that path.

All commands below are contextual and require a workspace except the two
explicitly physical bundle commands. Missing context returns
`no_active_workspace`.

## Reader commands

```text
factile list     [path] [--brief] [--view <id>]
factile stat     <path>
factile read     <document-path>
factile search   <path> <query> [--view <id>]
factile context  <path> <query> [--max-tokens <n>] [--depth 0|1] [--view <id>]
factile graph    <path> [--depth 0|1] [--view <id>]
factile validate <path> [--view <id>]
factile ui       [--port <port>] [--no-open] [--dev-assets <url>] [--curator]
```

`validate` automatically checks base OKF and optional schemas owned by each
physical bundle. JSON separates `okf` and `concept_schemas` and adds
`schema_diagnostics` for fields that need attention. A base or profile failure
returns exit status `3`; an operational resource limit aborts without a report.
Unknown types remain unprofiled, and writes do not enforce profiles. See
[Validate concept schemas](../guides/concept-schemas.md) for a complete example.

`ui` serves the embedded browser on loopback. Reader mode is the default;
`--curator` enables local write routes. `--dev-assets` loads browser assets from
the given development server while keeping the local workspace API.

## Mount and view commands

```text
factile mount <source> <mount-path>
  [--ref <ref> | --revision <40-hex-sha1>]
  [--writable] [--read-only]
  [--title <title>] [--description <text>]

factile refresh <mount-path>
factile unmount <mount-path>
factile mounts

factile view list
factile view inspect <id>
factile view set <id> --title <title> --path <path>
  [--description <text>]
factile view delete <id>
```

Repeat `--path` on `view set` to select more than one scope. Explicit mounts
default read-only; only a local source can use `--writable`. `--read-only` is a
deprecated compatibility flag.

## Directory and document writes

```text
factile mkdir <path> [--title <title>] [--log] [--overview] [--bundle]

factile create <document-path>
  --type <type> --title <title> --body <file|->

factile write <document-path> --rev <rev> --body <file|->

factile patch <document-path> --rev <rev> [patch options]

factile rename <old-path> <new-path> --rev <rev>
factile delete <document-path> --rev <rev>
factile deprecate <document-path> --rev <rev> --reason <text>
```

For `create` and `write`, a body operand of exactly `-` reads the complete
Markdown body from standard input. Use `./-` to read a literal file named `-`.
Ordinary file operands retain their existing behavior.

Patch options are:

```text
--replace-text <old> <new>
--set <key=value>
--delete-key <key>
--replace-section <heading> <file|->
--append-section <heading> <file|->
--replace-body <file|->
--input <file|->
--brief
--diff
```

Edit flags run in order and may be repeated. Exact text replacements require
one match in the Markdown body. Across all patch content options, at most one
operand may be exactly `-`; it reads standard input. Factile rejects a second
`-` before reading standard input or changing the document. Ordinary files may
be repeated, and `./-` addresses a literal file named `-`. All
existing-document writes require the current document revision.

`--input` accepts one JSON patch object and cannot be combined with edit flags.
Use `operations` for ordered `replace_text`, `replace_section`, `append_section`,
`replace_body`, `set`, and `delete_key` edits. It accepts `expected_revision`,
`brief`, `diff`, and the legacy patch fields too. All edits to a document save
atomically. Section operations ignore fenced code and reject duplicate headings.

`--brief --json` returns a compact receipt with `path`, `revision`, `changed`,
`summary`, and `validation`. `--diff` adds a unified diff. Default JSON remains
`{"concept": ...}`; requesting only a diff adds a `receipt` beside it. Validation
scope is document frontmatter, excluding Markdown syntax and bundle/link checks.
No-op edits retain their revision. Conflicts include expected/current revisions;
read and reconcile before retrying. Plain or typed `/index` and `/log` support
read/write/patch with the same source and revision checks.

See [Editing documents](../guides/editing-documents.md) for inline JSON examples.


## Bundle inspection

```text
factile bundle find [path]
factile bundle inspect <directory>
```

`bundle find` searches the named physical directory for valid bundle manifests;
`bundle inspect` validates one physical bundle directory. They require
`[bundle]`, need no workspace or logical `/`, create no `.factile/` state, and
do not publish or install remote bundles.

## Skills and MCP

```text
factile skill list
factile skill inspect codex
factile skill install codex --scope repo|user
  [--mode reader|curator] [--profile software]
factile skill uninstall codex --scope repo|user
factile skill doctor codex

factile mcp serve --stdio [--read-only]
```

Repo-scope install manages one generated skill, a concise `AGENTS.md` block,
and a mode-matched MCP block. `skill doctor` rejects generated-content drift
and reader/curator mismatches before probing local list and context commands.
Normal repo onboarding and repair belongs to `init`; use `skill install` for
intentional scope, mode, or profile reconfiguration and `skill doctor` for
focused diagnostics.

## Exit codes

| Code | Class |
|---:|---|
| `0` | success |
| `1` | general failure or an unsuccessful doctor check |
| `2` | invalid path syntax, unsupported command, or command usage |
| `3` | validation or OKF parsing failure, including failed post-init health |
| `4` | missing workspace, invalid bundle context, mount, path, concept, or wrong path kind |
| `5` | existing destination, missing/stale revision, or missing/ambiguous patch match |
| `6` | read-only, unsafe, unsupported, or unavailable source/revision |
| `7` | partial failure |
| `8` | lock timeout |

Use JSON error codes rather than parsing human messages.

## V1 migration

| Legacy v0.3 input | Root Layout v2 |
|---|---|
| `.factile/config.toml` | Bundle `[bundle]` metadata in `factile.toml`, plus an enclosing workspace manifest. |
| `.factile/views.toml` | Workspace-level `factile.views.toml`. |
| Global `--root <path>` on contextual commands | `--workspace <directory>`. Init's current `--root <directory>` instead selects its root bundle. |
| `--mount-file <path>` | Spatial `<name>.mount.toml` descriptors in the root bundle. |
| `no_active_root` | `no_active_workspace`. |

Outside `init`, the retired global root option and `--mount-file` may produce
targeted migration diagnostics, but they do not activate compatibility
behavior in v2.

## OKF v0.2 Authoring

`create` and meaningful concept edits record the actual Factile producer and UTC change time in `generated.by` and `generated.at`. Existing verification history and unrelated metadata are preserved. No-op edits retain the revision; review, lifecycle, or freshness-only changes do not restamp generation. `deprecate` sets `status: deprecated`. Valid lifecycle values are `draft`, `stable`, and `deprecated`; omission means stable.

`mkdir` creates plain directory indexes and date-headed logs. `--bundle` is the `--log --overview` shortcut inside the owning bundle. `init` creates a true bundle-root index containing only `okf_version: "0.2"`, and preserves valid existing root indexes. `overview.md` remains a concept with producer metadata.

## Explicit Bundle Migration

`factile migrate <physical-bundle-directory> [--apply] [--json]` previews the local OKF v0.2 conversion by default. Apply only after reviewing its revisions, diffs, findings, and native validation. Ambiguities and errors block writes; historical producers and review events are never invented. See [Migrating a Local Bundle](../guides/migrating-okf.md) for conversion rules and file atomicity.

## Sources and Claim References

`read` and `context` preserve authored `sources` and expose `source_references`, `claim_references`, and advisory `metadata_diagnostics`. Source entries retain unknown fields, author, modification time, usage count, and the declared source or document usage window. These observations are separate from billing and review.

Internal source paths resolve inside the owning physical bundle. Mounted targets use the corresponding logical mount prefix; views omit derived targets outside their selection. Graphs and rename backlink warnings include `source_reference` relationships. External resources are identified without fetching them, and scope descriptors remain non-dereferenceable evidence. Exact footnote labels join to unique source IDs; code, escapes, and definitions do not invent claims.

## Review and Freshness

Read optional derived state with `factile read /path --include-review --evaluated-at 2026-09-11T08:00:00Z`. The evaluation time defaults to the current UTC instant. `review_state` reports authored review claims, lifecycle, the latest review time, changed-since-review, and whether the freshness deadline is due. It preserves authored timezone and fractional precision. A missing deadline or unavailable change/review time remains unknown.

`factile review /path --rev <observed-revision>` appends an explicit `process:factile/<version>` event at the current time with the observed revision. It keeps earlier history, content and generation metadata unchanged. Local review does not assert an authenticated human identity; arbitrary actor strings can still be imported as portable metadata claims. MCP exposes the same revision-checked `factile_review` operation only in writable mode.

Search accepts `--include-review`, `--evaluated-at`, `--status draft|stable|deprecated`, `--review-tier unverified|machine-confirmed|human-reviewed`, `--stale true|false`, and `--changed-since-review true|false`. Filters apply to query matches and are opt-in. Unknown values match neither boolean choice. `selection` records the evaluation time, applied filters, and excluded matching paths with reasons. Direct reads and unfiltered searches retain historical material. Domain fields such as `workflow_status` remain separate from OKF lifecycle.

## Complete Context Evidence

Context includes the complete selected concept metadata, sources and claim references, revision, source origin, and review/lifecycle/freshness state beside its Markdown. One `evaluated_at` applies to the whole pack; callers may supply an explicit timezone-aware datetime. Local mounted concepts identify their owning bundle and selected remote ref/revision where available. Hosted concepts identify the selected source URI, ref and immutable artifact revision, and include matching authenticated review actions.

The `utf8_json_bytes/4` estimator counts the UTF-8 bytes of compact JSON `[concept, summary]`, rounded up to one token per four bytes, for every included concept. It includes body, complete metadata, source evidence and derived qualifications. Request/selection bookkeeping and omission records are outside this content budget. A concept that cannot fit is omitted whole with `reason: token_budget` and `estimated_tokens`; evidence is never silently truncated. The budget reports requested maximum and used estimate. Related inclusion is limited to one hop from original query matches, within the selected path, physical source and available view/access scope; external resources are not fetched.
Text and browser Copy context exports put each concept's metadata in a JSON block followed by its original body in a separate fenced Markdown block. Fences exceed authored backtick runs. This keeps identical footnote labels local to their original document and preserves source IDs without rewriting claims. Selection, source identity, evaluation time, budget and omissions accompany the selected documents. Browser JSON export retains the original context result, request, source binding and explicit selected paths.

`factile context / query --evaluated-at 2026-09-11T08:00:00Z --max-tokens 4000` exposes the same selection and evidence through text, JSON, local MCP and the local browser bridge.

## Browser Evidence Curation

`factile ui --curator` enables the reusable Sources and review editor and `/writer/review` route. It patches only changed evidence fields under the observed document revision. Source extensions, body and unrelated metadata survive evidence edits. Review appends the actual local process and time; read-only mode rejects the route and arbitrary actor/time input is not accepted. Read/search bridge calls forward optional native review state and filters. The browser retains rejected drafts and requires an explicit reload to discard them.
