---
type: Guide
title: Migrate a Local Bundle to OKF 0.3
---

# Migrate a Local Bundle to OKF 0.3

## Initialize a new bundle

Run these commands in a new directory to initialize and validate the bundle.

```bash
factile init --root docs --name my-knowledge --title "My Knowledge" --yes
factile --okf-bundle ./docs validate /
```

Fresh initialization writes the 0.3 declaration, title and description to the
root `index.md`. The standalone selector reads a physical directory without
requiring a workspace manifest or following its mounts. Existing workspace
bundles keep their legacy interpretation until explicitly migrated. The
standalone reader applies portable review and reference rules when inspecting
older content. Both readers leave the original files untouched.

## Preview and apply

Upgrade an OKF 0.1, 0.2 or unversioned bundle with one reviewed plan. Save the
plan and receipt outside the bundle to keep its inventory stable.

```bash
factile migrate /absolute/path/to/bundle --json > /tmp/migration-preview.json
plan=$(jq -r .plan_revision /tmp/migration-preview.json)
factile migrate /absolute/path/to/bundle --apply --expect-plan "$plan" --json > /tmp/migration-result.json
factile --okf-bundle /absolute/path/to/bundle validate /
```

The operand selects one physical bundle directory. The inventory reports
ordinary Markdown, assets, reserved files and independently declared nested
bundles. Mount descriptors and links never cause migration of another source.
Hidden paths and symlinks are skipped. A nested bundle blocks migration of the
outer directory. Select the nested bundle directly and reconcile the outer
distribution separately.

Review the inventory, per-file revisions, diffs, diagnostic findings and
candidate validation. `--expect-plan` binds the apply to the selected root,
observed bytes and proposed edits. `--apply` by itself prepares a fresh plan.
A stale plan, unknown declared format, unresolved ambiguity or invalid
candidate produces exit 3. Earlier bundles remain readable through normal
commands before an explicit migration.

The migration copies the candidate to an isolated directory, including its
assets and schemas. It checks the physical inventory under a migration lock,
replaces documents atomically, and checks the resulting inventory before
publishing the root version marker last. If a write fails, the receipt reports
completed replacements and the old declaration remains. Fix the reported
problem, preview the current bytes, and retry. A compliant OKF 0.3 bundle
produces zero changes.

An unknown future declaration remains readable for inspection with an
unsupported-version diagnostic. An older Factile writer must leave it alone.
Check the bundle with a reader and writer that support the declared version
before attempting a conversion.

## Conversion decisions

- Old timestamps fill an absent `generated.at` only when an explicit producer
  supports the attribution. Other old values remain in `legacy_metadata.timestamp`.
  Existing generation and review history stays authored. The report identifies
  changes in derived review tier.
- Supported links and bare HTTP URLs in legacy citation lists become source
  entries. Their prose remains in the body. Fix ambiguous citations by
  identifying the resource explicitly. No author or claim association is inferred.
- The legacy relative lookup takes priority. When only the root target exists,
  the migrator writes a rooted URI reference with the existing query and
  fragment. Different plausible targets produce a blocking finding. Choose
  an explicit `./relative.md` or `/rooted.md` path before retrying.
- Recognized deprecation flags become native lifecycle metadata. Conflicting
  values need explicit reconciliation. An authored unknown status remains
  in the document and is excluded from current-only results.
- The selected root index gains the quoted 0.3 declaration and keeps its
  existing metadata. Legacy frontmatter in ordinary indexes and logs stays
  available in a Markdown block. Assets and untyped Markdown retain their bytes.

## Publish a migrated revision

Confirm that readers in the target environment accept both the old and new
formats before allowing a 0.3 writer to publish there. A migrated bundle becomes
a new hosted revision. Record the selected channel and the old immutable
revision, then reopen both old and new by explicit revision before promoting a
channel. Compare the evidence, linked assets and status at a fixed evaluation
time. A date-only 0.3 review deadline takes effect at midnight UTC.

If a reader needs more time, restore the prior channel revision and verify both
the channel and historical pin. Consumers pinned to the new revision keep that
selection after a channel rollback. Keep 0.3-capable readers available for those
pins and coordinate a change to any affected consumer selection separately.
