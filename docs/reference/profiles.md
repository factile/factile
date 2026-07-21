---
type: Reference
title: Profiles and Recipes
description: Implemented profile, template, recipe-data, and skill-install behavior in Factile v0.4.
tags: [factile, profiles, recipes, skills, templates]
timestamp: 2026-07-20T00:00:00+02:00
---

# Profiles and Recipes

Profiles are optional data used when Factile installs agent guidance. They do
not create another workspace engine, command model, or validation contract.

## Software profile

The bundled seed lives at `profiles/software/` and contains:

- `profile.json`, which names the profile, document categories, templates, and
  recipe IDs;
- Markdown templates for ADRs, APIs, data models, deployments, domain
  concepts, modules or services, runbooks, security notes, testing strategies,
  and workflows; and
- JSON recipe data for answering questions, reviewing code, designing and
  documenting features, writing runbooks, capturing decisions, and validating
  bundles.

Select it while installing supported agent guidance:

```bash
factile skill install codex \
  --scope repo \
  --mode curator \
  --profile software
```

That command is the explicit reconfiguration surface. Later `factile init`
runs preserve the installed profile and reader/curator mode while refreshing
the generated repo integration.

Reader and curator are installation postures:

- reader guidance emphasizes discovery and configures read-only MCP; and
- curator guidance includes explicit mount, view, and document mutation
  workflows with write-capable MCP.

They do not change source capabilities. A read-only mount remains read-only in
curator mode.

## Recipe boundary

Recipes are guidance data. Factile v0.4 has no `factile recipe` command, recipe
runner, background workflow engine, or recipe-specific workspace API. A person
or agent may follow the ordered guidance using ordinary Factile commands.

This boundary keeps profiles replaceable and keeps the core engine path-based.
New domain templates or recipes should remain data unless a concrete user need
requires a new generic CLI capability.

Inspect available skill support and verify an install with:

```bash
factile skill list
factile skill inspect codex
factile skill doctor codex --json
```
