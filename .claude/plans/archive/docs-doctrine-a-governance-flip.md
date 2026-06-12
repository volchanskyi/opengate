# DD-A — ADR mutability governance flip (gating)

**Parent:** `current-state-docs-doctrine-and-adr-mutability.md` (Workstream A).
**Execution order:** **1st** — gates every other DD micro-plan **and** unblocks
teardown `dormant-scale-out-td6-docs-adrs.md`.
**Status:** Executed. **ADR consolidation folded in** (maintainer request, enabled
by the mutability flip): former amendment ADRs absorbed into their parents'
new "Amendments" sections — ADR-028 + ADR-032 → ADR-019, ADR-026 → ADR-020,
ADR-031 + ADR-033 → ADR-023 — the five files deleted, `decisions.md` rows dropped
with a consolidation map, and every cross-reference (docs/ADRs/code/config/plans)
repointed to the parent. One coherent commit.
**Risk:** Governance reversal — mutable ADRs lose the immutable audit trail
(mitigation: ADR-036 supersession + `date:`/`status:` frontmatter + git history).

## Objective

Make per-file ADRs (013+) **mutable**, change the plan-link rule to allow only
**archived** plan links, and record the current-state doctrine — all hook-enforced.
After this commit, ADRs are editable and teardown TD6 can proceed.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| `docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md` | **New ADR.** Supersedes **only** ADR-013's immutability clause (#3 at [ADR-013:70-77](../../docs/adr/ADR-013-docs-in-repo-and-immutable-adrs.md#L70-L77)); docs-in-repo (#1) + wiki-deprecated (#2) **stand**. Records: per-file ADRs 013+ mutable; the combined log `Architecture-Decision-Records.md` (001–012) **frozen**; success = content quality, **net line delta is NOT a gate**; plan links allowed **only** to `plans/archive/…`. **Must not link any active plan** (the `adr-plan-link` hook still applies to new ADRs) — fold rationale inline or cite [`decisions.md`](../decisions.md). | ADR-036 free (latest = 035) |
| [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) | Remove the `adr-immutable` block (`:35-40`). Change `adr-plan-link` (`:42-47`) from "block all plan links in ADRs" to "block links to **non-archived** plans" (allow `](…/plans/archive/….md)`). | grep-confirmed lines |
| [`scripts/tests/hooks.test.sh`](../../scripts/tests/hooks.test.sh) | Flip the ADR-immutability cases to assert edits are now **allowed**; add archived-vs-active plan-link cases. (21 ADR-referencing lines today.) | exists |
| [`.claude/rules/plans-and-adrs.md`](../rules/plans-and-adrs.md) | Rewrite the ADR-immutability + adr-plan-link sections to the new regime. | rule file |
| [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md) | Rewrite the "ADRs are immutable" clause in the `/docs` section. | rule file |
| [`docs/README.md`](../../docs/README.md) §2 | Rewrite "ADRs are immutable" → 013+ mutable + cross-checked; supersession still used for genuine decision *changes*; 001–012 log **stays frozen**; keep the directory-layout note. | canonical doc |
| [`CLAUDE.md`](../../CLAUDE.md) | Update the `plans-and-adrs.md` row + any immutability phrasing. | index |
| `.claude/skills/wiki-audit/SKILL.md`, `.claude/skills/precommit/SKILL.md` | Update any text that cites ADR immutability. | grep before edit |
| [`.claude/decisions.md`](../decisions.md) | Add the ADR-036 index row. | index |

## Steps (single commit, gauntlet green)

1. **Test-first:** edit `hooks.test.sh` (flip immutability cases → allowed; add
   archived/active plan-link cases). This is the TDD-gate test change for the
   `.sh` source edit (`pretooluse-bash-source-write-guard.sh`).
2. Edit `pretooluse-write-guard.sh` (drop `adr-immutable`; refine `adr-plan-link`).
3. Run `scripts/tests/hooks.test.sh` — green (immutable-ADR edit now allowed,
   active-plan-link still blocked, archived-plan-link allowed).
4. Write `ADR-036` (inline rationale, no active-plan link).
5. Rewrite the two rules, `docs/README.md` §2, `CLAUDE.md` row, the two skills,
   and add the `decisions.md` row.
6. Sanity: attempt a trivial Edit to an existing ADR in a scratch test — the hook
   must now **allow** it (verifies the flip empirically).
7. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [ ] ADR-036 scopes supersession to ADR-013 clause #3 **only**; #1/#2 explicitly stand; 001–012 frozen; net-line-delta explicitly not-a-gate; no active-plan link in the ADR.
- [ ] Hook: `adr-immutable` gone; `adr-plan-link` allows `plans/archive/` and blocks active plans.
- [ ] `hooks.test.sh` green with the flipped + new cases; an ADR Edit is now allowed in practice.
- [ ] `docs/README.md` §2, both rules, `CLAUDE.md`, both skills, `decisions.md` all consistent with the new regime.
- [ ] Full `/precommit` gauntlet green.

## Done when

A manual Edit to an existing ADR is permitted by the hook, an active-plan link is
still blocked, and the doctrine + new regime are recorded in ADR-036 + the rules.
This commit unblocks teardown TD6 and DD-C's ADR-body work.
