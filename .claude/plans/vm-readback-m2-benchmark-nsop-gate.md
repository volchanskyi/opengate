# M2 — benchmark ns/op window-baseline hard gate

**Objective:** Promote `benchmark_ns_op` from **advisory-only** (committed baseline,
150% tol, never reds) to a **hard-red** regression gate using a **VM window baseline**
(median over 14d + a frozen, calibrated relative band) **AND** an absolute ceiling.
Keep `allocs_op` / `bytes_op` on the deterministic committed-baseline ±2% gate. Fold
into [`benchmark.yml`](../../.github/workflows/benchmark.yml)'s existing red+Telegram
publish job. Amend the CI-Pipeline ns/op doctrine.

**Dependencies:** M0 (`vm-readback-m0-shared-vm-query-lib.md` — uses `vm_query_window`).
**Ordering:** land **after/with** `vm-readback-criterion-html-retire.md` (shared file
`benchmark.yml`).

## Context

[`scripts/benchmark-summarize.sh`](../../scripts/benchmark-summarize.sh) today has
`ns_advisories` (ns/op > `baseline*(1+1.50)` ⇒ advisory line, never reds) and
`hard_regressions` (allocs/bytes > `baseline*(1+0.02)` ⇒ red), both against the
committed [`benchmarks/baseline.json`](../../benchmarks/baseline.json). ns/op is
**machine-dependent (noisy)** — the repo already learned this twice (the 150% advisory
tol; the k6 relay 112–136ms runner→OCI RTT-jitter comment). Copying mutation's
"prev-sample ± fixed pp" onto ns/op ⇒ a flapping red gate. So the **mechanism** is
mutation's (push → Telegram → hard fail-red) but the **baseline** is noise-robust:
**A — `median_over_time(ns[14d])` + a frozen variance-derived tol + an absolute ceiling**.

## Calibration — empirical, not guessed (do FIRST; ship numbers here)

Query the live 14–30d VM series for each benchmark's `ns_op` (via M0's `vm_query_window`
transport), compute run-to-run CV = σ/μ, and set `tol = k × CV` so the band sits just
outside historical no-change noise. Freeze per benchmark. Record the derived numbers in
this table **before** coding the band (per locked decision 5 — numbers live in the plan,
not in code comments):

| benchmark | lang | 30d median ns | CV | frozen rel tol | abs ceiling |
|---|---|---|---|---|---|
| _(codec_bench/…)_ | rust | TBD | TBD | TBD | TBD |
| _(Benchmark…)_ | go | TBD | TBD | TBD | TBD |

## Two-rule structure (mirrors mutation's `drop OR floor`)

Regress if **either**: current `ns_op` > `median*(1+tol)` (relative), **or** current
> `abs_ceiling` (absolute — the boiling-frog backstop, since a drifting window would
never fire on a slow regression alone). Cold-start (< N window samples) ⇒ absolute-only,
never red on missing history. Fail-open on VM/infra failure ⇒ absolute-only (or skip),
never red on infra.

## File inventory

- **Modify** [`scripts/benchmark-summarize.sh`](../../scripts/benchmark-summarize.sh) —
  replace `ns_advisories` with a VM-window ns/op regression: source `lib/vm-query.sh`
  (M0), fetch the median window per benchmark, apply the two-rule, emit `REGRESSION_ALERT`
  on an ns regression, and fold ns regressions into the same `regressed` return so the
  workflow's existing exit-1 path reds. Keep `hard_regressions` (allocs/bytes committed
  baseline) **unchanged**. The frozen tol/ceiling are named constants whose *values* come
  from the calibration table above.
- **Modify** [`.github/workflows/benchmark.yml`](../../.github/workflows/benchmark.yml) —
  **move the "OCI + kubeconfig setup" step (currently ~line 204, after Summarize) to
  before "Summarize and check regression"** (summarize now reads VM). Broaden the
  "Fail workflow red on regression" + Telegram wording from "allocation regression" to
  include ns/op. (Telegram + fail-red steps already exist — no new job.)
- **Modify** [`scripts/tests/benchmark-summarize.test.sh`](../../scripts/tests/benchmark-summarize.test.sh)
  — mock `kubectl` returning a canned window response (the `pmat-vm-query.test.sh`
  pattern): seed a > tol ns/op ⇒ `regression=1` + alert; sub-tol ⇒ 0; cold-start (empty
  window) ⇒ absolute-only, no red on infra; allocs/bytes gate unchanged.
- **Modify** [`docs/CI-Pipeline.md`](../../docs/CI-Pipeline.md) — amend the "ns/op
  advisory-only" doctrine to "ns/op hard-gated via VM window baseline + absolute ceiling;
  allocs/bytes committed-baseline ±2%".

## Steps (TDD-first)

1. **Calibrate** (above); fill the table.
2. **Test first:** extend `benchmark-summarize.test.sh` (seeded regression / sub-tol /
   cold-start / allocs-bytes-unchanged) → red.
3. Rewrite ns handling in `benchmark-summarize.sh` (window fetch + two-rule + alert).
4. Reorder `benchmark.yml` (OCI before summarize) + broaden fail-red/Telegram wording.
5. Amend `CI-Pipeline.md`.

## Gotchas / constraints

- **OCI+kube setup MUST precede summarize** now (currently after) — summarize reads VM.
- Keep the **allocs/bytes deterministic committed-baseline gate untouched**.
- **Fail-open** — VM timeout/empty ⇒ absolute-only, never exit-2/red on infra.
- **Exclude current commit** (M0 handles) — a re-run must not pull its own sample into the median.
- **Window (14d) < VM retention (30d)** — clamp/validate so an over-long window doesn't
  silently use less data.
- **Runner-generation step-change** (GitHub swaps hardware) false-fires the first few runs;
  the wide band + Telegram soften it — accept as residual risk, note it.

## Reviewer checklist

- [ ] ns/op reds on seeded > `median*(1+tol)` or > ceiling; silent sub-tol (tests both).
- [ ] Cold-start / empty window ⇒ absolute-only, never red on infra (fail-open, not exit-2).
- [ ] allocs/bytes committed-baseline ±2% gate unchanged.
- [ ] OCI+kube setup moved before summarize; fail-red + Telegram wording covers ns/op.
- [ ] Calibration numbers recorded in this plan (not code comments); frozen constants match.
- [ ] `/precommit` green; `CI-Pipeline.md` doctrine amended.

## Verification

`make shell-test` (`benchmark-summarize.test.sh`). Dry-run the publish job with a stubbed
VM window (`regression=1` + alert) and a forced-empty VM (fail-open, no red). `/docs`:
[`CI-Pipeline.md`](../../docs/CI-Pipeline.md).
