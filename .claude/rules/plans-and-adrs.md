# Plans and ADRs

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh). **No bypass.**

## Plans

All agent plans must be created in **this repo's** `.claude/plans/` directory (i.e. `/home/ivan/opengate/.claude/plans/`), **not** the global `~/.claude/plans/`.

- Use a descriptive kebab-case name (e.g. `fix-auth-bug.md`, `phase-16-feature.md`). Never use auto-generated random names.
- If plan mode suggests a path under `~/.claude/plans/`, ignore it and use the project-local path instead.
- Completed plans are archived to `.claude/plans/archive/`.

### Plans vs memory

Plans and memory serve different purposes. Never confuse them:

- **Plans** (`.claude/plans/`) — implementation details, steps, and task breakdowns. Always a `.md` file in this directory.
- **Memory** (`~/.claude/projects/.../memory/`) — only for cross-session recall: user preferences, project context, references. Never store plans or task details here.

## ADRs

ADRs in [`docs/adr/`](../../docs/adr/) are **immutable**. Never edit an accepted ADR in place — supersede it with a new ADR.

When recording a new architectural decision:

1. Add a new immutable file in [`docs/adr/`](../../docs/adr/) with the next sequential number.
2. Add an index row in [`.claude/decisions.md`](../decisions.md).

### ADRs must not link plan files

An ADR is immutable but plans are not — they get archived and renamed. A link from an ADR to a plan file therefore rots into a dead link that can **never** be repaired (the ADR can't be edited). So ADRs link only to **stable** targets: other ADRs, code, or external URLs. Put the rationale that matters **inline** in the ADR (it is the durable record), and any "see the working plan" pointer in the mutable [`.claude/decisions.md`](../decisions.md) index, which *can* be kept current when plans move.

Enforced by [`pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) (`adr-plan-link`): a new ADR whose content contains a `](…plans/….md)` link is blocked. (Pre-existing ADRs with plan links are left as-is — historical; the maintained pointer is `decisions.md`.)

## AGENT.MD

`AGENT.MD` at the repo root is a symlink to `CLAUDE.md` for cross-tool compatibility (Aider, AGENTS.md-aware tooling). Do not edit it as a separate file — changes propagate automatically via the symlink.
