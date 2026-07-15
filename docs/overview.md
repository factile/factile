---
type: Reference
title: Factile Documentation Overview
description: Starting point for public Factile CLI documentation.
tags: [factile, docs, cli]
timestamp: 2026-07-15T00:00:00+02:00
---

# Factile Documentation Overview

This is the public documentation source for the local-first Factile CLI. It
owns:

- CLI installation and local knowledge workflows;
- the implemented command and text interfaces;
- local CLI architecture and contributor-facing boundaries; and
- agent, MCP, profile, and troubleshooting guidance for this repository.

Factile reads an active Markdown root, mounted local directories, and read-only
Git sources materialized in a generated cache under that root. The CLI, local
stdio MCP server, and embedded browser use the same workspace operations.

This source is self-contained. Building, testing, validating, or reading it
does not require a private repository, sibling checkout, credential, hosted
service, or host-specific mount. Hosted product behavior, private policy, and
shared normative contract maintenance live outside this public documentation.

Start with [Getting Started](guides/getting-started.md), then use the guides and
reference pages listed by the root navigation. Command help and implementation
tests are the executable evidence for current CLI behavior; when they and these
pages disagree, correct the documentation or the implementation in the same
change that establishes the intended behavior.
