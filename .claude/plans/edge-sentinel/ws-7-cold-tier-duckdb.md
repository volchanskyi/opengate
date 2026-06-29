# WS-7 — Cold tier (Parquet → OCI Object Storage) + server-side DuckDB

**Objective:** Offload aged telemetry from the shared Postgres to tenant-partitioned Parquet
in OCI Object Storage (via server-issued pre-authenticated requests), and query it on-demand
with a server-side DuckDB for historical + cross-fleet relational correlation. Keeps the
control-plane Postgres lean.

**Dependencies:** WS-4 (hypertable + the data to offload). **Parallel with:** WS-6.

## Context

OCI Always-Free object storage = 20 GB + 50k req/mo; the existing backup path already uses a
**write-only PAR** ([`secrets.example.yaml`](../../../deploy/helm/opengate/secrets.example.yaml),
ADR-035 pattern). DuckDB is Arrow-native, reads Parquet directly, and its `postgres_scanner`
can JOIN Parquet ↔ Postgres inventory in one query. Engine is **server-side only** — never
shipped in the agent.

## File inventory

- **Create:** `server/internal/olap/` — DuckDB query layer (`postgres_scanner` JOINs;
  range reads over Parquet); on-demand only.
- **Create:** Timescale **chunk-offload** job (aged chunks → Parquet) + Parquet writer
  (tenant-partitioned `org_id/device/day`).
- **Modify:** object-store client + PAR issuance (server issues short-lived write-only PARs;
  no standing creds at the edge). Optionally the agent flushes its own Parquet via PAR — if
  so, gate behind WS-2/WS-3.
- **Modify:** [`deploy/`](../../../deploy/) — bucket + lifecycle policy + `*_PAR_URL` secret
  (mirror the backup pattern).

## Steps (TDD-first)

1. **Test first:** PAR issuance/expiry tests (write-only, short-lived; cannot list/read).
2. **Test first:** DuckDB query smoke over a sample Parquet fixture; `postgres_scanner` JOIN
   returns tenant-scoped rows; cross-tenant partition is not readable.
3. Implement chunk-offload + Parquet writer + object-store client; wire the OLAP query path.

## Gotchas / constraints

- **DuckDB = CGO** — new build dependency, **ADR** required; keep it server-side.
- Stay within 20 GB / 50k-req budget — partition + lifecycle policy; batch reads.
- No standing object-store creds; PARs only, short-lived, write-only for uploads.
- Tenant isolation extends to Parquet partition paths; OLAP queries must filter by org.

## Reviewer checklist

- [ ] PAR write-only + short-lived; no standing creds; tests for expiry/scope.
- [ ] DuckDB server-side only (ADR for the CGO dep); tenant-scoped reads tested.
- [ ] Chunk-offload keeps Postgres lean; object budget respected.
- [ ] `/precommit` green.

## Verification

`cd server && go test ./internal/olap/...` (sample Parquet fixture). `/precommit` green.
`/docs`: Database/Monitoring + a cold-tier note.
