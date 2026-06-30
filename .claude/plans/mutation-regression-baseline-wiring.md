# Mutation regression — wire the baseline so drop-detection actually fires in CI

**Objective:** Make the scheduled mutation workflow's **"drop > 2pp from previous run"** rule
function in CI. Today only the absolute **< 85% floor** ever fires, because the baseline the
rule compares against is never populated in the CI job. Restore early detection of *gradual*
erosion before it reaches the floor.

**Dependencies:** none. **Related:** the web 85.7% → 84.3% regression (run 28355261715) that
only tripped because it crossed the floor; a working drop-rule would have flagged it a run
earlier.

## Context

The regression check has two rules ([`scripts/mutation-summarize.sh`](../../scripts/mutation-summarize.sh)):
`regressed(c; p) = (c < floor) or (p != null and (p - c) > drop)` — floor 85.0, drop 2.0pp.

`previous_row()` reads `HISTORY_FILE` (default `docs/mutation-history.jsonl`). But the workflow
runs the script with **`APPEND` unset and no restore step**, and that file is **not committed**
(["trend data lives in Loki"](../../.github/workflows/mutation.yml) — the Summarize step
comment). So `previous_row` is **always `null`**, the `p != null` guard is always false, and the
**entire drop-rule is dead in CI**. The only thing that can fail the workflow is a language
dropping below the static 85% floor.

Consequence: a slide like 92% → 89% → 86% → 84.9% is invisible until the *last* step crosses
the floor — which is exactly how web slid 85.7 → 84.3 in one day with no prior warning.

The durable trend store already has the data: [`scripts/mutation-vm-push.sh`](../../scripts/mutation-vm-push.sh)
pushes `mutation_score{commit,env,language}` (and `mutation_killed/survived/total/...`) to
VictoriaMetrics on every run via [`scripts/lib/vm-push.sh`](../../scripts/lib/vm-push.sh).

## Approach (recommended)

**Fetch the previous baseline from VictoriaMetrics before summarize.** VM is the single source
of truth already written each run; no committing from CI (which would fight the merge-to-main
policy), and it survives across runners (unlike an Actions cache).

A new `scripts/mutation-baseline-fetch.sh`:
1. Instant-query VM for the latest `mutation_score{env="ci"}` per `language` *excluding the
   current commit* (so a re-run doesn't compare against itself).
2. Reconstruct a single canonical-row JSON (`{scores:{rust:{score_pct},go:{...},web:{...}}}`)
   and write it as a one-line `HISTORY_FILE`.
3. The Summarize step exports `HISTORY_FILE=<that file>` so `previous_row` returns it and the
   drop-rule engages unchanged.

Rejected alternatives: committing `docs/mutation-history.jsonl` from CI (conflicts with
protected `main` / merge-to-main); Actions cache (per-branch, evictable, races the schedule).

## File inventory

- **Create:** `scripts/mutation-baseline-fetch.sh` — VM instant-query → previous canonical row;
  mirrors the metric names/labels emitted by `mutation-vm-push.sh`; reuses a `vm_query` helper.
- **Modify:** [`scripts/lib/vm-push.sh`](../../scripts/lib/vm-push.sh) — add a `vm_query`
  companion to the existing `vm_push` (same base URL / auth env), or a sibling `lib/vm-query.sh`.
- **Modify:** [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) — add a
  **"Restore baseline from VM"** step before Summarize that runs the fetch script and exports
  `HISTORY_FILE`; the Summarize step already consumes it.
- **Modify (minor, same function):** [`scripts/mutation-summarize.sh`](../../scripts/mutation-summarize.sh)
  — the alert text hardcodes `"regression on dev"`; derive the branch from `GITHUB_REF_NAME`
  (the failing run was the scheduled **main** run, mislabeled "dev").

## Steps (TDD-first)

1. **Test first** (shell, `make shell-test`): `mutation-baseline-fetch.sh` parses a captured
   VM query response into a valid canonical-row JSON; on **empty/error response it emits
   nothing and exits 0** (fail-open). Then write the fetch script.
2. **Test first:** extend the summarize regression tests with a **non-null previous** row where
   web drops 2.1pp → `regression=1` with `"(drop > 2pp)"`; and a <2pp drop → `regression=0`.
   (Covers the path that is currently never exercised in CI.)
3. **Test first:** branch-label test → derive the alert branch from `GITHUB_REF_NAME`.
4. Wire the workflow step; keep Summarize's exit-2 (incomplete) vs exit-1 (regression) split.

## Gotchas / constraints

- **Fail-open, never fail-closed on infra.** A VM query timeout / empty series must degrade to
  the **floor-only** behavior (today's behavior), i.e. baseline absent → `previous_row` null.
  It must **not** raise exit 2 (incomplete run) or block the workflow — a flaky metrics backend
  is not a score regression.
- **Exclude the current commit** from the baseline query, or a workflow re-run compares against
  itself (delta 0, never a drop).
- **Per-language**, matching the three canonical languages; a missing language in VM → null
  prev for that language only (floor still applies).
- Do **not** touch `REGRESSION_FLOOR_PCT` (85.0) or `REGRESSION_DROP_PP` (2.0) here — threshold
  tuning is a separate decision.

## Reviewer checklist

- [ ] Drop-rule fires on a seeded >2pp baseline drop; silent on <2pp (tests prove both).
- [ ] Baseline query failure / empty → floor-only, exit 0 (fail-open); no exit-2 on infra error.
- [ ] Current commit excluded from the baseline query.
- [ ] Alert branch label derived from `GITHUB_REF_NAME`, not hardcoded.
- [ ] No change to floor/drop thresholds; `make shell-quality` green; `/precommit` green.

## Verification

`make shell-test` (fetch-parse + fail-open + regression-with-baseline cases). Dry-run the
workflow step on a branch with a stubbed VM response asserting `regression=1`/`"(drop > 2pp)"`.
Confirm a forced empty VM response yields floor-only behavior. `/docs`: mutation-testing page.
