# VM read-back regression gates — generalize the pattern across mutation, benchmark, load-test

> **Status: READY (design locked) — decomposed into micro-plans.** Master plan +
> micro-plan index. The per-item execution specs are the sibling `vm-readback-*.md`
> files in this directory (M0–M3 + the Criterion-HTML retirement). **Consolidated**
> the standalone `mutation-regression-baseline-wiring` plan into
> `vm-readback-m1-mutation-drop-gate.md` (that file is deleted). Per the doc-link
> rule, this plan references **other plans** by plain path/code span (never markdown
> links) — only repo source/docs are linked.

## Context

Four CI trend families push numeric series to VictoriaMetrics every nightly run
through the shared [`scripts/lib/vm-push.sh`](../../scripts/lib/vm-push.sh)
transport (Benchmark Trends B1–B5, [ADR-038](../../docs/adr/ADR-038-victoriametrics-ci-trend-store.md)).
But only two of them *read the trend back* to detect regression, and coverage is
uneven:

| Family | Metrics | Gate today | Baseline source |
|---|---|---|---|
| **PMAT** | `pmat_repo_score`, `pmat_below_bplus` | drop >2pp OR <75% floor — **works** | **VM read-back** ([`pmat-vm-query.sh`](../../scripts/pmat-vm-query.sh)) |
| **Mutation** | `mutation_score{language}` | floor 85% works, **drop >2pp DEAD** | `HISTORY_FILE` never populated in CI |
| **Benchmark** | `benchmark_{ns,allocs,bytes}_op{benchmark,lang}` | allocs/bytes ±2% vs committed baseline → red; **ns/op advisory-only** (150% tol) | committed `benchmarks/baseline.json` |
| **Load-test** | `loadtest_latency_{p50,p95,p99}_ms`, `loadtest_rps`, `loadtest_error_rate{scenario,phase,source}` | **NONE** — visibility-only (ADR-038) | none |

**PMAT already is the pattern** the mutation plan reinvents: read the previous
sample from VM, compare, alert + fail red. The generalization is to (1) extract
that read-back into a shared library, and (2) apply it to the three families that
don't yet gate their VM trends — mutation's dead drop-rule, benchmark's advisory
ns/op, and load-test's ungated latency/throughput/error-rate.

The prompt: apply the mutation gate mechanism uniformly. The gate mechanism is
fixed by the reference run
([mutation run 28355261715, job "Publish results + alert on regression"](https://github.com/volchanskyi/opengate/actions/runs/28355261715)):
**Push to VM (always) → Telegram alert on regression (non-failing) → a final
"Fail workflow red on regression" step (`if: always() && regression=='1'` →
exit 1)**. `benchmark.yml` already has this job; `load-test.yml` has none.

## Locked decisions

1. **Scope:** shared `scripts/lib/vm-query.sh` + all three families.
2. **Gate mechanism:** identical to mutation/benchmark publish job — VM push
   always; Telegram on regression; separate hard **workflow-red** step. Not
   alert-only. This *reverses* two prior decisions and needs ADR updates:
   - ADR-038 §Consequences "load-test trends remain visibility-only … only
     pass/fail gates" — supersede/amend for the new load-test regression gate.
   - `docs/CI-Pipeline.md` + benchmark baseline doctrine "ns/op advisory-only" —
     amend when ns/op becomes a hard gate.
3. **Baseline transport:** VM read-back (not committed files, not Actions cache).
   Committed `benchmarks/baseline.json` for allocs/bytes **stays** (deterministic).
4. **p99 latency:** gate p50/p95 hard; p99 is pushed + named in the Telegram alert
   but **never reds CI** (heavy-tailed — a single slow request moves it).
5. **Band calibration is empirical:** every noisy-metric tolerance is derived from
   measured live-VM run-to-run variance (below), never hand-guessed.
6. **Baseline statistic (noisy metrics):** **A — median over window + a frozen,
   variance-derived relative tolerance**. B (control chart) and C (quantile
   ceiling) rejected for our small nightly sample (n≈14); B kept as a documented
   future upgrade. Detailed trade-off below.

## The crux — baseline design for HARD-gating noisy metrics

Hard-red is safe for **deterministic** metrics (mutation score, allocs/op,
bytes/op, pmat score) because the same code yields the same number: previous
single sample + a small relative/absolute threshold never false-fires.

Hard-red is **dangerous** for **machine-dependent** metrics because run-to-run
variance on shared GitHub runners can exceed any fixed threshold with zero code
change. The repo already learned this twice: ns/op is advisory at 150% tolerance,
and the k6 relay threshold carries a comment about 112–136ms runner→OCI RTT
jitter. Copying mutation's "prev-sample ± fixed pp" onto p95/p99/ns_op ⇒ a
flapping red gate ⇒ alert fatigue ⇒ the gate gets ignored. So the *mechanism* is
mutation's, but the *baseline* must be noise-robust.

### Metric taxonomy (drives per-metric baseline choice)

| Metric | Bad direction | Noise class | Proposed baseline |
|---|---|---|---|
| mutation_score, pmat_repo_score | ↓ lower | deterministic | prev sample; rel drop + abs floor |
| benchmark_allocs_op / bytes_op | ↑ higher | deterministic | **keep** committed baseline ±2% |
| loadtest_error_rate | ↑ higher | near-deterministic | abs ceiling (+ prev-sample rel) |
| benchmark_ns_op | ↑ higher | **noisy** | window statistic + wide rel band + abs ceiling |
| loadtest_latency_p50/p95 | ↑ higher | **noisy** | window statistic + wide rel band + abs ceiling |
| loadtest_latency_p99 | ↑ higher | **very noisy (heavy tail)** | window statistic, widest band — or gate p95 only, p99 alert-only *(open)* |
| loadtest_rps | ↓ lower | **noisy** | window statistic + wide rel band |

### Two-rule structure (mirrors mutation's `drop OR floor`)

For every gated metric, regress if **either**:
- **Relative:** current is worse than `baseline * (1 ± tol)` (direction-aware), where
  `baseline` = the window statistic below (deterministic metrics use prev sample).
- **Absolute:** current crosses a hard floor/ceiling (mutation's 85% floor
  analog). This is the **boiling-frog backstop**: a window baseline drifts with a
  slow regression and would never fire on its own, so an absolute bound must
  co-exist. (For latency the k6 SLA numbers are natural ceilings.)

### Baseline statistic for noisy metrics — detailed trade-off

`pmat-vm-query.sh` reads the **newest single sample** (`/api/v1/export` → jq
max_by timestamp) — fine for deterministic metrics, wrong for noisy ones. The
noisy-metric baseline needs a new "aggregate over window" mode (`/api/v1/query`
PromQL). **What shapes the choice:** nightly cadence ⇒ only ~14 samples in a 14d
window (~30 at the 30d retention cap); latency is right-skewed/spiky; the gate is
hard-red (a false positive breaks CI); and band width is calibrated **empirically**
(locked) — variance is measured from real history whichever statistic we pick.

**A. Median + calibrated relative tol** — `median_over_time(m[14d])`; regress if
`current` worse than `median*(1±tol)`.
- *Pros:* robust **center** (median ignores single-run spikes; the mean does not);
  stable at n≈14 (no variance estimate needed); simplest to implement and the most
  explainable alert ("p95 142ms vs 14d median 100ms, +42% > 30% band"); tol is set
  per-metric from measured CV, so it still adapts to each metric's noise.
- *Cons:* tol is a frozen constant (won't self-adjust if a metric's noise changes
  until recalibration); a symmetric band is slightly mis-centered on skewed data
  (fires marginally early); drift raises the median ⇒ leans on the absolute
  backstop.

**B. Control chart (mean + k·σ)** — `avg_over_time + k*stddev_over_time`, k≈3.
- *Pros:* self-calibrating **width** in units of each metric's own σ with one
  global k; a chosen k maps to a chosen false-alarm rate — *if* data were normal.
- *Cons (decisive here):* σ is **unstable at n≈14** — a calm fortnight shrinks σ,
  collapsing the band to a hair-trigger that reds on the next normal jitter (SPC
  wants ≥20–30 points); the **mean is spike-sensitive** (one bad run inflates both
  center and width); latency is **non-normal**, so the clean k↔false-alarm math
  fails and you calibrate k empirically anyway; dodging the σ-collapse needs a
  σ-floor knob, eroding the "no tuning" pitch.

**C. Quantile ceiling** — `quantile_over_time(0.9, m[14d])`; regress if worse.
- *Pros:* intuitive "worse than 9 of the last 10 runs"; outlier-robust **width**
  via order statistics (no σ); no distributional assumption.
- *Cons:* **coarse at n≈14** — p90-of-14 ≈ the 2nd-largest point, itself jumpy;
  by construction ~10% of *normal* runs exceed it, so you re-add a margin
  (`>p90*(1+tol)`) → collapses toward A; absorbs slow drift fastest (leans hardest
  on the backstop).

**Recommendation — A (median + variance-derived, frozen tol).** On our data B's
edge evaporates (unstable small-sample σ, spike-sensitive mean, non-normal tails)
and C is too coarse at n≈14. The **locked empirical-calibration step gives A most
of B's benefit without the cost:** measure the robust spread (CV / relative MAD)
over the full 30d **once, offline**, and freeze `tol = k × spread` per metric —
"variance-derived width" (B's goal) computed on the largest sample at calibration
time, not re-estimated every run on 14 points (B's failure mode). A MAD-based
robust control chart is the textbook best-of-both, but VM has no native MAD
(nested-query only) — another point for A operationally. Keep B documented as a
future upgrade **iff** we widen the window toward 30d and find the frozen tol too
static.

### Tolerance / band calibration — empirical, not guessed (locked, decision 5)

Do **not** hardcode 20%/30%/50%. Add an implementation step that **queries the
live 14–30d VM series** for each noisy metric and computes the observed
run-to-run coefficient of variation (CV = σ/μ), then sets k (or tol) so the band
sits just outside historical no-change noise. (~30d of nightly data already
exists in VM under ADR-038's retention.) Ship the derived numbers in the plan,
not in code comments.

## Edge cases / unexpected use cases to cover

1. **Cold start / thin history** — <N samples in window (new metric, new k6
   scenario, wired `edge_sentinel_bench`, post-retention-wipe). ⇒ skip the
   relative rule, fall back to absolute-only; **never** red on missing history
   (mutation's `prev==null → floor-only`).
2. **Re-runs / same-commit dup** — a manual re-run adds a 2nd sample for the same
   commit; including it biases the window toward "no change." ⇒ **exclude current
   commit** from every baseline query (window and prev-sample alike).
3. **VM/infra flakiness** — query timeout, empty series, kubectl pod failure. ⇒
   **fail-open** to absolute-only (or skip); never exit-2/red on infra.
   `pmat-vm-query.sh` already returns empty on failure — preserve that contract.
4. **Multi-dimensional series (load-test)** — metrics are per
   `{scenario,phase,source}`; a single scalar baseline is wrong. ⇒ group-by and
   compare **each series** independently; per-series cold-start handling.
5. **Metric direction** — latency/ns_op/error_rate: higher = worse; rps/score:
   lower = worse. The lib + gate must take a **direction** param.
6. **Heavy-tailed p99** — a single slow request moves p99; hard-gating it flaps.
   ⇒ decide: widest band, or gate p95 and keep p99 alert/visibility (open fork).
7. **Runner-generation / regime change** — GitHub swaps runner hardware ⇒ step
   change in ns_op/latency that isn't a code regression. Window self-heals after
   N runs but false-fires the first few. ⇒ wide bands + the Telegram alert soften
   it; accept as residual risk, document.
8. **Unit/definition drift** — changing a metric's computation (ms↔s, k6 trend
   stat) mixes populations in the window ⇒ false regression. ⇒ on definition
   change, treat as a reset (rename / note); low frequency.
9. **Null / missing values** — benchmark that didn't run, error_rate with 0
   requests, aborted load run. ⇒ summarize emits `null` (not 0); gate skips nulls
   per-series (mutation already does per-language nulls).
10. **Window vs retention** — window (14d) must stay < VM retention (30d); clamp/
    validate so an over-long window doesn't silently use less data.

## Shared library — `scripts/lib/vm-query.sh` (M0)

Generalize `pmat-vm-query.sh` into a reusable read-back helper; repoint PMAT at
it (behavior-preserving). Proposed surface:

- `vm_query_latest <metric> <selector>` — newest single sample (current PMAT
  behavior via `/api/v1/export`), current commit excluded.
- `vm_query_window <promql>` — scalar/vector from `/api/v1/query` for the window
  statistic (median/avg+σ/quantile). Returns per-series values keyed by labels
  for the multi-dimensional load-test case.
- Same `VM_NAMESPACE/VM_SERVICE/VM_CURL_IMAGE` env + kubectl curl-pod transport.
- **Fail-open contract:** any transport/parse failure ⇒ empty output, exit 0.
- Shared behavioral test in `scripts/tests/` (mock kubectl, like
  `vm-transport.test.sh` / `pmat-vm-query.test.sh`).

## Micro-plans — execution specs (sibling files)

Each item is a **self-contained micro-plan** (objective, file inventory, TDD steps,
gotchas, reviewer checklist, verification) in its own sibling file, TDD-first (shell
tests before script logic). The shared design above — baseline statistic **A**, the
two-rule structure, empirical calibration, the edge-case taxonomy — is the SSOT; the
micro-plans reference it, and M2/M3 each carry a **calibration table to fill from
live VM before coding the band**.

| Micro-plan | File | Summary |
|---|---|---|
| **HTML retire** | `vm-readback-criterion-html-retire.md` | drop Criterion `html_reports` (data-only artifacts); independent, land **before/with M2** (shared `benchmark.yml`) |
| **M0** | `vm-readback-m0-shared-vm-query-lib.md` | extract `lib/vm-query.sh` (`vm_query_latest` + `vm_query_window`); repoint PMAT behavior-preserving. **Foundation for M1–M3** |
| **M1** | `vm-readback-m1-mutation-drop-gate.md` | fetch per-language baseline from VM ⇒ engage the dead `drop > 2pp` rule; branch label from `GITHUB_REF_NAME` |
| **M2** | `vm-readback-m2-benchmark-nsop-gate.md` | ns/op hard gate via VM window median + calibrated band + abs ceiling; keep allocs/bytes committed-baseline; amend CI-Pipeline doctrine |
| **M3** | `vm-readback-m3-loadtest-gate.md` | new publish job; per-`{scenario,phase,source}` window gate on latency/rps + abs ceiling on error_rate; p99 alert-only; supersede ADR-038 visibility-only clause |

## Related cleanup — retire benchmark HTML reports (independent of M0–M3)

Now its own micro-plan `vm-readback-criterion-html-retire.md` (land **before/with
M2** — shared file `benchmark.yml`). Rationale: benchmark trends live in VM
(ADR-038), so Criterion's HTML is dead weight — the summarizer only reads
`new/estimates.json`. `html_reports` is a *default* feature, so retiring it needs
`default-features = false` + re-adding `cargo_bench_support`. See the micro-plan for
the file inventory and steps.

## Execution order

0. **Done:** `mutation-regression-baseline-wiring.md` deleted (folded into
   `vm-readback-m1-mutation-drop-gate.md`).
1. **`vm-readback-criterion-html-retire.md`** (independent; leaves a clean
   `benchmark.yml` for M2).
2. **M0** — `vm-readback-m0-shared-vm-query-lib.md` (foundation for M1–M3).
3. **M1–M3** — sequential, or parallel micro-plans to other engineers after M0.

## Open decisions

- **Sequencing only:** M1–M3 sequential vs parallel micro-plans post-M0. Every
  design decision — scope, gate mechanism, transport, baseline statistic
  (**A: median + frozen tol**), empirical calibration, p95-hard/p99-alert — is
  locked.

## Verification (per family, at implementation)

`make shell-test` for each new/changed script (parse, fail-open, cold-start,
per-series, direction, seeded-regression fires / sub-threshold silent). Dry-run
each workflow publish job on a branch with a stubbed VM response asserting
`regression=1` + the alert text, and a forced-empty VM response asserting
fail-open (no red). `/docs`: Monitoring.md, CI-Pipeline.md, and the ADR-038 +
ns/op-doctrine amendments. `/precommit` green.
