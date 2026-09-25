---
type: Guide
title: Migrate a Local Bundle to OKF v0.2
generated:
  at: "2026-09-11T08:34:43.120788542Z"
  by: factile/v0.6.0
---

# Migrate a Local Bundle to OKF v0.2

Use the explicit migration command for an authorized local bundle conversion:

```bash
factile migrate /absolute/path/to/bundle --json > migration-preview.json
factile migrate /absolute/path/to/bundle --apply --json > migration-result.json
```

The operand is the physical bundle directory, not a logical mount path or the
workspace containing it. Select each independent bundle separately. All Markdown
inside that directory is considered; mounts, symlinks, `.git`, and `.factile`
content are not followed. Normal reading, writing, initialization, and ingestion
never migrate content automatically.

Preview is the default. Inspect the per-file before/after revisions, diff,
findings, and normal v0.2 validation result. `--apply` recomputes the candidate,
validates it in an isolated workspace, locks the observed files, checks their
bytes and document inventory again, and atomically replaces changed files.
Blocking findings or validation errors cause exit 3 and no writes. Each file
replacement is atomic; an I/O failure across multiple files is reported with
which replacements completed. Re-run preview against the resulting state.
Already converted content produces no further changes.

## Conversion Decisions

- A valid old timestamp fills `generated.at` only when a producer is already
  explicitly recorded and the generation datetime is absent. An identical
  existing datetime needs no duplicate. Otherwise the original value is retained
  in `legacy_metadata.timestamp` with a historical-information finding. Migration
  never attributes past content to itself or invents a producer or reviewer.
- Existing `generated` extensions and all `verified` history remain unchanged.
- An unambiguous deprecation flag becomes `status: deprecated`. Conflicting
  lifecycle fields or custom status values block application. Resolve their
  meaning explicitly; preserve a separate workflow value under an extension
  field when appropriate before choosing `draft`, `stable`, or `deprecated`.
- Explicit links and bare HTTP URLs in a body citation list become `sources`
  entries. The heading becomes `Sources`, retaining its prose and links.
  Ambiguous entries block application. Source authors, source IDs, and claim
  attribution are never inferred. Fenced examples are unchanged.
- Root index declarations become `okf_version: "0.2"`; valid plain root indexes
  remain plain. Other reserved-file frontmatter is removed and preserved as
  inert Markdown under `Previous navigation metadata`. Ordinary directory
  indexes remain plain. Unknown declared versions require explicit resolution.

The command preserves unrelated files, concept metadata, Markdown, and source
resources. The normal validator reports unresolved links and malformed optional
metadata; its warnings remain visible in the migration result. Review these
findings before treating the converted bundle as ready for readers.
