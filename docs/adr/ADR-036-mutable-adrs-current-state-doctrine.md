---
adr: 036
title: Per-file ADRs become mutable; current-state docs doctrine
status: Accepted
date: 2026-06-11
supersedes: ADR-013 (immutability clause #3 only)
---

# ADR-036: Per-file ADRs Become Mutable; Current-State Docs Doctrine

## Status

Accepted — 2026-06-11.

Supersedes **only** clause #3 ("ADRs are immutable") of
[ADR-013](ADR-013-docs-in-repo-and-immutable-adrs.md). The other two decisions
in ADR-013 — **#1 docs live in-repo** and **#2 link-over-paraphrase / wiki
deprecated** — remain in force and are **not** affected. ADR-013 is otherwise
intact; this is a scoped, partial supersession of one of its three bundled
decisions.

## Context

ADR-013 made per-file ADRs immutable: once accepted, an ADR was never edited in
place, and a changed decision required a whole new superseding ADR. The intent
was an honest, append-only decision history that a reader could walk to
reconstruct "why was it X, and what changed?".

In practice the immutability rule fought a second goal the maintainers now want:
**documentation that describes current state**, with every file/spec reference a
working relative link. Two frictions surfaced:

1. **Stale links and facts cannot be corrected.** An ADR that links a path,
   names a flag, or cites a file which later moves becomes permanently wrong —
   the only "fix" was a new ADR whose sole purpose was to restate the old one
   with a corrected link. That is noise, not history.

2. **The append-only chain itself drifts from reality.** A reader chasing a
   supersession chain across several ADRs to assemble the current decision is
   doing work a single, maintained ADR would have saved them.

git history already preserves the audit trail that immutability was protecting:
every edit to an ADR is a diff with an author and a date, recoverable with
`git log --follow`. The `date:`/`status:`/`supersedes:` frontmatter records the
decision lineage inline. So the immutability rule was paying a real correctness
cost (uncorrectable drift) for a guarantee git already provides.

## Decision

### 1. Per-file ADRs (013+) are mutable

ADRs under [`docs/adr/`](./) (ADR-013 onward) may be **edited in place** to keep
them accurate against current state: fix a rotted link, correct a moved path,
strip chronological/logistical noise from the body, tighten wording. The
[`pretooluse-write-guard.sh`](../../.claude/hooks/pretooluse-write-guard.sh) hook
no longer blocks edits to ADR files.

Supersession is **still used for genuine decision *changes***. When a decision
is reversed or replaced, write a new ADR with the next number, set its
`supersedes:` frontmatter, and update the prior ADR's `status:` — the lineage
stays explicit. The difference: mutability covers *keeping an ADR true*;
supersession covers *changing what was decided*. git history is the audit
trail for both.

### 2. The combined historical log (001–012) stays frozen

[`docs/Architecture-Decision-Records.md`](../Architecture-Decision-Records.md)
(ADR-001 through ADR-012) is **out of scope** — it remains the frozen historical
log per ADR-013 #2 and [`docs/README.md`](../README.md). Mutability reaches only
the per-file ADRs (013+); the combined log is never edited or appended to.

### 3. Success = content quality; net line delta is explicitly NOT a gate

The current-state doctrine (below) is graded on **content quality**, not line
count. Removing genuine noise and *adding* a correct clickable link are both
wins; a "net-negative-or-you're-wrong" line budget is explicitly rejected — it
would reward deleting durable rationale and fight the coverage / TDD / ADR / PMAT
gates. The real success signals are deterministic:

- the doc link-checker reports zero broken links, zero active-plan links, and
  zero plan links (archived included) from non-ADR docs under `docs/` — now
  enforced literally, with no baseline ledger: the ephemeral
  `.claude/plans/**` working-area (active plans and `archive/`) is out of the
  checker's scan scope, so its by-design link rot no longer counts against the
  signal, and
- the inventory greps (dates-in-prose, commit/PR IDs, phase/PR tokens,
  config-duplicated schedules) trend to ~0 across the scoped trees.

### 4. ADRs may link only ARCHIVED plans

An ADR may link a plan **only** under `plans/archive/…`. Active plans get
archived and renamed, so an ADR→active-plan link rots. Archived plans are stable
targets. For any other working-plan pointer, fold the rationale inline (the ADR
is the durable record) or reference the mutable
[`.claude/decisions.md`](../../.claude/decisions.md) index, which can be kept
current as plans move. The write-guard hook enforces this for every
Write/Edit/MultiEdit of an ADR.

### The current-state doctrine (applies to docs, ADR bodies 013+, and code comments)

**PURGE** (noise): chronological prose ("verified 2026-05-19", "over the last
two weeks"), PR/commit identifiers in prose (`(PR 9)`, "removed in commit
9236826"), phase/PR labels used as logistics, speculative-future notes
("a subsequent ADR will tighten…"), negative-state prose ("there is no SARIF
export…"), and schedules/values duplicated from config (link to the workflow
instead).

**KEEP** (durable): the substantive decision/behavior/why, structural metadata
(ADR `date:`/`status:`/`supersedes:` frontmatter, table schemas), and any
explanation that is the only pointer to non-obvious rationale — rewritten to
state it **directly** rather than by citation. Exported-symbol doc comments are
**rewritten, never deleted** (Go requires them; clippy/eslint/PMAT-TDG grade
them).

**Reconciliation rule** (purge refs *and* keep the why): in ADR bodies and code
comments, remove every `ADR-NNN §x` / plan / phase / PR **token** and rephrase
to describe the behavior/why directly. Fully delete a comment only when it was
*purely* a citation or *purely* speculative-future with no current-state
content. Worked example: "SARIF export was removed in commit 9236826 (the
dismissed-fingerprint bug made new alerts invisible)" → "SARIF export was removed
because the dismissed-fingerprint bug made new alerts invisible" — drops the SHA
token, keeps the behavior and the reason.

## Consequences

**Positive.**

- ADRs can be corrected against current state, so stale links and moved paths
  are fixed in place instead of spawning restate-only ADRs.
- A single maintained ADR replaces multi-hop supersession chains for the common
  "keep it accurate" case; supersession is reserved for real decision changes.
- The doctrine + the link-checker + inventory greps give a deterministic, no-line-budget definition of "done" for the docs cleanup.

**Negative.**

- The append-only guarantee is gone: the live ADR text no longer *is* the full
  history. Mitigation: git history (`git log --follow` per ADR file) plus the
  `date:`/`status:`/`supersedes:` frontmatter and the maintained
  [`.claude/decisions.md`](../../.claude/decisions.md) index are the audit trail.
- Purging mechanical noise from ADR *bodies* is the highest-risk part of the
  doctrine. Guardrail: the reconciliation rule above — rewrite to preserve the
  fact and the why, never delete substantive rationale, and keep the frontmatter.

**Mitigated risks.**

- "Could mutability silently rewrite a decision?" — no: a *decision change* still
  requires a new superseding ADR with `supersedes:` set; mutability is for
  keeping an ADR true, and every edit is a reviewable git diff.
- "Does this rescind docs-in-repo or link-over-paraphrase?" — no: this ADR
  scopes its supersession to ADR-013 clause #3 only; clauses #1 and #2 stand.

## Supersession history

None. This ADR partially supersedes ADR-013 (clause #3 only); it is not itself
superseded.
