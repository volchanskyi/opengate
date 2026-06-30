# WS-8 — Ops + sustained soak + default-on gate (Grafana, load-test)

**Objective:** Make the telemetry path observable and run the **sustained soak** that decides
default-on. Baseline feasibility measurement has moved **earlier** — it is now the **Wave 0
gate** (see root plan); WS-8 is the long-running confirmation at the intended default level.
Alerts are deferred (investigation-aid only).

**Dependencies:** WS-4 (ingest) + WS-6 (full path). Final wave. **Pairs with:** the Wave 0
feasibility gate (same harness, longer duration, default settings).

## Context

Telemetry numeric series live in **VictoriaMetrics** (off the control-plane Postgres), so the
original "shared-Postgres contention" risk is largely removed — but the soak still proves it
empirically: control-plane p99 must not regress under telemetry load, VM disk/series growth must
track the model, and stream-agg rollups must hold long-term storage flat. VictoriaMetrics +
Grafana are already deployed
([`deploy/helm/monitoring/values.yaml`](../../deploy/helm/monitoring/values.yaml); the Grafana
VM datasource is already wired). The load harness exists
([`server/tests/loadtest/main.go`](../../server/tests/loadtest/main.go)).

## File inventory

- **Modify:** [`server/tests/loadtest/main.go`](../../server/tests/loadtest/main.go) — drive
  **500 multi-tenant agents** emitting the default telemetry shape (summaries + windows +
  minimal process) over a sustained run; plus a **fleet-wide reconnect-storm scenario** (a large
  cohort returns at once with offline backlogs) to prove the WS-15 scheduler drains gradually
  without starving live + control or breaching the p99 budget.
- **Create:** Grafana dashboard JSON under
  [`deploy/grafana/provisioning/dashboards/`](../../deploy/grafana/provisioning/dashboards/)
  (VM datasource): anomaly-rate, ingest rate, **VM active-series cardinality + disk growth**,
  control-plane query p99, correlation latency, telemetry drop count + queue depth, **backfill
  scheduler state (active/queued slots, granted ingest rate, per-tenant fair-share, deferrals)**.
- **Modify:** [`server/internal/metrics/`](../../server/internal/metrics/) — counters for
  telemetry ingest + drops + correlation if missing.

## Steps (TDD-first)

1. **Test first:** loadtest scenario test (N tenants × M agents deterministically; assertion
   that telemetry frames are produced/accepted and land in VM).
2. Run the sustained soak at the default level; record vs the Wave 0 budgets: control-plane p99
   regression (≤ 20%), VM ingest throughput, active-series cardinality, disk growth + rollup
   lag, correlation latency.
3. Add the Grafana dashboard; add any missing Prometheus counters.
4. **Default-on decision:** flip the default only if every quality metric passes; document the
   measured numbers in the telemetry-storage ADR + `/docs`.

## Gotchas / constraints

- Use `make e2e`/the Docker lifecycle for any stack run — never bare tooling.
- The soak is the **default-on gate** (feasibility already gated at Wave 0): if a budget fails,
  keep telemetry default-off and record the gap.
- Tenancy in the load-test: multiple orgs; verify the VM label-scoped reads and the Postgres
  RLS process table both hold under concurrency.

## Reviewer checklist

- [ ] Loadtest drives 500 multi-tenant agents with default telemetry; deterministic.
- [ ] Dashboard shows ingest, VM cardinality + disk growth, control-plane p99, correlation
      latency, drop count.
- [ ] Measured numbers + default-on decision documented in the telemetry-storage ADR + `/docs`.
- [ ] Alerts deferred (investigation-aid only); `/precommit` green.

## Verification

`cd server && go test ./tests/loadtest/...`; run the harness against the compose/e2e stack;
capture metrics in Grafana. `/precommit` green. `/docs`: Monitoring + Multiscale-Readiness.
