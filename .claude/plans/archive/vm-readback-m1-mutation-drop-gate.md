# M1 — mutation drop-gate (wire the VM baseline so drop-detection fires in CI)

**Objective:** Make the scheduled mutation workflow's **"drop > 2pp from previous
run"** rule fire in CI. Today only the absolute **< 85% floor** ever fires; a working
drop-rule would have flagged the web **85.7% → 84.3%** slide
([run 28355261715](https://github.com/volchanskyi/opengate/actions/runs/28355261715))
a run earlier, before it crossed the floor. Restore early detection of *gradual* erosion.

**Dependencies:** M0 (`vm-readback-m0-shared-vm-query-lib.md` — uses `vm_query_latest`).
**Consolidates** the standalone `mutation-regression-baseline-wiring` plan (now deleted).

## Root cause

[`scripts/mutation-summarize.sh`](../../scripts/mutation-summarize.sh)'s
`regressed(c; p) = (c < floor) or (p != null and (p - c) > drop)` (floor 85.0, drop
2.0pp) reads `previous_row()` from `HISTORY_FILE` (default `docs/mutation-history.jsonl`).
But [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) runs
summarize with **`APPEND` unset and no restore step**, and that file is **not committed**
(ADR-017 dropped it; trend data lives in VM). So `previous_row` is **always null**, the
`p != null` guard is always false, and the **drop-rule is dead** — only the floor can
fail the run. A slide like 92 → 89 → 86 → 84.9 is invisible until the *last* step crosses
the floor.

## Approach

Fetch the previous per-language baseline from VM via M0's
`vm_query_latest mutation_score 'language="<lang>",env="ci"'` (deterministic metric ⇒
newest prior sample, current commit excluded — the labels/metric names are those
[`scripts/mutation-vm-push.sh`](../../scripts/mutation-vm-push.sh) emits), reconstruct a
one-line canonical `HISTORY_FILE`, and export it so `previous_row` returns it and the
drop-rule engages **unchanged**. No committing from CI (fights merge-to-main); survives
across runners (unlike an Actions cache). Rejected: committing `docs/mutation-history.jsonl`
from CI (protected `main`); Actions cache (per-branch, evictable, races the schedule).

## File inventory

- **Create** `scripts/mutation-baseline-fetch.sh` — thin adapter: for each canonical
  language (rust/go/web) call `vm_query_latest` (M0) and assemble the canonical-row JSON
  (`{scores:{rust:{score_pct},go:{score_pct},web:{score_pct}}}`), written as the one-line
  `HISTORY_FILE`. On empty/error input emit nothing and exit 0 (fail-open). (Replaces the
  old standalone plan's bespoke `vm_query` — query mechanics now live in `lib/vm-query.sh`.)
- **Modify** [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) —
  add a **"Restore baseline from VM"** step **before** Summarize that runs the fetch and
  exports `HISTORY_FILE`. **Move the "OCI + kubeconfig setup" step (`id: infra`, currently
  *after* Summarize) to before the Restore step** — the fetch needs `kubectl`. Keep the
  same `if: github.event_name == 'schedule' || …dispatch'` gate on both. Leave the exit-2
  (incomplete) vs exit-1 (regression) split and the existing Telegram + "Fail workflow red
  on regression" steps unchanged.
- **Modify** [`scripts/mutation-summarize.sh`](../../scripts/mutation-summarize.sh) —
  derive the alert branch from `GITHUB_REF_NAME` instead of the hardcoded
  `"regression on dev"` (the failing run was the scheduled **main** run, mislabeled "dev").
  No other logic change; do **not** touch `REGRESSION_FLOOR_PCT` (85.0) / `REGRESSION_DROP_PP` (2.0).

## Steps (TDD-first)

1. **Test first** (`make shell-test`) → then write `mutation-baseline-fetch.sh`: parses a
   captured `vm_query_latest` result per language into valid canonical-row JSON; on
   empty/error input emits nothing and exits 0 (fail-open).
2. **Test first:** extend the summarize regression tests with a **non-null previous** row:
   web drops 2.1pp ⇒ `regression=1` with `"(drop > 2pp)"`; <2pp drop ⇒ `regression=0`
   (this path is currently never exercised in CI).
3. **Test first:** branch-label derives from `GITHUB_REF_NAME`.
4. Wire the workflow "Restore baseline from VM" step + move OCI setup ahead of it.

## Gotchas / constraints

- **Fail-open, never fail-closed on infra.** A VM timeout / empty series degrades to the
  **floor-only** behavior (today's behavior), never exit-2 (incomplete run) or workflow-red.
  A flaky metrics backend is not a score regression.
- **Exclude the current commit** from the baseline query (M0 handles) — else a re-run
  compares against itself (delta 0, never a drop).
- **Per-language:** a language missing in VM ⇒ null prev for that language only (floor
  still applies).
- Do **not** touch `REGRESSION_FLOOR_PCT` / `REGRESSION_DROP_PP` — threshold tuning is a
  separate decision.

## Reviewer checklist

- [ ] Drop-rule fires on a seeded >2pp baseline drop; silent on <2pp (tests both).
- [ ] Baseline query failure/empty ⇒ floor-only, exit 0 (fail-open); no exit-2 on infra error.
- [ ] Current commit excluded from the baseline query.
- [ ] OCI+kube setup moved ahead of the Restore step; Restore runs before Summarize.
- [ ] Alert branch derived from `GITHUB_REF_NAME`, not hardcoded.
- [ ] No change to floor/drop thresholds; `make shell-quality` + `/precommit` green.

## Verification

`make shell-test` (fetch-parse + fail-open + regression-with-baseline + branch-label).
Dry-run the workflow "Restore baseline from VM" + Summarize on a branch with a stubbed VM
response asserting `regression=1` + `"(drop > 2pp)"`, and a forced-empty VM response
asserting floor-only (no red). `/docs`: mutation-testing section of
[`Testing.md`](../../docs/Testing.md).
