# Plans and ADRs

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh). **No bypass.**

## Plans

All agent plans must be created in **this repo's** `.claude/plans/` directory (i.e. `/home/ivan/opengate/.claude/plans/`), **not** the global `~/.claude/plans/`.

- Use a descriptive kebab-case name (e.g. `fix-auth-bug.md`, `phase-16-feature.md`). Never use auto-generated random names.
- If plan mode suggests a path under `~/.claude/plans/`, ignore it and use the project-local path instead.

### Archive a plan the moment its work is done (MANDATORY)

**Enforced by:** [`scripts/tests/plans-archive-consistency.test.sh`](../../scripts/tests/plans-archive-consistency.test.sh) (gauntlet shell-tests step) + [`scripts/check-doc-links`](../../scripts/check-doc-links/). **No bypass.**

The commit that lands a micro-plan's final implementation MUST also retire the plan — do **not** leave it for "later" (that has been forgotten repeatedly). In the **same commit**:

1. `git mv .claude/plans/<plan>.md .claude/plans/archive/<plan>.md`.
2. Bump every internal relative link **one `../` deeper** (`../../` → `../../../`) — a freshly-archived plan isn't baselined, so stale links fail the doc-links gate. Validate with `GO111MODULE=off go run ./scripts/check-doc-links`.
3. Repoint every reference to it — the master-plan index row, the `phases.md` **Completed** row (link `plans/archive/<plan>.md`), and cross-refs in sibling plans — to the `archive/` path.

The consistency gate refuses any `phases.md` **Completed** row whose Plan link resolves to a **non-archived** plan, so recording a phase as done forces its plan into `archive/`. Pair this with the existing "update `phases.md` after completing significant work" rule in [`CLAUDE.md`](../../CLAUDE.md): finishing a workstream means a `phases.md` Completed row **and** the plan archived, together, in the completing commit.

### Plans vs memory

Plans and memory serve different purposes. Never confuse them:

- **Plans** (`.claude/plans/`) — implementation details, steps, and task breakdowns. Always a `.md` file in this directory.
- **Memory** (`~/.claude/projects/.../memory/`) — only for cross-session recall: user preferences, project context, references. Never store plans or task details here.

## The state files: index, ledger, register

**Enforced by:**
[`scripts/tests/state-index-density.test.sh`](../../scripts/tests/state-index-density.test.sh)
(gauntlet shell-tests step). **No bypass.**

The ADR is the only home of a decision and its why. The three state files are
pointers with just enough text to let a reader choose a link:

| File | Role | Cap |
|---|---|---|
| [`decisions.md`](../decisions.md) | **Index** — number → one line → phase → status → link | 200 characters of prose per row |
| [`phases.md`](../phases.md) | **Ledger** — what shipped, in what order, linking the plan and the ADRs | 300 characters of prose per row |
| [`techdebt.md`](../techdebt.md) | **Register** — what is still owed, by severity, and its pay-down trigger | no cap; an entry states the debt, not a decision |

Links, ADR numbers, phase names, dates and table scaffolding do not count against
a cap — only prose. The gate also checks the index is complete in **both**
directions: every `docs/adr/ADR-*.md` has exactly one `decisions.md` row, and
every row resolves to a file.

**Shorten by moving, never by deleting.** Before a row is cut, check its
distinctive terms against the ADR it points at; anything substantive the ADR does
not carry is written **into the ADR first**. A row that genuinely cannot say what
shipped inside its cap is describing a decision, and belongs in an ADR rather
than in a longer row.

## ADRs

All ADRs are **mutable** — edit them in place to keep them accurate against current state (fix a rotted link, correct a moved path, strip chronological/past-state noise per [`docs-live-state.md`](docs-live-state.md)). This covers both the per-file ADRs in [`docs/adr/`](../../docs/adr/) (ADR-013 onward) and the combined historical log [`docs/Architecture-Decision-Records.md`](../../docs/Architecture-Decision-Records.md) (ADR-001–012). git history (`git log --follow` per file) is the audit trail.

Supersession is still used for genuine **decision changes** (a reversal or replacement, not a correction): create a new ADR with the next number, set its `supersedes:` frontmatter, and update the prior ADR's `status:`. Mutability keeps an ADR *true*; supersession records what *changed*. See [`docs/adr/ADR-036`](../../docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md).

When recording a new architectural decision:

1. Add a new file in [`docs/adr/`](../../docs/adr/) with the next sequential number.
2. Add an index row in [`.claude/decisions.md`](../decisions.md).

### Plan links from docs

Plans are **ephemeral** — active plans get archived/renamed, and archived plans get **deleted** in cleanups. So permanent documentation must not depend on them. Two rules, by document class:

- **ADRs** (`docs/adr/ADR-*.md`) may link a plan **only** under `plans/archive/…` — a stable-enough target for a decision record — alongside other stable targets (other ADRs, code, external URLs). Never link an **active** plan (it rots when archived). Put the rationale that matters **inline** in the ADR (it is the durable record), and any working-plan pointer in the mutable [`.claude/decisions.md`](../decisions.md) index.
- **All other docs under `docs/`** (Testing.md, Home.md, …) must **not link any plan at all** — archived or active. Fold the rationale inline or reference [`.claude/decisions.md`](../decisions.md). A doc that links an archived plan breaks the moment that plan is cleaned up.

Enforced by two mechanisms:

- [`pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) (`adr-plan-link`): a Write/Edit/MultiEdit of an ADR whose new content links a **non-archived** plan (`](…plans/….md)` not under `plans/archive/`) is blocked.
- [`scripts/check-doc-links`](../../scripts/check-doc-links/) (gauntlet): scans durable sources only — `docs/**` and `.claude/**` **minus the ephemeral `.claude/plans/**` working-area** (active plans and `archive/`), whose files are deletion-bound and whose internal links rot by design. Within that scope it refuses any **active-plan** link and any **plan link at all** (archived included) from a non-ADR doc under `docs/`. Plan files remain valid link *targets*; they are simply no longer scanned as *sources*, so the gate is a clean "zero broken links" with no baseline ledger to maintain.
