# Editing Protocol and Scope Rules

## Editing protocol

### Read the whole file before editing numbered or globally-ordered structures

Numbered lists, ADR indexes, phases tables, OpenAPI parameter orderings, migration files, changelog entries — these are silent invariants. A partial insert that doesn't renumber the rest rots cross-references elsewhere in the file (e.g. "see step 17" prose downstream).

Before editing such a structure: `Read` the whole file, then `grep` for ordinal cross-references (`step [0-9]+`, `step #[0-9]+`, `section [0-9]\.[0-9]`, `phase [A-Z]:`) and consolidate the renumber into one Edit/Write.

### Never claim "SKIP" passes in /precommit

A pre-commit step that exits 0 because the underlying tool isn't on `$PATH` is a setup defect, not a pass — it appears clean locally but fails in CI. If a tool is missing, fail loudly with a clear setup message; don't write `|| echo "SKIP"`.

### Zero manual installation steps for the agent

Anything environment-specific (desktop tray, GUI shims, platform-only crates) must auto-detect at install time and silently no-op on unsupported environments. No `--flags`, no separate install scripts, no "also run X on desktop machines" documentation. One install command handles every fleet machine.

## Scope rules

### No operational scripts in refactor/audit plans unless explicitly requested

Backup scripts, retention jobs, alerting routines, and similar operational tooling are out of scope for codebase audits and refactoring efforts. Propose them separately when a user need surfaces. Hardening phases focus on configuration changes (resource limits, CI gates, alert rules), not new operational scripts.

### /docs is the canonical developer documentation

Each implementation phase ends with a [`/docs`](../../docs/) update step. The previous GitHub Wiki is deprecated; do not edit it.

Read [`/docs/README.md`](../../docs/README.md) before editing any doc — it defines the two non-negotiable conventions: (1) **link, don't paraphrase** — do not copy numbers, versions, flags, or paths into prose, link to the source; (2) **ADRs are immutable** — superseded ADRs get a new file, never an in-place edit.
