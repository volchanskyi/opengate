# WS-5 — On-demand correlation engine (Netdata Anomaly-Advisor)

**Objective:** Given a time window + tenant, rank the top-N metric dimensions that "broke
pattern" using native SQL over the hypertable (RLS auto-scopes), bounded by concurrency +
timeout. Query-time only — no continuous background matrix.

**Dependencies:** WS-4 (hypertable + continuous aggregates). **Blocks:** WS-6.

## Context

Netdata's Metric Correlations / Anomaly Advisor ranks dimensions by two methods: KS-test
(two-sample distribution shift) and anomaly-rate volume. It runs **on demand** when an
operator investigates an incident window — never as an always-on aggregator. RLS from WS-0
auto-scopes every query when the tenant GUC is set in the request tx.

## File inventory

- **Create:** `server/internal/correlate/` — ranking (KS-test + anomaly-rate volume),
  concurrency limiter, per-request timeout.
- **Modify:** [`api/openapi.yaml`](../../../api/openapi.yaml) — `POST /devices/{id}/correlate`
  (or a fleet/tenant-scoped variant): body = window; response = ranked top-N + correlated
  events. Regenerate Go + TS (`oapi-codegen`; `npm run generate:api`).
- **Modify:** [`server/internal/api/`](../../../server/internal/api/) handler wiring.

## Steps (TDD-first)

1. **Test first:** ranking unit tests — an injected anomalous dimension ranks #1; a flat
   dimension ranks low; KS-test + anomaly-rate agree on a synthetic shift.
2. **Test first:** bounds tests — concurrency limiter rejects/queues past N; per-request
   timeout fires; **tenant scope enforced** (org A's correlate cannot see org B series).
3. Implement the engine querying `device_metrics` + continuous aggregates within a
   tenant-scoped tx; add the OpenAPI endpoint + handler; regen both sides.

## Gotchas / constraints

- The correlation query **must run inside the tenant-scoped tx** (GUC set) or RLS won't apply.
- Bound CPU: concurrency limiter + statement timeout; this runs on the shared control-plane
  Postgres — a runaway correlation must not starve relay/WebRTC (feeds the WS-8 p99 budget).
- Keep it on-demand; do **not** add background workers.

## Reviewer checklist

- [ ] Ranking tests: injected anomaly ranks #1; positive + negative.
- [ ] Concurrency + timeout bounds tested; tenant-scope deny tested.
- [ ] On-demand only (no background matrix); OpenAPI regen committed for Go + TS.
- [ ] `/precommit` green.

## Verification

`cd server && go test ./internal/correlate/... ./internal/api/...`; OpenAPI types regenerated
and committed. `/precommit` green. `/docs`: API page.
