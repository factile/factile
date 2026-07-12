---
name: coding-practice
description: Use shared Factile engineering practice when a non-trivial software task needs a decision about authority, process weight, durable coordination, completion evidence, or a documented technical practice. Read one exact mapped concept first and combine it with closer project facts. Do not use for simple factual answers, formatting, mechanical renames, syntax-only fixes, or obvious local edits unless material risk changes the decision.
---

# Coding Practice

Use this skill as a small router, not as a prompt-sized manual.

## Respect Authority

Follow platform constraints, then the explicit current request, then closer
project instructions and verified facts. Shared practice is a compatible
default; it never broadens permission.

If the task is trivial and low-risk, stop here and handle it directly.

## Read One Exact Concept

Choose the most specific matching route. An explicit subject in the current
request wins. Use task modes only when the question is about allowed actions or
mode boundaries, or when no more specific route applies.

| Need | Read |
|---|---|
| Django CI, versioning, release, or Docker delivery | `/coding/practices/ci/django-apps` |
| Adopt, update, override, or remove this bundle | `/coding/governance/using-and-evolving` |
| Concise completion note | `/coding/verification/evidence-record` |
| Completion or verification depth | `/coding/verification/definition-of-done` |
| Accepted work needing durable coordination | `/coding/workflows/tracked-design-to-delivery` |
| Repository facts versus a generic pattern | `/coding/principles/project-facts` |
| Design simplicity or process weight | `/coding/principles/simplicity` |
| Allowed actions or mode boundary | `/coding/workflows/task-modes` |

Read the selected path directly:

```bash
factile read <mapped-path> --json
```

If `/coding` is unavailable, strip that prefix and use the canonical checkout:

```bash
factile --root "${CODING_PRACTICE_ROOT:-/srv/knowledge/coding}" \
  read <mapped-path-without-/coding> --json
```

If neither source exists, continue from project guidance and live evidence.
Mention the missing source only when it materially limits the work.

The exact read selected here satisfies Factile knowledge retrieval for this
shared bundle. Do not also run generic Factile list or context discovery for the
same bundle unless the mapped path fails. Retrieve separate project knowledge
when the task needs it.

## Use Context Only For Ambiguity

Use `context` only when no route above fits or the question genuinely crosses
several concepts. Choose the narrowest applicable scope, such as
`/coding/principles`, `/coding/workflows`, `/coding/verification`, or a precise
technical-practice path.

Never request root `/coding` context by default. Do not load every related
concept after an exact route answered the decision.

## Apply It Locally

Inspect the active repository, contracts, runtime, data, and worktree. Use local
commands and closer policy. Keep client-specific rules in the client project.
For tracked work, follow its readiness and evidence gates one task at a time.

Use reader commands only. Do not curate, publish, mount, or promote shared
practice without an explicit request.

## Finish Truthfully

Lead with the outcome. Name checks actually run, the highest evidence level
observed, material skips or pre-existing failures, and remaining risk. Local
evidence never proves publication, deployment, or production behavior.
