# WS-7 — Cold tier (VM retention + stream-agg rollups; optional Parquet export)

**Objective:** Keep long-term telemetry cheap. The primary mechanism is **VictoriaMetrics OSS
retention + stream-aggregation rollups** (set up in WS-4) — short raw retention, long-lived
1-min/1-hr rollups. An **optional** server-side Parquet export of aged data to OCI Object
Storage is provided for compliance/archival only. DuckDB is **deferred** (no CGO dependency in
this rollout).

**Dependencies:** WS-4 (VM ingest + stream-agg). **Parallel with:** WS-6.

## Why this shrank from the original

The original WS-7 (agent Parquet flush + server DuckDB `postgres_scanner` over a Timescale hot
store) assumed Timescale and a storage squeeze. With telemetry on VM (48.9 GB free on its own
volume, Gorilla compression, stream-agg rollups), the long-term tier is largely **already
solved inside VM** — no Parquet/DuckDB needed for normal operation. OCI Always-Free object
storage is 20 GB + **50k req/mo**, and per-agent uploads blow that budget instantly (500 agents
hourly = 360k req/mo), so any object writes must be **server-side batched**, never per-agent.

## File inventory

- **Modify:** [`deploy/helm/monitoring/`](../../../deploy/helm/monitoring/) — confirm VM retention
  split (raw vs rollup) and document the long-term-tier behavior (the real WS-7 deliverable).
- **Create (optional/archival only):** a server-side aged-data → Parquet export job
  (tenant-partitioned `org_id/day` keys) using the existing **write-only PAR** pattern
  ([`secrets.example.yaml`](../../../deploy/helm/opengate/secrets.example.yaml), ADR-035). Batched,
  coarse cadence (≤ daily), well within 50k req/mo. **No per-agent uploads.**
- **Deferred:** DuckDB / `olap` package — only if a separate ADR proves a need that VM
  MetricsQL + rollups cannot meet.

## Steps (TDD-first)

1. **Test first:** retention/rollup assertion — aged raw is dropped, rollups survive (drives
   the VM retention config alongside WS-4).
2. **Test first (only if Parquet export is built):** PAR issuance/expiry (write-only,
   short-lived, prefix-scoped to one `org_id`); a broad bucket glob is **not** used; request
   count per export stays within budget.
3. Implement the export job only if archival is in scope this rollout; otherwise WS-7 = VM
   retention + docs.

## Gotchas / constraints

- **Server-side batched only** — per-agent object uploads are incompatible with 50k req/mo.
- PARs (if used) are write-only, short-lived, **prefix-scoped to the tenant partition**; tenant
  isolation in object storage **is** the file list (no RLS reaches objects).
- Keep VM the source of truth for long-term; Parquet is archival, not a query path, unless a
  DuckDB ADR is added later.

## Reviewer checklist

- [ ] VM retention split (short raw + long rollup) documented and applied.
- [ ] Any object writes are server-side batched, ≤ daily, within 20 GB / 50k req; PARs
      write-only + short-lived + prefix-scoped; expiry/scope tested.
- [ ] DuckDB **not** introduced (no CGO dep) unless a separate ADR justifies it.
- [ ] `/precommit` green.

## Verification

`cd server && go test ./internal/telemetry/...` (retention/rollup behavior; PAR scope if built).
`/precommit` green. `/docs`: Database/Monitoring + a cold-tier note.
