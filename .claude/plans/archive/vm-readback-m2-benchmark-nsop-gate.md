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

## Calibration — empirical, not guessed (DONE; numbers frozen here)

Queried the **live VM** `benchmark_ns_op` series (2026-07-02, ~40d export, n≈15
nightly samples per series over 18 series) via M0's transport, computed run-to-run
CV = σ/μ and the worst historical excursion `max/median` per series (the largest
no-change spike a hard gate must clear). Full per-series numbers:

| lang | series | n | median ns | CV | max/median |
|---|---|---|---|---|---|
| go | BenchmarkQUICHandshake_Cold | 15 | 1 921 517 | 3.40% | 1.059 |
| go | BenchmarkQUICHandshake_Resumed | 15 | 1 445 489 | 2.99% | 1.074 |
| go | BenchmarkManager_SignServer | 15 | 157 067 | 3.50% | 1.071 |
| go | BenchmarkManager_SignAgent | 15 | 156 337 | 3.73% | 1.081 |
| go | BenchmarkNewManager_Generate | 15 | 307 093 | 7.03% | 1.154 |
| go | BenchmarkNewManager_Load | 15 | 48 719 | 7.98% | 1.081 |
| go | BenchmarkHandshaker_PerformHandshake | 15 | 16 066 | 3.89% | 1.106 |
| go | BenchmarkCodec_EncodeControl | 15 | 2 670 | 8.08% | 1.280 |
| go | BenchmarkCodec_DecodeControl | 15 | 1 307 | 5.94% | 1.119 |
| go | BenchmarkCodec_ReadFrame | 15 | 291.9 | 9.99% | 1.201 |
| go | BenchmarkCodec_WriteFrame | 15 | 33.9 | 4.43% | 1.170 |
| go | BenchmarkEncodeServerHello | 15 | 7.5 | 7.68% | 1.080 |
| go | BenchmarkDecodeServerHello | 15 | 10.3 | 9.61% | 1.132 |
| rust | frame_encode_control | 15 | 302.6 | 2.90% | 1.041 |
| rust | frame_decode_control | 15 | 744.4 | 5.77% | 1.039 |
| rust | encode_server_hello | 15 | 23.6 | 6.57% | 1.124 |
| rust | decode_server_hello | 15 | 16.2 | 12.40% | 1.200 |
| rust | frame_encode_ping | 15 | 24.1 | 7.89% | 1.152 |

**Aggregate:** go median CV 5.94% / max 9.99%; rust median CV 6.57% / max 12.40%.
Worst single no-change excursion above the median across all 18 series = **+28.0%**
(go/Codec_EncodeControl), rust worst +20.0%.

**Frozen constants** (one band for both languages — their noise profiles coincide,
and 18 per-benchmark bands is over-fitting at n≈15):

| constant | value | derivation |
|---|---|---|
| `NS_REL_TOL` | **0.50** (50%) | ≈ 4× the median CV, ≈ 1.8× the worst observed no-change excursion (+28%). Clears runner jitter + heavy-tail + one runner-generation step change, yet reds any real ≥50% ns/op blow-up. Replaces the old never-firing 150% advisory. |
| `NS_ABS_CEIL_TOL` | **1.0** (ceiling = committed-baseline ns × 2.0) | The committed [`baseline.json`](../../benchmarks/baseline.json) ns_op is the frozen, drift-proof anchor (updated only via the deliberate `--update-baseline` dispatch), so it — not the self-updating window median — is the boiling-frog backstop. A sustained 2× over a reviewed baseline is unambiguous vs. the 28%-max noise; per-benchmark ceilings come free from the existing baseline (no hand-coded ns magic numbers that rot). |
| `NS_WINDOW_DAYS` | **14** | < 30d VM retention (decision 10); ~14 nightly samples. |
| `NS_MIN_WINDOW_SAMPLES` | **3** | Cold-start floor: < 3 window samples ⇒ relative rule skipped, absolute-only. |

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
