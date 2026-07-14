# Security Policy

Factile is local-first software. It reads and writes local OKF content, can
materialize read-only Git repositories beneath an active root, and can expose
that content through the local UI and stdio MCP server.

## Git Source Boundary

Treat every Git repository as untrusted content. Factile uses the system Git
executable without a shell, disables interactive prompts and automatic Git LFS
downloads, does not initialize submodules, and does not execute hooks supplied
by a remote repository. Repository symlinks are rejected rather than followed.

Resolved commits are materialized as immutable generated snapshots under the
active root's `.factile/cache/git/` directory. Cache directories are private to
the current user and ignored by the root repository. Git mounts remain
read-only at the workspace boundary even if a descriptor is hand-edited.

Factile rejects Git URI passwords, HTTP(S) userinfo, and every literal `?`
query or `#` fragment delimiter before Git execution or descriptor creation,
including an empty trailing delimiter and `git+` compatibility sources.
Percent-encoded `%3F` and `%23` path data remain valid. Use Git's normal
credential helper or SSH agent/key configuration instead. Errors, traces, and
source status redact rejected sources. Do not include secrets in a mount
command, descriptor, or repository URL.

Automatic floating-source checks occur no more than once per 24 hours. An
explicit `factile refresh <mount-path>` performs an immediate check. If the
remote is unavailable, Factile may continue reading the last successful
snapshot and reports it as stale.

Git acquisition limits the effect of repository content, but does not make the
Markdown itself trusted. Review mounted content before relying on instructions,
links, or embedded resources from an unfamiliar repository.

## Local Exposure

The local UI and MCP server expose knowledge available to the Factile process.
Run them only in an environment where local clients and configured agents may
read that content. Reader mode prevents Factile mutations; it is not an
operating-system access-control boundary.

## Reporting a Vulnerability

Use GitHub private vulnerability reporting for this repository. Please do not
open a public issue for a suspected vulnerability.

Include:

- the affected version, commit, or command;
- a minimal reproduction when possible;
- the expected impact;
- whether local files, Git source/cache behavior, generated agent guidance, UI,
  or MCP behavior are involved.

There is no paid support or SLA process. The maintainers will prioritize reports
based on severity, reproducibility, and current project scope.
