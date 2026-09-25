---
type: Guide
title: Validate concept schemas
generated:
  at: "2026-09-11T09:01:41.962451643Z"
  by: factile/v0.6.0
---

# Validate concept schemas

Concept schemas add optional rules to concept frontmatter. `factile validate`
checks base OKF and any matching schemas automatically. Each physical bundle
owns its own schemas; a workspace root's schemas do not apply to mounts.

## Add a profile

In the physical folder of your bundle, create
`concept-schemas/note.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "x-factile-concept-schema": {
    "format": "concept-schema/v1",
    "id": "example.note",
    "concept_type": "Note"
  },
  "type": "object",
  "required": ["type", "owner", "state"],
  "properties": {
    "type": {"const": "Note"},
    "owner": {"type": "string", "minLength": 1},
    "state": {"enum": ["draft", "ready"]}
  },
  "additionalProperties": true
}
```

Create `bad.md` in that bundle:

```markdown
---
type: Note
title: A note to fix
state: unknown
---

This is readable, base-valid OKF with incomplete profile metadata.
```

For the root bundle, run:

```bash
factile validate /bad
factile validate /bad --json
```

Both return exit status `3`. Text identifies the missing `owner` and the invalid
`state`. JSON keeps `okf.valid: true`, while the matching profile has
`conformant: false`. The concept remains readable and editable.

Use `factile read /bad --json` to obtain its revision, then fill in the fields
using your editor or a revision-checked patch:

```bash
factile patch /bad --rev <revision> --set owner=docs --set state=ready
factile validate /bad
```

After those edits validation succeeds. For a mounted bundle, use its virtual
mount path, such as `/notes/bad`, in the commands.

## Read the result

JSON retains top-level `path`, `valid`, and `issues`. `okf` reports base validity.
`concept_schemas` contains one entry per selected bundle, with `bundle_path`,
`scope_paths`, `complete_bundle`, and its `result`.

- A full base-valid bundle's result includes `contract: concept-schema/v1` and
  the exact portable `conformant`, `schemas`, `evaluated_concepts`, and `issues`.
- A narrow document, directory or view omits the portable contract marker.
- Base-invalid concepts are excluded and counted in `skipped_concepts`; their
  bundle result also omits the portable contract marker.
- An unavailable source has `skipped_reason` and no schema result.
- No definitions and no matching types are distinct from evaluated success.
  Unknown or mistyped types remain unprofiled under v1.
- `schema_diagnostics` adds safe field-level guidance outside the exact portable
  report. Each entry includes the logical path, bundle path, schema id, concept
  type, JSON Pointer `field`, keyword and a readable message.

A resource limit aborts the command with an operational error, without a
successful report. An older source that omits schema results provides no schema
conformance evidence. CLI, local MCP, and the loopback reader/curator bridge use
the same workspace validation result.

The embedded reader and curator show base OKF separately, profile coverage and
field diagnostics with document links. `scripts/smoke-schema-ui.sh` checks those
flows against real local bridges on desktop and mobile. Embedded asset metadata
in `factile-ui-source.json` identifies the UI revision and worktree status.

## Scope and limits

Only direct regular `concept-schemas/*.schema.json` files are discovered. Nested
schema directories and symlinks are ignored. Each exact type and schema id may
have one definition per bundle; duplicates disable all conflicting definitions.

Frontmatter uses portable YAML values. Explicit Core tags must match their value
and node kind; for example, `!!int nope` is a base parsing error. Quoted scalars
remain strings, and `!!str` preserves plain text. A closing frontmatter delimiter
may end the file without a trailing newline. Parser limits return errors safely.

Schemas use Draft 2020-12 and in-document `#/...` references. Remote references,
cross-file references, identifiers, anchors and dynamic references are rejected.
Referenced targets receive schema checks even inside an unknown-keyword or
annotation location. Untargeted annotation data remains literal.

Only frontmatter is validated. There is no Markdown-body schema, type registry,
coercion, default insertion, generated form or schema authoring interface.
`format` remains annotation-only. Writes and bootstrap health checks retain
their existing base OKF rules; these profiles add no write or publishing gate.

See [CLI architecture](../architecture/overview.md) for exact parser/evaluator versions,
operational limits and the reproducible upstream qualification command. A runnable
example is included in the checkout at `testdata/bundles/schema-profiles`:

```bash
factile --workspace testdata/bundles/schema-profiles validate / --json
```

Its deliberately invalid note makes that command return `3`.
