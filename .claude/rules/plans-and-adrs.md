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

Per-file ADRs in [`docs/adr/`](../../docs/adr/) (ADR-013 onward) are **mutable** — edit them in place to keep them accurate against current state (fix a rotted link, correct a moved path, strip chronological noise). git history (`git log --follow` per file) is the audit trail. The combined historical log [`docs/Architecture-Decision-Records.md`](../../docs/Architecture-Decision-Records.md) (ADR-001–012) stays **frozen** — never edited or appended to.

Supersession is still used for genuine **decision changes** (a reversal or replacement, not a correction): create a new ADR with the next number, set its `supersedes:` frontmatter, and update the prior ADR's `status:`. Mutability keeps an ADR *true*; supersession records what *changed*. See [`docs/adr/ADR-036`](../../docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md).

When recording a new architectural decision:

1. Add a new file in [`docs/adr/`](../../docs/adr/) with the next sequential number.
2. Add an index row in [`.claude/decisions.md`](../decisions.md).

### ADRs may link only archived plans

Active plans get archived and renamed, so an ADR→active-plan link rots. ADRs may link a plan **only** under `plans/archive/…` (a stable target), alongside other stable targets: other ADRs, code, or external URLs. Put the rationale that matters **inline** in the ADR (it is the durable record), and any working-plan pointer in the mutable [`.claude/decisions.md`](../decisions.md) index, which *can* be kept current when plans move.

Enforced by [`pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) (`adr-plan-link`): a Write/Edit/MultiEdit of an ADR whose new content links a **non-archived** plan (`](…plans/….md)` not under `plans/archive/`) is blocked.
