# WS-4 — Server ingest + TimescaleDB hypertable + process RLS table

**Objective:** Persist incoming telemetry: numeric series → a TimescaleDB hypertable
(extension on the existing Postgres, compressed, continuous-aggregate rollups, retention);
process descriptive data (name + **redacted** cmdline) → an RLS relational table. Wire the
new control messages into the dispatch switch.

**Dependencies:** WS-0 (org_id + RLS + tenant-tx helper), WS-3 (messages). **Blocks:** WS-5,
WS-6, WS-7.

## Context

Handlers live in [`conn.go`](../../../server/internal/agentapi/conn.go) (`handleControl`
switch; mirror `handleHardwareReport`). DB is Postgres 17 via the repository pattern; WS-0
added the tenant-scoped tx helper. Telemetry **shares the control-plane Postgres** (free-tier
has no spare volume) — so compression + continuous aggregates + retention are **mandatory**.

## File inventory

- **Create:** `server/internal/db/migrations/003_telemetry.{up,down}.sql` — `CREATE EXTENSION
  timescaledb`; `device_metrics` hypertable (numeric, `org_id`, RLS, compression policy,
  continuous aggregates 1 min/1 hr, retention); `device_processes` RLS table (rank, name,
  cmdline, pid, cpu, mem, ts).
- **Create:** `server/internal/telemetry/` repo (hypertable writes + process table writes,
  tenant-scoped).
- **Modify:** [`conn.go`](../../../server/internal/agentapi/conn.go) — add cases for
  `AgentHealthSummary`, `AgentMetricWindow`, `ProcessReport`; **server-side redaction guard**
  (defense-in-depth even if agent redaction is off); scope writes to the connection's
  device→org (never trust agent-supplied `org_id` for authz).
- **Modify:** [`deploy/helm/`](../../../deploy/helm/) — Postgres image → `timescaledb` flavor
  (ADR-gated).

## Steps (TDD-first)

1. **Test first:** `telemetry` repo tests in `testpg` (hypertable insert + read; continuous
   aggregate returns rollups; **RLS cross-tenant-deny**; retention/compression policy
   present) → write `003` + the repo.
2. **Test first:** `conn.go` handler tests (each new message persists; redaction guard
   strips secrets; unknown still tolerated per WS-1) → add the switch cases.
3. ADR + helm change for the TimescaleDB image.

## Gotchas / constraints

- **RLS + hypertables + continuous aggregates** interact — verify policies apply to chunks
  and to the continuous-aggregate views; test cross-tenant deny on the aggregates too.
- Compression policy vs RLS: ensure compressed chunks remain RLS-scoped.
- Batch inserts; bound per-message size; the tenant-tx helper must wrap every write.
- Server-side redaction is **defense-in-depth**, not a replacement for WS-2 redaction.

## Reviewer checklist

- [ ] Migration: hypertable + compression + continuous aggregates + retention; `org_id`+RLS
      on `device_metrics` and `device_processes`; `.down.sql` reverses.
- [ ] Handlers persist via tenant-tx; authz scope from connection, not agent payload.
- [ ] Server redaction guard tested; RLS cross-tenant-deny on tables **and** aggregates.
- [ ] TimescaleDB image via ADR + helm; `/precommit` (incl. sonar) green.

## Verification

`cd server && go test ./internal/telemetry/... ./internal/agentapi/...` (testpg auto-starts
Timescale image). `/precommit` green. `/docs`: Database + Monitoring pages.
