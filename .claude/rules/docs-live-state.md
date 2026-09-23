# Documentation Reflects Live State Only

**Applies to:** all documentation (`docs/**`) and every code, config, and
workflow comment. Companion to [`editing-and-scope.md`](editing-and-scope.md)
and the [`docs/README.md`](../../docs/README.md) conventions.

**Enforced by:**
[`scripts/tests/docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh)
(gauntlet shell-tests step). **No bypass.**

Documentation and comments describe only what is currently in place and live.

## The rules

- When something is removed, renamed, or replaced, every doc and comment that
  named it is updated to describe the current system. No note about the old
  state is left, and none is added.
- Behaviour is described positively: what the system does now, not what it
  stopped doing. "The `-data-dir` flag stores …", not "the `-data-dir` flag no
  longer stores the database".

Banned in live docs and comments (non-exhaustive):

- "X was retired / removed / decommissioned / deprecated"
- "the old X", "the previous X", "formerly X", "legacy X" (when X is gone)
- "X is now dormant", "dormant recovery path", "kept for rollback"
- "no longer does X", "used to do X", "previously …", "migrated from X"

## The gate

- It matches paragraph-joined text, so a phrase straddling an 80-column wrap is
  caught.
- It carries no allowlist.
- Its phrase list is narrower than the rule above — `used to `, `the old ` and
  `the previous ` are deliberately absent. Clearing the gate is the floor.
- Scope is `docs/**` minus `docs/adr/**` and
  `docs/Architecture-Decision-Records.md`.

## Exceptions

- Every ADR is out of the gate's scope: a Context section states the problem the
  decision solved. An ADR's descriptive body still follows this rule, and one
  whose decision leaves nothing behind is deleted rather than marked as past.
- Code comments may carry concise design rationale — why the current design is
  shaped this way — but not narration of removed features.
