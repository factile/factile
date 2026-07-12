---
name: coding-practice
description: Apply shared Factile engineering practice to non-trivial software architecture, design, documentation, planning, review, diagnosis, implementation, verification, or delivery work where workflow selection, scope and authority, sequencing, evidence, or cross-project consistency matters. Retrieve focused guidance from the read-only /coding mount or canonical coding-practice root and combine it with closer project facts. Do not use for simple factual answers, formatting, mechanical renames, syntax-only fixes, or obvious one-line local edits unless risk or a crossed boundary makes the task non-trivial.
---

# Coding Practice

Use the shared bundle as a focused decision aid, not as a replacement for
project inspection or as permission to take broader action.

## Resolve Authority And Mode

1. Follow platform safety and authorization constraints.
2. Follow the explicit current user request.
3. Follow closer client and project instructions and verified repository facts.
4. Apply shared practice only as a compatible default.

Identify whether the user asked to explain, discuss, review, diagnose, plan,
implement, verify, or ship. Do not advance to a later mode without authority.

## Locate The Knowledge

Prefer the project's logical read-only mount:

```bash
factile list /coding --brief --json
```

If `/coding` is unavailable, use the canonical root configured by
`CODING_PRACTICE_ROOT`, falling back on this installation's path:

```bash
factile --root "${CODING_PRACTICE_ROOT:-/srv/knowledge/coding}" list / --brief --json
```

If neither source exists, continue from project guidance and live evidence.
Mention the missing shared source only when it materially limits the task.

## Retrieve Only Relevant Context

Summarize the actual task in one specific query. Narrow the path when the task
kind is already clear: use `/coding/workflows` for workflow selection,
`/coding/principles` for a decision rule, `/coding/verification` for completion
evidence, or `/coding/practices` for technical guidance. Use the bundle root
only when the task genuinely crosses those scopes.

For example:

```bash
factile context /coding/workflows "<task summary>" --json
```

When using the canonical root fallback:

```bash
factile --root "${CODING_PRACTICE_ROOT:-/srv/knowledge/coding}" \
  context /workflows "<task summary>" --json
```

Read individual concepts only when the context result needs more detail. Common
entry points are:

- `/coding/workflows/task-modes` for action and authority boundaries
- `/coding/workflows/tracked-design-to-delivery` for accepted, non-trivial,
  sequenced work
- `/coding/verification/definition-of-done` for proportional completion gates
- `/coding/governance/using-and-evolving` for adoption, overrides, and changes
- `/coding/practices` for a relevant reusable technical practice

Strip the `/coding` prefix when using the canonical-root fallback. Do not load
the whole bundle by default.

## Apply The Guidance

- Inspect the active repository, contracts, runtime, data, and worktree before
  relying on a generic pattern.
- Keep trivial work direct.
- For accepted non-trivial work, create the durable epic and sequenced tasks
  before repository edits when the tracked workflow's triggers apply.
- Implement one ready task at a time and close it only after its acceptance
  evidence passes.
- Verify the changed surfaces proportionately and distinguish static, local,
  integrated, hosted, and production evidence.
- Keep client-specific policy and examples in the client project.

Use reader commands only. Never curate the shared bundle, alter its mount, or
promote a correction without an explicit human request and review.

## Finish Truthfully

State the outcome first. Name the checks actually run, the highest evidence
level observed, material skips or pre-existing failures, and remaining risk.
Do not claim publishing, deployment, or production behavior from local
evidence.

For installation assets, use `assets/AGENTS.block.md` and
`assets/coding.mount.toml`; follow the canonical bundle's
`/governance/using-and-evolving` guide for reversible adoption.
