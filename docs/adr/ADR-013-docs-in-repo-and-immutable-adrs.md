---
adr: 013
title: Docs live in-repo; wiki deprecated; ADRs are immutable
status: Accepted
date: 2026-04-11
supersedes: none
---

# ADR-013: Docs Live In-Repo; Wiki Deprecated; ADRs Are Immutable

## Status

Accepted — 2026-04-11.

## Context

Developer documentation was historically maintained in a separate GitHub wiki
repository (`volchanskyi/opengate.wiki`, cloned locally alongside the main
repo). This produced recurring, hard-to-catch drift between what the code does
and what the wiki claims it does. Concrete incidents over the last two weeks:

1. **Coverage threshold drift.** The per-language unit test jobs were raised
   from 70% to 80% in CI. The wiki kept describing the threshold as 70% for
   weeks, because the PR that touched `ci.yml` had no reason to touch the
   wiki repo.

2. **SARIF export drift.** A SARIF upload step feeding GitHub Code Scanning
   was removed in commit 9236826 (the dismissed-fingerprint bug made new
   alerts invisible). The wiki continued to describe SARIF export as a
   feature long after removal — again because the commit that deleted the
   step was in the main repo and the wiki change was a separate, manual,
   easy-to-forget step.

3. **In-place ADR edits.** ADR-012 ("SonarCloud Quality Gate as Hard Merge
   Block") was edited in place multiple times as the policy evolved.
   Readers trying to reconstruct history from ADR-012 alone cannot tell what
   the gate used to require or when it changed.

The root cause in all three cases is structural: a separate wiki repo means
code changes and documentation changes live in separate review contexts, so
the review process cannot catch drift. The secondary cause is a cultural
habit of *paraphrasing* facts (numbers, versions, flags) into prose, which
guarantees eventual drift whenever the source of truth changes.

## Decision

Three linked changes, adopted together:

### 1. Move developer documentation into the main repository

All long-form developer docs live under `/docs` in the main repo. The pages
previously in the GitHub wiki have been relocated here. The GitHub wiki repo
(`volchanskyi/opengate.wiki`) is deprecated and its contents removed.

Entry point: [`docs/Home.md`](../Home.md). Conventions: [`docs/README.md`](../README.md).

### 2. Link, don't paraphrase

Documentation must not copy numbers, versions, flags, file paths, or port
numbers into prose. Any such fact must be **linked** to the file in which it
actually lives.

Test for whether a sentence violates the rule: *"If the underlying code
changes, would I need to come back and edit this sentence?"* If yes, the
sentence is drift-prone and must be rewritten as a link.

When a number must appear inline (e.g. in a summary table), the link to the
source must be adjacent so a reader can verify it in one click.

### 3. ADRs are immutable

Once an ADR is accepted, it is never edited in place. If a decision changes:

1. A new ADR is created with the next available number.
2. The new ADR's header lists `supersedes: ADR-NNN`.
3. The old ADR's status is updated **exactly once** to
   `Superseded by ADR-NNN` and otherwise left untouched.

This preserves the historical record: a reader who wants to know "why was
the coverage threshold 70% originally, and what changed?" can walk the
supersession chain. The combined historical log at
[`docs/Architecture-Decision-Records.md`](../Architecture-Decision-Records.md)
(ADR-001 through ADR-012) is **frozen** — it is kept as-is for history and
not appended to. New ADRs live as individual files in
[`docs/adr/`](./) using the `ADR-NNN-kebab-title.md` convention.

### Enforcement

Two defences run continuously:

- **`/wiki-audit` skill** at [`.claude/skills/wiki-audit/SKILL.md`](../../.claude/skills/wiki-audit/SKILL.md)
  greps the docs for drift-prone patterns (percentages, version pins, paths,
  config flags, port numbers, SonarCloud/SARIF claims) and verifies each hit
  against the source of truth. Auto-fixes the unambiguous cases; flags the
  rest. ADR content drift is *flagged*, never auto-fixed (ADRs are immutable).

- **Documentation conventions are part of `CLAUDE.md`** so every session picks
  them up without re-reading this ADR.

## Consequences

**Positive.**

- A PR that changes `ci.yml`, `sonar-project.properties`, `Cargo.toml`,
  `go.mod`, `package.json`, or deploy configs can be reviewed alongside the
  docs update in the same diff. Drift is caught at review time, not weeks
  later.
- Code search finds doc references to any symbol, making renames safer.
- The convention of linking over paraphrasing moves the source-of-truth
  burden from prose onto the linked file. Prose that passes this bar
  doesn't need to be re-audited every time an underlying number changes.
- ADR supersession preserves an honest decision history. A reader who needs
  to understand "why" can trace the chain.

**Negative.**

- The GitHub wiki UI (with its wiki-style browsing) is gone. Readers now
  navigate docs via GitHub's file browser or a local clone.
- The `[[PageName]]` wiki-style markup had to be converted to standard
  relative markdown links (one-time, done).
- Strict link-over-paraphrase requires more discipline when writing prose:
  authors must identify the source of truth before stating any fact.
- ADR immutability means that a stylistic or typo fix to an accepted ADR
  now requires a whole new ADR. In practice this is rare and is an
  acceptable cost for the honesty it preserves.

**Mitigated risks.**

- "What if a docs file references a file that gets renamed?" — the
  `/wiki-audit` skill grep-verifies every file path in docs against the
  filesystem.
- "What if the wiki URL is bookmarked externally?" — the wiki repo is fully
  emptied. GitHub will show an empty wiki. External links 404; acceptable
  cost for preventing drift.

## Supersession history

None yet. This ADR is the first born under the new regime.
