---
name: factile
description: Use local Factile OKF knowledge for architecture, design, documentation, review, runbook, standards, policy, legal, compliance, domain, or implementation-choice tasks that need repository knowledge. Discover local knowledge paths, retrieve focused context, and cite relevant concepts. Do not use for mechanical renames, formatting, syntax fixes, or obvious local edits.
---

# Factile local knowledge workflow

## Workspace model

- The nearest ancestor `factile.toml` with `[workspace]` is the workspace
  boundary. Its selected root bundle has `[bundle]`, either in a separate
  `factile.toml` or in the same manifest when the workspace root is also the
  bundle.
- The selected root bundle supplies the same logical `/` throughout the
  workspace. Nearby or contained secondary bundles are invisible unless
  mounted.
- Mount descriptors are `<name>.mount.toml` files in the root bundle. Views
  live in workspace-level `factile.views.toml` and narrow scope without
  changing document paths.
- `.factile/` is ignored workspace-local state and cache only. Never put
  authored knowledge, configuration, views, mount descriptors, or credentials
  there.
- Sources may be local directories or read-only Git repositories. Git
  authentication belongs in normal credential helpers, OS keychains, SSH
  agents, or the process environment.
- Use `--workspace <directory>` for explicit selection. `factile init` may
  establish a workspace in that existing directory; every other workspace-aware
  command requires the named directory itself to contain `[workspace]`.
- Repository setup and repair use `factile init`. It writes tracked manifests,
  starter knowledge, and generated repo integration, so run it only when the
  user explicitly asks for initialization or repair. Use `--yes --json` for a
  requested non-interactive run. Repeated init is the normal repair and upgrade
  path; it refuses unrecognized generated ownership and malformed managed
  markers instead of overwriting them.

## Workflow

1. Confirm the workspace and selected root bundle:

   ```bash
   factile status --json
   ```

2. Choose the smallest useful discovery step:

   ```bash
   factile list / --brief --json
   factile stat <known-path> --json
   factile view inspect <relevant-view> --json
   ```

   If the task or closer guidance names an exact concept path, read or inspect
   it directly after `status`. Use `factile list / --json` only when the full
   tree matters; do not run both full and brief root listings by default.

3. Get focused context at the narrowest sensible path. Use `/` only for a
   genuinely cross-cutting task:

   ```bash
   factile context <path> '<one sentence task summary>' --json
   factile context <path> '<one sentence task summary>' --view <view-id> --json
   ```

   Use `factile search <path> '<query>' --json` when the brief cards do not
   reveal the relevant path.

4. Read only the specific concepts needed for the decision:

   ```bash
   factile read <document-path> --json
   ```

5. Apply the retrieved knowledge with current repository facts. Mention the
   specific Factile concept paths used when relevant.

## Rules

- Prefer JSON output for stable agent-facing results.
- Do not classify a path as local or mounted before navigating it.
- Do not invent Factile paths. Discover them or use an exact path supplied by
  closer guidance.
- Do not edit Factile configuration, views, mount descriptors, or OKF files
  unless the user explicitly asks to curate knowledge.
- If Factile commands fail, continue normally and briefly note the issue.
- Keep Factile use proportional to the task.
