# Micro-Plan B4: Load-Test Trend Persistence

**Parent master:** `benchmarks-grafana-trends.md` (§9 B4). **Branch:** `dev`.
**Owner:** CI/Bash. **Depends on:** `bench-trends-b1-vm-transport.md`.

## 1. Goal

Persist load-test summary metrics as a VM trend for visibility. The existing k6
threshold pass/fail gate stays the **only** alarm — **trends-only**, no new
latency-regression alert (shared-runner latency is too noisy to gate on).

## 2. Scope

**In:** extract summary metrics from [`load-test.yml`](../../../.github/workflows/load-test.yml)
(k6 `--summary-export` JSON + the QUIC harness output) → VM push → PromQL dashboard.
**Out:** any new alert; changing the k6 thresholds; per-PR runs.

## 3. File inventory

| File | Change |
|---|---|
| `scripts/loadtest-summarize.sh` | **New, sourceable + unit-tested.** Parse k6 `--summary-export` JSON + QUIC harness output → canonical metrics: latency p50/p95/p99, throughput (rps), error rate. |
| `scripts/loadtest-vm-push.sh` | **New.** Map → Prometheus text per B1 convention (`loadtest_latency_*`, `loadtest_rps`, `loadtest_error_rate`; labels `commit`,`env=ci`) → `lib/vm-push.sh`. |
| `scripts/tests/loadtest-summarize.test.sh` | **New.** Behavioral test on a captured k6 summary-export + QUIC harness fixture. |
| [`.github/workflows/load-test.yml`](../../../.github/workflows/load-test.yml) | Add a `--summary-export` flag to the k6 run (if absent) and a post-run push step (`if: always()`); the threshold gate stays as-is. |
| `deploy/grafana/provisioning/dashboards/loadtest-trend.json` | **New.** PromQL panels (uid `VictoriaMetrics`): latency percentiles, rps, error rate over time. |

## 4. Approach (TDD)

1. B1 merged.
2. Capture a real k6 `--summary-export` JSON + QUIC harness output as fixtures.
3. **Red:** `loadtest-summarize.test.sh` asserts the canonical metrics extracted from
   the fixtures; implement `loadtest-summarize.sh` to green.
4. Implement `loadtest-vm-push.sh`; add the workflow push step (after the gate, `if:
   always()` so a failing threshold still records the trend).
5. Author `loadtest-trend.json`; verify it renders from VM live.
6. `make shell-quality` + `/precommit` green.

## 5. Acceptance criteria / DoD

- [ ] A load-test run pushes latency/rps/error-rate to VM; the `loadtest-trend.json`
      dashboard renders the series in Grafana (verified live).
- [ ] The k6 threshold gate is unchanged and remains the only alarm; **no** new alert
      added.
- [ ] Trend is recorded even when the gate fails (`if: always()`).
- [ ] `loadtest-summarize.sh` sourceable + unit-tested; `make shell-quality` green.
- [ ] `/precommit` green.

## 6. NFRs

- **Performance:** load-test cadence unchanged; tiny VM volume.
- **Reliability:** `if: always()` capture; no dependence on gate outcome.
- **Maintainability:** reuses B1 transport + the dashboard style.

## 7. Reviewer/QA checklist

- [ ] No latency-regression alert was added (trends-only).
- [ ] `--summary-export` parsing handles the actual k6 JSON shape (fixture-backed).
- [ ] QUIC-harness metrics are included, not just k6.
- [ ] Dashboard uses uid `VictoriaMetrics` + B1 metric names.
