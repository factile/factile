---
type: Guide
title: Editing Documents Safely
description: Create and change OKF documents with optimistic revisions and targeted patches.
tags: [factile, cli, writing, revisions, patch]
timestamp: 2026-09-10T00:00:00+02:00
---

# Editing Documents Safely

Document mutation is explicit. Existing-document operations require the
revision observed by the caller so a concurrent change is not overwritten.

## Create

Prepare a Markdown body file, then create a concept:

```bash
factile create /runbooks/cache-recovery \
  --type Runbook \
  --title "Cache Recovery" \
  --body ./cache-recovery.md
```

To pipe the complete Markdown body instead, use `-`:

```bash
printf '# Cache Recovery\n\nFollow the recovery steps.\n' |
  factile create /runbooks/cache-recovery \
    --type Runbook \
    --title "Cache Recovery" \
    --body -
```

`type` must be non-empty, but Factile does not require a central type registry.
Choose a stable value that communicates the document's role.

## Read before updating

Use JSON to capture the exact current revision:

```bash
factile read /runbooks/cache-recovery --json
```

Pass the returned `concept.revision` to the next mutation. If it no longer
matches, Factile returns `revision_mismatch`; read again and reconcile instead
of retrying blindly.

## Replace the body

`write` replaces Markdown body content while preserving frontmatter:

```bash
factile write /runbooks/cache-recovery \
  --rev sha256:<current-revision> \
  --body ./cache-recovery.md
```

`write --body -` reads its complete replacement body from standard input too.
Use `./-` when the intended input is a literal file named `-`. Empty standard
input behaves like an empty body file.

## Make a precise edit

For an authorized edit at a known path, read it directly and reuse that revision.
There is no need to repeat list, stat, or context discovery.

```bash
factile read /runbooks/cache-recovery --json
factile patch /runbooks/cache-recovery \
  --rev sha256:<revision-from-read> \
  --replace-text 'Restart the cache.' 'Restart the cache, then check its health.' \
  --brief --json
```

`--replace-text` matches the exact Markdown body text once, including line
endings. Empty replacement text deletes the match. Missing text returns
`text_not_found`; multiple matches return `text_ambiguous`. Include surrounding
text to select a duplicate sentence, link, bullet, or table cell precisely.
Metadata edits use `--set key=value` or `--delete-key key`.

For multiple or multiline edits, send one JSON object through standard input:

```bash
factile patch /runbooks/cache-recovery \
  --rev sha256:<revision-from-read> --input - --brief --json <<'JSON'
{
  "operations": [
    {"op": "replace_text", "old": "Restart the cache.", "new": "Restart the cache.\nCheck its health."},
    {"op": "set", "key": "status", "value": "active"},
    {"op": "append_section", "heading": "Notes", "markdown": "Record the recovery time."}
  ]
}
JSON
```

Operations run in order against the preceding result and save once. A failure
leaves the document unchanged. Use `--input <file>` for a saved JSON request.
The JSON object accepts `expected_revision`, `operations`, `brief`, and `diff`,
as well as the existing `set`, `delete_keys`, `replace_sections`,
`append_sections`, and `replace_body` fields. Legacy fields run first in that
order; section maps use sorted heading order. Prefer `operations` for repeated
or interdependent edits. Do not combine `--input` with edit flags. A supplied
`--rev` must agree with an `expected_revision` in the JSON object.

| Option | Effect |
|---|---|
| `--replace-text <old> <new>` | Replace exactly one body-text match. |
| `--set key=value` | Set one parsed frontmatter value. |
| `--delete-key key` | Remove one frontmatter key. |
| `--replace-section "Heading" <file|->` | Replace a section body, including its nested sections. |
| `--append-section "Heading" <file|->` | Append to a section, or create it if missing. |
| `--replace-body <file|->` | Replace the complete body. |
| `--input <file|->` | Read one JSON patch object. |
| `--brief` | Return a compact receipt instead of the complete document. |
| `--diff` | Include a unified diff in the receipt. |

Edit flags run in command-line order; repeated operations are retained. At most
one content operand may use `-`. Use JSON input for several inline contents;
use `./-` for a literal file named `-`.

Section names match case-insensitively and ignore headings inside fenced code.
Duplicate headings return `section_ambiguous`; a missing replacement target
returns `section_not_found`. Use exact text with context to edit one duplicate.
Body edits preserve frontmatter bytes. Metadata edits preserve other entries
and body bytes, including comments, quoting, lists, and line endings. New
section content uses the document's line ending; exact text and whole-body
replacements use the supplied bytes.

## Read the receipt

`--brief --json` returns `path`, `revision`, `changed`, `summary`, and
`validation`. `summary` lists the operations applied, including any that leave
bytes unchanged. No-op patches report `changed: false` and retain the revision.
`--diff` adds a unified `diff` string; with default output the receipt accompanies
the existing `concept` result. Without either option the JSON shape is unchanged.

Validation scope is `document_frontmatter`: parsing and the required concept
`type`. It does not validate Markdown syntax, links, or the whole bundle. Use
`factile validate <affected-path>` when those broader bundle checks are needed.
Reuse the successful receipt's revision for a subsequent edit. Revision
conflicts report both expected and current revisions; read and reconcile the
content before using the current revision.

## Edit navigation and logs

Read, patch, and write support `/index` and `/log`, including nested reserved
documents, with or without frontmatter. They use the same revision and source
checks as concepts. Ordinary concepts still require frontmatter with a non-empty
`type`. Edit a concept, its navigation entry, and its log as separate document
writes, each using its own observed revision.

## Rename, deprecate, or delete

```bash
factile rename /runbooks/cache-recovery /runbooks/cache-repair \
  --rev sha256:<current-revision>

factile deprecate /runbooks/cache-repair \
  --rev sha256:<current-revision> \
  --reason "Use /runbooks/storage-recovery."

factile delete /runbooks/cache-repair \
  --rev sha256:<current-revision>
```

Each successful content change returns a new revision; reuse the returned one.
Rename reports backlink warnings but does not rewrite links. Prefer deprecation
when readers still need a transition path, and use delete only when removal is
intentional.

## Safety boundary

Writes are allowed only in the workspace's root bundle or an explicitly writable local
mount. Git and ordinary explicit mounts return `source_read_only`. The
workspace keeps locks beneath its ignored `.factile/` state directory, locks
before its final read and revision check, then
validates before returning the saved result.

Use broader validation after edits that affect links or bundle structure:

```bash
factile validate /runbooks
```

## Reproduce the payload comparison

Build the local binary and run `python3 scripts/compare-editing.py /path/to/factile`
from this checkout. It checks sentence, link, metadata, multi-section, and
concept-plus-index/log edits on temporary fixtures with 400 unchanged reference
lines. All saved bytes must match the expected edit exactly.

The 2026-09-10 run measured these total UTF-8 response bytes, including the read:

| Edit | Direct file read/edit | CLI with brief receipt | CLI with full result |
|---|---:|---:|---:|
| Sentence | 12,274 | 13,171 | 25,809 |
| Link | 12,274 | 13,171 | 25,810 |
| Metadata | 12,274 | 13,162 | 25,811 |
| Two sections | 12,274 | 13,191 | 25,810 |
| Concept, index, and log | 12,334 | 14,167 | 26,765 |

Each path uses one logical read and one edit: two requests for single-document
cases and six for concept/index/log. All cases have zero retries and zero
unrelated changes. Request bytes range from 79–109 for direct single-document
edits and 235–306 for the CLI brief requests; the three-document case uses 288
and 783 respectively. The script emits every input/output byte count as JSON.

The direct baseline models raw file reads and an exact replacement request with
an `ok` acknowledgement. CLI numbers are actual process responses; workspace
selection overhead is excluded. This measures payloads and correctness, not
wall-clock speed, model tokens, or agent preference. Compact receipts roughly
halve the CLI read/edit response payload here. Direct editing remains smaller;
Factile adds revisions, source checks, atomic edits, and document validation.
