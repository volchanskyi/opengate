# Plans and ADRs

**Enforced by:** [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh). **No bypass.**

## Plans

All agent plans must be created in **this repo's** `.claude/plans/` directory (i.e. `/home/ivan/opengate/.claude/plans/`), **not** the global `~/.claude/plans/`.

- Use a descriptive kebab-case name (e.g. `fix-auth-bug.md`, `phase-16-feature.md`). Never use auto-generated random names.
- If plan mode suggests a path under `~/.claude/plans/`, ignore it and use the project-local path instead.

### Delete a plan the moment its work is done (MANDATORY)

**Enforced by:** [`scripts/tests/plans-retirement.test.sh`](../../scripts/tests/plans-retirement.test.sh) (gauntlet shell-tests step). **No bypass.**

A plan is a working document. Once its implementation has landed, what it
described lives in the code, in [`/docs`](../../docs/) and in the ADRs — the
plan is a second, stale account of the same thing. So the commit that lands a
plan's final implementation MUST also `git rm` the plan, and add its
[`phases.md`](../phases.md) row in the same commit. Do not leave it for
"later"; that has been forgotten repeatedly.

`phases.md` rows link no plan. The consistency gate refuses one that does.

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

Every ADR describes **live state**. They are edited in place to stay accurate,
and git history (`git log --follow` per file) is the audit trail.

There is no superseded status and no supersession chain. When a decision
changes, its ADR is rewritten to say what is true now. When a decision leaves
nothing behind, its ADR is deleted and anything still live merges into the ADR
that replaced it. Numbers are never reused, so gaps are expected.

A minor fix or patch does not get an ADR. It belongs in the ADR whose decision
it refines, or nowhere.

When recording a new decision:

1. Add a file in [`docs/adr/`](../../docs/adr/) with the next number, carrying
   `number:` and `title:` frontmatter.
2. Add an index row in [`.claude/decisions.md`](../decisions.md).

### No plan links from docs

Plans are working documents and are deleted when their work lands, so nothing
durable may depend on one. No ADR and no page under `docs/` links a plan. Fold
what matters inline — the ADR is the durable record.

Enforced by [`pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh)
(a Write/Edit of an ADR whose content links a plan is blocked) and by
[`scripts/check-doc-links`](../../scripts/check-doc-links/), which scans
`docs/**` and `.claude/**` minus the `.claude/plans/**` working area and
refuses any plan link.
