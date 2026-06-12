# DD-B — Deterministic Markdown link enforcement (hook)

**Parent:** [`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md) (Workstream B).
**Execution order:** **2nd** (after DD-A) — so DD-C/DD-D are enforced as they land.
**Status:** Implemented.
**Risk:** Low. Internal-only, zero-network, must stay <1.5s per the hooks posture.

## Objective

A hook + gauntlet check that verifies every `](relative/path…)` in `docs/**`,
`.claude/**`, and ADRs resolves (file **and** `#Lnn`/anchor), and flags links into
**non-archived** `.claude/plans/`. This makes "high-density clickable linking" and
"only archived plan links" deterministically enforced.

## File inventory

| Path | Action |
|---|---|
| `scripts/check-doc-links/` (Go package) | **New.** Walk `docs/**`, `.claude/**`; for each markdown link, verify target file + anchor exists; flag links to non-archived `.claude/plans/`. No network. |
| `scripts/tests/check-doc-links*.test.*` + fixtures | **New.** TDD fixtures: ok-link, broken-file, broken-anchor, active-plan-link (blocked), archived-plan-link (allowed). |
| [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) | Wire the checker into the gauntlet. |
| [`.claude/settings.json`](../settings.json) | Register a `PreToolUse` doc-write hook invoking the checker on `Write`/`Edit` of `*.md`. |

## Design constraints (verified posture)

- **<1.5s, zero-network, no-bypass** — matches the existing
  [hooks](../hooks/). Go-native (compiled, fast) preferred over Node
  `markdown-link-check` (adds a Node boundary + network).
- Anchor checking: support `#Lnn` (line refs into code) and `#heading-slug`
  (markdown headings). For code `#Lnn`, verify the file has ≥ nn lines.
- Plan-link rule: a link matching `](…/.claude/plans/<name>.md)` is allowed
  **only** when `<name>` is under `plans/archive/` — mirror DD-A's hook rule.

## Steps (gauntlet green per commit)

1. **Test-first:** write the fixtures + the test harness (failing).
2. Implement the checker; make fixtures pass.
3. Run it across the real `docs/**` + `.claude/**`; **expect existing broken
   anchors/links** — record them as DD-C input (do **not** mass-fix here; DD-C
   owns content). If the gauntlet wire-in would fail on pre-existing breakage,
   gate the wire-in commit on a clean baseline or fix the trivially-broken ones
   in this commit and hand the rest to DD-C.
4. Wire into `precommit-gauntlet.sh` + register the `PreToolUse` hook.
5. Time it (`time scripts/check-doc-links…`) — confirm <1.5s.
6. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [x] Checker is internal-only, zero-network, <1.5s on the full tree.
- [x] Fixtures cover ok/broken-file/broken-anchor/active-plan/archived-plan.
- [x] Wired into the gauntlet **and** registered as a `PreToolUse` md-write hook.
- [x] Existing link debt is captured by a count-aware fingerprint baseline for DD-C cleanup.
- [x] Full `/precommit` gauntlet green.

## Done when

Writing a markdown file with a broken link or an active-plan link is blocked
deterministically, and the gauntlet enforces the same across the tree.
