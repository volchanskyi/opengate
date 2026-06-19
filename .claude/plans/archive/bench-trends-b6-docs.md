# Micro-Plan B6: Docs — ADR + Register Updates for VM Trend Store

**Parent master:** `benchmarks-grafana-trends.md` (§9 B6). **Branch:** `dev`.
**Owner:** docs. **Depends on:** B2–B5 landed (documents the landed state).

## 1. Goal

Record the decision (VM as the canonical CI-trend store) and reflect the landed state in
the project's source-of-truth files.

## 2. Scope / file inventory

| File | Change |
|---|---|
| `docs/adr/ADR-0NN-victoriametrics-ci-trend-store.md` | **New ADR** (next sequential number): VM is the canonical CI-trend store; the VM-vs-Loki rationale (numeric series + PromQL regression primitives; Loki = logs only); 30-day retention; the shared `vm-push.sh` transport; gh-pages + Loki-push retired. Per-file ADRs are mutable — write it as current-state. |
| [`.claude/decisions.md`](../../decisions.md) | Add the index row for the new ADR. |
| [`.claude/phases.md`](../../phases.md) | Add completed rows for B1–B5. |
| [`.claude/techdebt.md`](../../techdebt.md) | **Pay down** the "Performance benchmarks — no CI regression detection" entry. |
| [`docs/Monitoring.md`](../../../docs/Monitoring.md) | Document the metric convention (cross-check with B1) + that VM now holds CI trends; Loki = logs. Follow [docs/README.md](../../../docs/README.md) — link, don't paraphrase numbers/paths. |

## 3. Approach

1. Write the ADR as current-state (mutable per the ADR doctrine); set the next number
   after the highest existing ADR.
2. Add the `decisions.md` row + `phases.md` rows; remove the paid-down techdebt entry.
3. Update `docs/Monitoring.md` (link to the convention/scripts; don't duplicate numbers).
4. `/precommit` green.

## 4. Acceptance criteria / DoD

- [x] New ADR exists with the next sequential number; `decisions.md` row added.
- [x] `phases.md` records B1–B5; the techdebt benchmark-regression entry is removed.
- [x] `docs/Monitoring.md` reflects VM = CI trends, Loki = logs, and links (not copies)
      the metric convention.
- [x] Doc-link checker + `/precommit` green.

## 5. Reviewer/QA checklist

- [x] ADR follows the per-file mutable-ADR convention (no edit to the frozen 001–012 log).
- [x] No paraphrased numbers/paths in `docs/Monitoring.md` (link to source).
- [x] techdebt entry actually removed (not just edited).
