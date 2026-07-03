# Documentation Reflects Live State Only

**Applies to:** all documentation (`docs/**`) and every code, config, and
workflow comment. Companion to [`editing-and-scope.md`](editing-and-scope.md)
(`/docs` is canonical) and the [`docs/README.md`](../../docs/README.md)
conventions.

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

- An ADR may record a genuine **decision change** through a `supersedes:` link —
  that is a decision record, not descriptive prose. Its *descriptive* body still
  follows this rule. Every ADR is editable to keep it true, including the
  combined historical log
  [`docs/Architecture-Decision-Records.md`](../../docs/Architecture-Decision-Records.md)
  (ADR-001–012).
- Code comments may carry concise **design rationale** (why the current design
  is shaped this way), but not narration of removed features.
