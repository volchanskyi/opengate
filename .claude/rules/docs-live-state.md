# Documentation Reflects Live State Only

**Applies to:** all documentation (`docs/**`) and every code, config, and
workflow comment. Companion to [`editing-and-scope.md`](editing-and-scope.md)
(`/docs` is canonical) and the [`docs/README.md`](../../docs/README.md)
conventions.

**Enforced by:**
[`scripts/tests/docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh)
(gauntlet shell-tests step). It matches paragraph-joined text, so a phrase that
straddles an 80-column wrap is still caught, and it carries **no allowlist**. Its
phrase list is narrower than the prose below — `used to `, `the old ` and
`the previous ` match ordinary live writing and are deliberately absent — so
clearing the gate is the floor, not the standard. Scope is `docs/**` minus
`docs/adr/**` and `docs/Architecture-Decision-Records.md`: an ADR's Context
section is required to state the problem the decision solved, so past-state is
structural there.

Documentation and comments describe **only what is currently in place and
live**. There is no value in documenting something that is no longer part of the
system.

## The rule

When something is removed, renamed, or replaced, update every doc and comment
that named it to describe the **current** system — do **not** leave, and do
**not** add, a note about the old state. Deleting an artifact and narrating its
funeral are two different jobs; only the first is wanted.

Banned in live docs/comments (non-exhaustive):

- "X was retired / removed / decommissioned / deprecated"
- "the old X", "the previous X", "formerly X", "legacy X" (when X is gone)
- "X is now dormant", "dormant recovery path", "kept for rollback"
- "no longer does X", "used to do X", "previously …", "migrated from X"

Describe behavior **positively** — say what the system does now, not what it
stopped doing. Replace "the `-data-dir` flag no longer stores the database" with
"the `-data-dir` flag stores …".

## Why

A live doc is a description of the system as it is. Past-state narration carries
no actionable information, misleads readers into thinking removed things still
matter, and rots the moment the next change lands.

## Exceptions

- Every ADR is out of the gate's scope for the reason above, and an ADR may
  record a genuine **decision change** through a `supersedes:` link —
  that is a decision record, not descriptive prose. Its *descriptive* body still
  follows this rule. Every ADR is editable to keep it true, including the
  combined historical log
  [`docs/Architecture-Decision-Records.md`](../../docs/Architecture-Decision-Records.md)
  (ADR-001–012).
- Code comments may carry concise **design rationale** (why the current design
  is shaped this way), but not narration of removed features.
