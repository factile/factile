---
name: factile
summary: Use local Factile OKF knowledge when a task depends on repository-specific architecture, design decisions, domain concepts, workflows, runbooks, standards, policy, legal, compliance, or documentation knowledge.
description: Use local Factile OKF knowledge for architecture, design, documentation, review, runbook, standards, policy, legal, compliance, domain, or implementation-choice tasks that need repository knowledge. Discover local knowledge paths, retrieve focused context, and cite relevant concepts. Do not use for mechanical renames, formatting, syntax fixes, or obvious local edits.
---

# Factile local knowledge workflow

Factile exposes local OKF knowledge as a virtual filesystem.
Reader commands work on paths such as `/`, `/engineering`, and `/engineering/django`; do not stop to classify a path as a Library, Knowledge Base, bundle link, or OKF folder before navigating it.
At a Knowledge Base path, reader commands include every linked Bundle. Use a narrower path when the task scope is specific.
Use `--view <id>` on reader commands when a named library view matches the task; views narrow scope without changing document paths.

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

1. Check that Factile is available:

   ```bash
   factile list / --json
   ```

2. Inspect compact discovery cards when choosing where to look:

   ```bash
   factile list / --brief --json
   factile stat <path> --json
   ```

3. When a named library view appears relevant, inspect it and use it to narrow reader commands:

   ```bash
   factile view inspect <view-id> --json
   factile context / '<one sentence task summary>' --view <view-id> --json
   ```

4. Get focused context for the task:

   ```bash
   factile context / '<one sentence task summary>' --json
   ```

5. If the context references a specific concept that matters, read it:

   ```bash
   factile read <concept-path> --json
   ```

6. Use the retrieved knowledge to guide the work.

7. In the final response, mention the specific Factile concept paths used when relevant.

## Rules

- Use `factile context / '<task>' --json` after initial path and card discovery.
- Navigate progressively by Factile path; treat path boundaries as folders unless the user explicitly asks for catalog curation.
- Use narrower paths when obvious, for example `factile context /project-docs '<task>' --json`.
- Do not edit OKF files unless the user explicitly asks to update knowledge.
- If Factile commands fail, continue normally and briefly note the issue.
- Do not invent Factile paths. Discover with `factile list` or `factile search` first.
- Keep Factile use proportional to the task.
