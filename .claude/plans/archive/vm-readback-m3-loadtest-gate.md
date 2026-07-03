# M3 — load-test regression gate (latency / rps / error_rate, per-series)

**Objective:** Add the first regression gate to load-test trends — today
**visibility-only** ([ADR-038](../../docs/adr/ADR-038-victoriametrics-ci-trend-store.md)).
A new **publish job** in [`load-test.yml`](../../.github/workflows/load-test.yml)
mirroring mutation/benchmark (push to VM **always** → Telegram on regression → a hard
**fail-red** step). Window-baseline gate on `latency_p50/p95` + `rps` + an **absolute
ceiling on `error_rate`**, evaluated **per `{scenario,phase,source}`**. `p99` is pushed
and named in the alert but **never reds CI** (decision 4 — heavy-tailed). Amend ADR-038's
visibility-only clause.

**Dependencies:** M0 (`vm-readback-m0-shared-vm-query-lib.md` — `vm_query_window` returns
**per-series keyed by labels**).

## Context

[`scripts/loadtest-summarize.sh`](../../scripts/loadtest-summarize.sh) builds per-`{source,
scenario,phase}` rows (`latency_p50/p95/p99_ms`, `rps`, `error_rate`);
[`load-test.yml`](../../.github/workflows/load-test.yml) pushes them to VM
([`loadtest-vm-push.sh`](../../scripts/loadtest-vm-push.sh)) but has **no gate at all**.
Because metrics are **multi-dimensional**, a single scalar baseline is wrong — group-by
and compare **each series** independently, with per-series cold-start. Per the metric
taxonomy: `latency_p50/p95` + `rps` are **noisy** ⇒ window median + calibrated band +
absolute ceiling; `error_rate` is **near-deterministic** ⇒ absolute ceiling (+ prev-sample
rel); `p99` is **very noisy** ⇒ alert-only.

## Calibration — empirical, not guessed (do FIRST; ship numbers here)

Per series, query the live 14–30d VM window, compute run-to-run CV, freeze `tol = k × CV`.
Record before coding (locked decision 5 — numbers in the plan, not code comments). The k6
SLA numbers are the natural `error_rate` / latency absolute ceilings.

Live query date: 2026-07-02. VictoriaMetrics retained 15 QUIC load-test samples and no k6
load-test samples in the 30d window, so k6 starts in cold-start absolute-only mode until
new history lands. Frozen bands: latency p50/p95 = 200% (covers the seeded QUIC worst
normal excursions; roughly 4x the worst seeded latency CV, rounded up), rps = 50% (roughly
2x the seeded rps CV, rounded up), p99 advisory = 300% and never red.

| metric | scenario / phase / source | 30d median | CV | frozen rel tol | abs ceiling/floor |
|---|---|---:|---:|---:|---:|
| latency_p95_ms | api-baseline / http / k6 | cold-start (no 30d samples) | n/a | 200% once seeded | ceiling 200 ms |
| rps | concurrent-agents / http / k6 | cold-start (no 30d samples) | n/a | 50% once seeded | floor 5 rps |
| latency_p95_ms | quic-agents / connect / quic | 257 ms | 43.9% | 200% | ceiling 1000 ms |
| latency_p95_ms | quic-agents / handshake / quic | 169 ms | 29.1% | 200% | ceiling 500 ms |
| rps | quic-agents / aggregate / quic | 198.807157 | 23.1% | 50% | floor 50 rps |
| error_rate | quic-agents / aggregate / quic | 0 | n/a | previous-sample only | ceiling 0 |

## File inventory

- **Create** `scripts/loadtest-regression-check.sh` — reads the summarize rows JSON
  (stdin/arg) + sources `lib/vm-query.sh` (M0). Per series (group by source+scenario+phase),
  **direction-aware** two-rule (latency/error_rate: higher = worse; rps: lower = worse):
  `latency_p50/p95` + `rps` via window median + calibrated band + absolute ceiling;
  `error_rate` via absolute ceiling (+ prev-sample rel); `p99` folded into the alert text
  but **never** sets `regression`. Per-series cold-start ⇒ absolute-only. Fail-open. Emits
  the rows passthrough + `REGRESSION_ALERT:` lines + exit 1 on regression (mirror
  [`mutation-summarize.sh`](../../scripts/mutation-summarize.sh)).
- **Modify** [`.github/workflows/load-test.yml`](../../.github/workflows/load-test.yml) —
  the `load-test` job **uploads the summary JSON as an artifact** (and stops pushing VM
  here). Add a new **`publish`** job: `needs: [load-test]`, `if: always()`,
  `environment: observability`, OCI+kube setup → download summary → run
  `loadtest-regression-check.sh` (reads the VM window baseline) → **push to VM (always)**
  → Telegram on regression → fail-red step. Mirror the mutation/benchmark publish job.
  Keep the `load-test` job **without** an `environment:` (its comment deliberately avoids
  staging reviewers; `observability` — no reviewers — lives on `publish` only).
- **Create** `scripts/tests/loadtest-regression-check.test.sh` — mock `kubectl` per-series
  window responses: a seeded `p95` breach on one series ⇒ `regression=1` (that series only);
  `rps` drop ⇒ regression; `error_rate` over ceiling ⇒ regression; a `p99`-only breach ⇒
  `regression=0` (alert-only); per-series cold-start ⇒ absolute-only; fail-open on
  empty/transport.
- **Modify** [`docs/adr/ADR-038-victoriametrics-ci-trend-store.md`](../../docs/adr/ADR-038-victoriametrics-ci-trend-store.md)
  — the §Consequences "load-test trends remain visibility-only … only pass/fail gates" is
  a **decision change** (a reversal, not a correction), so **author a new ADR** (next
  sequential number) that supersedes that consequence, set ADR-038's `status:`/the new
  ADR's `supersedes:` frontmatter, and add an index row in
  [`.claude/decisions.md`](../decisions.md). Also update
  [`docs/Monitoring.md`](../../docs/Monitoring.md) + [`docs/CI-Pipeline.md`](../../docs/CI-Pipeline.md)
  load-test rows.

## Steps (TDD-first)

1. **Calibrate** (above); fill the table.
2. **Test first:** write `loadtest-regression-check.test.sh` (per-series seeded breach,
   p99-alert-only, cold-start, fail-open) → red.
3. Write `loadtest-regression-check.sh`.
4. Wire `load-test.yml`: summary artifact upload + new `publish` job.
5. Author the superseding ADR + `decisions.md` row; update docs.

## Gotchas / constraints

- **Per-series, not one scalar** — one noisy series must not drag another; group by
  source+scenario+phase.
- **`p99` never reds** (decision 4) — pushed + named in the alert only.
- **Direction param** — latency/error_rate higher = worse; rps/score lower = worse.
- **Per-series cold-start** (new k6 scenario / thin history) ⇒ absolute-only, never red on
  missing history.
- **Fail-open** on VM/infra/kubectl failure ⇒ absolute-only (or skip), never red on infra.
- **Exclude current commit** (M0 handles); **window (14d) < VM retention (30d)** (clamp).
- **Null/missing per series** — summarize emits `null` (not 0); the gate skips nulls per series.
- `observability` environment on the **publish** job only; `load-test` stays environment-less.
- Runner-regime change residual risk (wide bands + Telegram soften) — document.

## Reviewer checklist

- [ ] Per-series gate (group by source+scenario+phase); one series can't drag another.
- [ ] `latency_p50/p95` + `rps` window-median band + abs ceiling; `error_rate` abs ceiling;
      `p99` alert-only (never reds).
- [ ] Direction-aware; per-series cold-start ⇒ absolute-only; fail-open (no red on infra);
      current commit excluded.
- [ ] New `publish` job mirrors mutation/benchmark (push always → Telegram → fail-red);
      `observability` env on publish only; `load-test` job uploads the summary artifact.
- [ ] Calibration numbers in this plan; ADR-038 consequence superseded + `decisions.md` row.
- [ ] `/precommit` green; `Monitoring.md` / `CI-Pipeline.md` updated.

## Verification

`make shell-test` (`loadtest-regression-check.test.sh`). Dry-run the `publish` job with a
stubbed per-series VM window (`regression=1` on the seeded series + alert incl. `p99`) and a
forced-empty VM (fail-open, no red). `/docs` + the new ADR.
