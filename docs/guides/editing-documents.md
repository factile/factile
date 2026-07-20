---
type: Guide
title: Editing Documents Safely
description: Create and change OKF documents with optimistic revisions and targeted patches.
tags: [factile, cli, writing, revisions, patch]
timestamp: 2026-07-15T00:00:00+02:00
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

## Patch selected fields or sections

```bash
factile patch /runbooks/cache-recovery \
  --rev sha256:<current-revision> \
  --set status=active \
  --set title="Cache recovery"
```

Available patch operations can be repeated:

| Option | Effect |
|---|---|
| `--set key=value` | Set one parsed frontmatter value. |
| `--delete-key key` | Remove one frontmatter key. |
| `--replace-section "Heading" file.md` | Replace one existing Markdown section. |
| `--append-section "Heading" file.md` | Append content to one existing section. |
| `--replace-body file.md` | Replace the complete body through the patch operation. |

Patch preserves unrelated frontmatter and sections. A missing requested section
returns `section_not_found` without a partial write.

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

Each successful mutation returns a new revision; do not reuse the old one.
Rename reports backlink warnings but does not rewrite links. Prefer deprecation
when readers still need a transition path, and use delete only when removal is
intentional.

## Safety boundary

Writes are allowed only in the workspace's root bundle or an explicitly writable local
mount. Git and ordinary explicit mounts return `source_read_only`. The
workspace keeps locks beneath its ignored `.factile/` state directory, locks
before its final read and revision check, then
validates before returning the saved result.

Finish with a focused read and validation:

```bash
factile read /runbooks/cache-repair
factile validate /runbooks
```
