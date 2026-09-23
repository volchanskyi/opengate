# Plans and ADRs

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh). **No bypass.**

## Plans

- All agent plans live in **this repo's** `.claude/plans/` directory
  (`/home/ivan/opengate/.claude/plans/`), never the global `~/.claude/plans/`.
- Use a descriptive kebab-case name (`fix-auth-bug.md`,
  `phase-16-feature.md`). Never an auto-generated random name.
- If plan mode suggests a path under `~/.claude/plans/`, use the project-local
  path instead.

### Delete a plan the moment its work is done (MANDATORY)

**Enforced by:** [`scripts/tests/plans-retirement.test.sh`](../../scripts/tests/plans-retirement.test.sh) (gauntlet shell-tests step). **No bypass.**

- The commit that lands a plan's final implementation MUST also `git rm` the
  plan and add its [`phases.md`](../phases.md) row in the same commit.
- `phases.md` rows link no plan. The consistency gate refuses one that does.

### Plans vs memory

- **Plans** (`.claude/plans/`) — implementation details, steps, task
  breakdowns. Always a `.md` file in this directory.
- **Memory** (`~/.claude/projects/.../memory/`) — cross-session recall only:
  user preferences, project context, references. Never plans or task details.

## The state files: index, ledger, register

**Enforced by:**
[`scripts/tests/state-index-density.test.sh`](../../scripts/tests/state-index-density.test.sh)
(gauntlet shell-tests step). **No bypass.**

The ADR is the only home of a decision and its why. The three state files are
pointers with just enough text to let a reader choose a link:

| File | Role | Cap |
|---|---|---|
| [`decisions.md`](../decisions.md) | **Index** — number → one line → phase → status → link | 200 characters of prose per row |
| [`phases.md`](../phases.md) | **Ledger** — what shipped, in what order, linking the ADRs | 300 characters of prose per row |
| [`techdebt.md`](../techdebt.md) | **Register** — what is still owed, by severity, and its pay-down trigger | no cap; an entry states the debt, not a decision |

- Links, ADR numbers, phase names, dates and table scaffolding do not count
  against a cap. Only prose does.
- The gate checks the index is complete in both directions: every
  `docs/adr/ADR-*.md` has exactly one `decisions.md` row, and every row resolves
  to a file.
- **Shorten by moving, never by deleting.** Anything substantive the ADR does
  not carry is written into the ADR first.
- A row that cannot say what shipped inside its cap is describing a decision,
  and belongs in an ADR.

## ADRs

- Every ADR describes **live state**. ADRs are edited in place to stay accurate;
  `git log --follow` per file is the audit trail.
- There is no superseded status and no supersession chain. When a decision
  changes, its ADR is rewritten to say what is true now.
- When a decision leaves nothing behind, its ADR is deleted and anything still
  live merges into the ADR that replaced it.
- Numbers are never reused. Gaps are expected.
- A minor fix or patch does not get an ADR. It belongs in the ADR whose decision
  it refines, or nowhere.

When recording a new decision:

1. Add a file in [`docs/adr/`](../../docs/adr/) with the next number, carrying
   `number:` and `title:` frontmatter.
2. Add an index row in [`.claude/decisions.md`](../decisions.md).

### No plan links from docs

- No ADR and no page under `docs/` links a plan. Fold what matters inline.
- Enforced by [`pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh)
  and by [`scripts/check-doc-links`](../../scripts/check-doc-links/), which
  scans `docs/**` and `.claude/**` minus `.claude/plans/**`.
