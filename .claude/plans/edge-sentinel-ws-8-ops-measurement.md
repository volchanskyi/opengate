# WS-8 — Ops + measurement (Grafana, load-test, p99 budget + mitigation ladder)

**Objective:** Make the telemetry path observable and **measure** the accepted control-plane
contention risk end-to-end. Produce the numbers that validate (or trigger mitigation of) the
Timescale-on-shared-Postgres decision. Alerts are deferred (investigation-aid only).

**Dependencies:** WS-4 (ingest) + WS-6 (full path). Final wave.

## Context

The locked combo (TimescaleDB extension on the existing Postgres + full {min,max,avg,last}@10s
+ process metrics) shares the DB that runs live relay/WebRTC/device control. The master plan
mandates a measured **control-plane p99 budget** and a **mitigation ladder** (coarsen
interval → drop to 2 aggregates → isolate Timescale to its own instance). This WS implements
the measurement and the dashboard. The load-test harness already exists
([`server/tests/loadtest/main.go`](../../server/tests/loadtest/main.go)); VictoriaMetrics
+ Grafana are deployed ([`deploy/helm/monitoring/values.yaml`](../../deploy/helm/monitoring/values.yaml)).

## File inventory

- **Modify:** [`server/tests/loadtest/main.go`](../../server/tests/loadtest/main.go) —
  drive **500 multi-tenant agents** emitting full telemetry (summaries + windows + process).
- **Create:** Grafana dashboard JSON under
  [`deploy/grafana/provisioning/dashboards/`](../../deploy/grafana/provisioning/dashboards/)
  (Postgres/Timescale datasource): anomaly-rate, ingest rate, active-series cardinality,
  control-plane query p99, correlation latency.
- **Modify:** [`server/internal/metrics/`](../../server/internal/metrics/) — counters for
  telemetry ingest + correlation if missing.

## Steps (TDD-first)

1. **Test first:** loadtest scenario test (the harness spins N tenants × M agents
   deterministically; assertion that telemetry frames are produced/accepted).
2. Extend the harness to emit the new messages at scale; record: control-plane p99 under
   metric-write load, ingest throughput + compression ratio, active-series cardinality,
   correlation query latency, RLS overhead.
3. Add the Grafana dashboard; add any missing Prometheus counters.
4. **Document the measured numbers + the mitigation-ladder trigger** in `/docs` and the
   TimescaleDB ADR (close the loop the architect flagged).

## Gotchas / constraints

- Use `make e2e`/the Docker lifecycle for any stack run — never bare tooling.
- Measurement **informs tuning**, it is not a build gate (all WS ship); but if the p99 budget
  is exceeded, record it and apply the ladder.
- Tenancy in the load-test: multiple orgs, verify RLS holds under concurrency.

## Reviewer checklist

- [ ] Loadtest drives 500 multi-tenant agents with full telemetry; deterministic.
- [ ] Dashboard shows ingest, cardinality, control-plane p99, correlation latency.
- [ ] Measured numbers + ladder trigger documented in the TimescaleDB ADR + `/docs`.
- [ ] Alerts deferred (investigation-aid only); `/precommit` green.

## Verification

`cd server && go test ./tests/loadtest/...`; run the harness against the compose/e2e stack;
capture metrics in Grafana. `/precommit` green. `/docs`: Monitoring + Multiscale-Readiness.
