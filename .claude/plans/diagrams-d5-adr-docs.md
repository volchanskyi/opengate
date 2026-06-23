# Micro-Plan D5: ADR + decisions / phases

**Parent master:** `diagrams-as-code-part-2.md` (§5 D5). **Branch:** `dev`.
**Owner:** docs. **Depends on:** D1–D4 landed (documents the landed state).

## 1. Goal

Record the Part-2 decisions as a durable, mutable ADR and reflect the landed state in the
project's source-of-truth indices.

## 2. Scope / file inventory

| File | Change |
|---|---|
| `docs/adr/ADR-0NN-diagrams-as-code-part-2.md` | **New ADR** (next sequential number after the highest existing): records (1) native Mermaid **C4 adoption** + the **GitHub render-verification gate** + fallback rationale (cite the experimental-C4 fragility); (2) **CI-only syntax validation** (no Puppeteer, pinned, version-aligned); (3) **drift-guard hardening** (all diagram docs pinned + nudge); (4) the **coverage standard**. Written as current-state (per the mutable-ADR doctrine). |
| [`.claude/decisions.md`](../decisions.md) | Add the index row for the new ADR. |
| [`.claude/phases.md`](../phases.md) | Add completed rows for D1–D4. |
| [`docs/README.md`](../../docs/README.md) | Ensure the convention section links the ADR (the durable rationale) rather than repeating it. |

## 3. Approach

1. Pick the next ADR number (after the current highest in `docs/adr/`).
2. Write the ADR as current-state (mutable); keep rationale **inline** (durable record);
   link only stable targets (other ADRs, code, external URLs, archived plans).
3. Add the `decisions.md` row + `phases.md` rows.
4. Update `docs/README.md` to link the ADR.
5. `/precommit` green (incl. the doc-link checker).

## 4. Acceptance criteria / DoD

- [ ] New ADR exists with the next sequential number, recording all four Part-2
      decisions + the C4 render-gate caveat.
- [ ] `decisions.md` row added; `phases.md` records D1–D4.
- [ ] README links the ADR (no paraphrased duplication).
- [ ] Doc-link checker + `/precommit` green.

## 5. Reviewer/QA checklist

- [ ] ADR follows the per-file **mutable** convention (the frozen 001–012 log untouched).
- [ ] The C4 render-gate + fallback rationale is captured (not just "adopted C4").
- [ ] No active-plan links in the ADR (only archived plans / stable targets).
- [ ] `decisions.md` + `phases.md` consistent with the landed work.

## 6. Notes

This ADR supersedes nothing — Part 1 (DD-E) remains valid; Part 2 **extends** it. Use a
new ADR (not an edit of the DD-E record) because these are new decisions, per the
mutable-ADR-for-corrections / new-ADR-for-changes rule.
