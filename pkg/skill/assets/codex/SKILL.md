---
name: factile
summary: Use local Factile OKF knowledge when a task depends on repository-specific architecture, design decisions, domain concepts, workflows, runbooks, standards, policy, legal, compliance, or documentation knowledge.
description: Use local Factile OKF knowledge for architecture, design, documentation, review, runbook, standards, policy, legal, compliance, domain, or implementation-choice tasks that need repository knowledge. Discover local knowledge paths, retrieve focused context, and cite relevant concepts. Do not use for mechanical renames, formatting, syntax fixes, or obvious local edits.
---

# Factile local knowledge workflow

Factile exposes one workspace's OKF knowledge as a virtual filesystem.
Reader commands work on paths such as `/`, `/engineering`, and `/engineering/django`; a path may be backed by root-bundle Markdown files or mounted sources.
A workspace is marked by `factile.toml` with `[workspace]`. Its selected root bundle has its own `factile.toml` with `[bundle]` and supplies the logical `/` from every directory in that workspace. A secondary bundle remains portable content and does not become visible merely because it is nearby or contained in the workspace.
Mount descriptors are `<name>.mount.toml` files in the root bundle. Views live in workspace-level `factile.views.toml`. The workspace `.factile/` directory is ignored local state and cache only; never put authored knowledge, configuration, views, mount descriptors, or credentials there.
Use `--workspace <directory>` only for explicit selection; the named directory must itself contain `[workspace]`.
Use `--view <id>` on reader commands when a named view matches the task; views narrow scope without changing document paths.
Mount sources may be local directories or read-only Git repositories. Reader commands use the same paths for both. Inspect generated Git status with `factile mounts --json`; use `factile refresh <mount-path>` only when an immediate upstream check is needed. Refresh does not grant write access.
Git authentication belongs in normal credential helpers, OS keychains, SSH agents, or the process environment, never in Factile files or local state.

Use Factile when the task may depend on repository-specific:

- project architecture
- domain concepts
- previous decisions
- workflows
- runbooks
- standards
- policies
- legal or compliance references
- implementation choices or coding conventions documented as knowledge
- any task where grounded local context would reduce guessing

Do not use Factile for mechanical renames, formatting, syntax fixes, or obvious local edits that clearly need no project or domain knowledge.

## Workflow

1. Confirm the workspace boundary and selected root bundle:

   ```bash
   factile status --json
   ```

2. Discover available knowledge:

   ```bash
   factile list / --json
   ```

3. Inspect compact discovery cards when choosing where to look:

   ```bash
   factile list / --brief --json
   factile stat <path> --json
   ```

4. When a named view appears relevant, inspect it and use it to narrow reader commands:

   ```bash
   factile view inspect <view-id> --json
   factile context / '<one sentence task summary>' --view <view-id> --json
   ```

5. Get focused context for the task:

   ```bash
   factile context / '<one sentence task summary>' --json
   ```

6. If the context references a specific concept that matters, read it:

   ```bash
   factile read <document-path> --json
   ```

7. Use the retrieved knowledge to guide the work.

8. In the final response, mention the specific Factile concept paths used when relevant.

## Rules

- Use `factile context / '<task>' --json` after initial path and card discovery.
- Navigate progressively by Factile path; treat path boundaries as folders unless the user explicitly asks for curation.
- Use narrower paths when obvious, for example `factile context /project-docs '<task>' --json`.
- Do not edit OKF files unless the user explicitly asks to update knowledge.
- If Factile commands fail, continue normally and briefly note the issue.
- Do not invent Factile paths. Discover with `factile list` or `factile search` first.
- Keep Factile use proportional to the task.
