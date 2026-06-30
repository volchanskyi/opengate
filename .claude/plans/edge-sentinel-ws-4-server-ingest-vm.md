# WS-4 — Server ingest → VictoriaMetrics (numeric) + Postgres RLS process table

**Objective:** Persist incoming telemetry. Numeric series → the **already-deployed
VictoriaMetrics** (OSS) via server-side `remote_write`, tenant-tagged with an `org_id` label
and downsampled with OSS **stream aggregation**; descriptive process data (basename, rank, pid,
cpu, mem, optional cmdline hash) → a Postgres **RLS** relational table. Wire the new control
messages into the dispatch switch.

**Dependencies:** WS-0 (org_id + RLS + tenant-tx helper), WS-3 (messages). **Blocks:** WS-5,
WS-6, WS-7. **Gated by:** Wave 0 (VM ingest feasibility must pass first).

## Why VictoriaMetrics, not Timescale (decision record)

Timescale on the existing Postgres was rejected: upstream
[#6827](https://github.com/timescale/timescaledb/issues/6827) +
[#5787](https://github.com/timescale/timescaledb/issues/5787) (both open) prove **RLS cannot
coexist with compression or continuous aggregates on a hypertable** — so the original WS-4
combination is impossible. Live `df` shows VictoriaMetrics' own 50 GB volume at **0% (26 MB
used)**: ~48.9 GB free, already paid for, already counted in the 200 GB cap. VM is purpose-built
for this cardinality, keeps telemetry **off the control-plane Postgres** (eliminates the
relay/WebRTC contention risk), is Apache-2, and its Grafana datasource is already wired. The
downsampling Timescale CAGGs would have provided is covered by VM OSS **stream aggregation**.

## Context

Handlers live in [`conn.go`](../../server/internal/agentapi/conn.go) (`handleControl`
switch; mirror `handleHardwareReport`). The server is the **sole writer** to VM — agents never
hold store credentials; writes are scoped by the connection's enrolled device→org. VM today is
scrape-only; this WS adds a server **push** path (`remote_write` / `/api/v1/import`).

## File inventory

- **Create:** `server/internal/telemetry/` — a VM `remote_write` client (numeric series with an
  `org_id` label) + a Postgres process-table repo (tenant-scoped via the WS-0 tx helper).
- **Create:** `server/internal/db/migrations/003_telemetry.{up,down}.sql` — `device_processes`
  RLS table (org_id, ts, rank, basename, pid, cpu, mem, optional cmdline_hash). **No Timescale.**
- **Modify:** [`conn.go`](../../server/internal/agentapi/conn.go) — cases for
  `AgentHealthSummary`, `AgentMetricWindow`, `ProcessReport`: numeric → VM client, process →
  Postgres; scope by connection device→org (**never** trust agent-supplied `org_id` for authz);
  server-side redaction guard (defense-in-depth even if agent redaction is off).
- **Create/Modify:** [`deploy/helm/monitoring/`](../../deploy/helm/monitoring/) — a VM
  `-streamAggr.config` (1-min + 1-hr min/max/avg/last rollups) + retention split (short raw,
  long rollup). **VM `-retentionPeriod` is a sized parameter (default ~90 d** — fits the existing
  free volume for 500 agents at ~15-39 GB; **not a hard cap**, grow as disk allows). Backfill
  (WS-15) clamps to whatever it is set to. Bump the VM `storage` request into the existing headroom.
- **Create:** a server-side **scoped VM query client** (single choke point; injects the tenant
  `org_id` label matcher on every read) for WS-5/WS-6 to reuse.

## Steps (TDD-first)

1. **Test first:** `telemetry` VM-client tests — series carry the `org_id` label; the scoped
   query client refuses/auto-injects so org A cannot read org B series. Use a throwaway VM
   (Testcontainers/compose) so the test always runs deterministically.
2. **Test first:** process-repo tests in `testpg` (insert + read; **RLS cross-tenant-deny**;
   `.down.sql` reverses) → write `003` + the repo.
3. **Test first:** `conn.go` handler tests (each new message persists to the right store;
   redaction guard strips secrets; full cmdline never stored by default; unknown still tolerated
   per WS-1) → add the switch cases.
4. Add the VM stream-aggregation config + retention; verify rollups are produced.

## Gotchas / constraints

- **Numeric tenancy is app-layer** (VM has no RLS): every read goes through the single scoped
  query client; add a CI grep gate forbidding raw VM queries elsewhere. Sensitive PII lives in
  the Postgres RLS table (fail-closed), not in VM.
- **VM tenant-isolation trust model (document explicitly):** single-node VM label scoping
  **emulates** isolation — it is an **application-level guard for internal org segmentation, not a
  hard boundary for mutually-distrusting external tenants**. Hard isolation would require VM cluster
  multitenancy or per-tenant stores (a future ADR at the >20k regime). `org_id` stays mandatory on
  every write/read as defense-in-depth. (Proof: https://docs.victoriametrics.com/keyconcepts/.)
- **Central = `avg` only (cardinality decision).** VM stores **avg per dim**; **min/max/last + 1 s
  raw stay in the agent-local TSDB** (WS-14b), pulled on-demand (WS-15). Aggregates count as
  **separate series** in the VM model, so storing all four would ~4× the active-series count.
- **Metric schema budget + per-entity caps.** Bound per-entity expansion (per-core CPU, per-disk,
  per-interface, per-filesystem) by aggregating or **top-N**, so per-agent series count is bounded
  regardless of host size. No high-cardinality/PII labels (process/package/service names, cmdline,
  path, user, IP, URL, error string) in VM. Wave 0 measures real base-dim + per-entity counts.
- Server is the only VM writer; no standing agent creds; bound per-message size (WS-3 cap).
- **Stream-agg is for LIVE 10 s telemetry only**; historical backfill writes pre-rolled rollups
  directly via the import API (WS-15), never through `-streamAggr`.
- Process table writes go through the WS-0 tenant-tx helper.

## Reviewer checklist

- [ ] Numeric → VM with `org_id` label; all reads via the scoped client; CI grep gate added.
- [ ] `device_processes`: org_id + RLS + cross-tenant-deny; `.down.sql` reverses; **no Timescale**.
- [ ] Handlers persist via the right store; authz scope from connection, not agent payload.
- [ ] Server redaction guard tested; full cmdline not stored by default.
- [ ] VM stream-agg + retention applied; storage request fits existing headroom; `/precommit` green.

## Verification

`cd server && go test ./internal/telemetry/... ./internal/agentapi/...` (throwaway VM +
`testpg` auto-start). `/precommit` green. `/docs`: Database + Monitoring pages.
