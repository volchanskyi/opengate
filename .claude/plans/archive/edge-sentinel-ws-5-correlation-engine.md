# WS-5 — On-demand correlation engine (Netdata Anomaly-Advisor, over VictoriaMetrics)

**Objective:** Given a time window + tenant, rank the top-N metric dimensions that "broke
pattern" by pulling candidate series from **VictoriaMetrics** (MetricsQL) and computing the
ranking **server-side in Go** (KS-test two-sample + anomaly-rate volume), bounded by
concurrency + timeout. Query-time only — no continuous background matrix.

**Dependencies:** WS-4 (VM ingest + the scoped VM query client). **Blocks:** WS-6.

## Context

Netdata's Metric Correlations / Anomaly Advisor ranks dimensions by KS-test (distribution
shift) and anomaly-rate volume, **on demand** when an operator investigates an incident window.
VM speaks **MetricsQL (PromQL dialect)**, not SQL — there is no native KS-test or SQL JOIN — so
the ranking moves into Go: the server fetches the candidate series for the window via the
**scoped VM query client from WS-4** (which injects the tenant `org_id` label matcher, so a
query can only ever see one org's series) and computes the statistics in-process.

## File inventory

- **Create:** `server/internal/correlate/` — series fetch (via the WS-4 scoped VM client),
  KS-test + anomaly-rate ranking, concurrency limiter, per-request timeout.
- **Modify:** [`api/openapi.yaml`](../../api/openapi.yaml) — `POST /devices/{id}/correlate`
  (or a fleet/tenant-scoped variant): body = window; response = ranked top-N + correlated
  events. Regenerate Go + TS (`oapi-codegen`; `npm run generate:api`).
- **Modify:** [`server/internal/api/`](../../server/internal/api/) handler wiring.

## Steps (TDD-first)

1. **Test first:** ranking unit tests (pure Go, fixture series) — an injected anomalous
   dimension ranks #1; a flat dimension ranks low; KS-test + anomaly-rate agree on a synthetic
   shift.
2. **Test first:** bounds + tenancy tests — concurrency limiter rejects/queues past N;
   per-request timeout fires; the scoped VM client makes org A's correlate unable to see org B
   series (label matcher enforced).
3. Implement the engine: fetch window series from VM via the scoped client, rank in Go; add the
   OpenAPI endpoint + handler; regen both sides.

## Gotchas / constraints

- All VM reads go through the **WS-4 scoped query client** — never an unscoped MetricsQL call
  (numeric tenancy is app-layer; the label matcher is the isolation boundary).
- Bound CPU/memory: concurrency limiter + per-request timeout + a cap on series/points fetched
  per correlation (a runaway must not starve the server). Correlation runs on the **server**,
  not the control-plane Postgres — so it no longer competes with relay/WebRTC for DB I/O.
- Keep it on-demand; do **not** add background workers.

## Reviewer checklist

- [ ] Ranking tests: injected anomaly ranks #1; positive + negative; pure-Go KS-test covered.
- [ ] Concurrency + timeout + fetch-size bounds tested; tenant-scope (label matcher) deny tested.
- [ ] On-demand only (no background matrix); reads only via the WS-4 scoped VM client.
- [ ] OpenAPI regen committed for Go + TS; `/precommit` green.

## Verification

`cd server && go test ./internal/correlate/... ./internal/api/...`; OpenAPI types regenerated
and committed. `/precommit` green. `/docs`: API page.
