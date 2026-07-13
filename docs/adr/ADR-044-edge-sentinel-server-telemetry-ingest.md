---
adr: 044
title: Edge Sentinel Server Telemetry Ingest
status: Accepted
date: 2026-07-02
---

# ADR-044: Edge Sentinel Server Telemetry Ingest

## Status

Accepted.

## Context

Edge Sentinel needs central visibility without moving high-volume telemetry into
the control-plane Postgres database. Numeric host health samples are not
relational data, while process rows carry higher privacy risk and need the
hard tenant boundary added by [ADR-041](ADR-041-postgres-rls-multitenancy.md).

The prior Timescale direction is not viable for this rollout because the needed
combination of RLS, compression, and continuous aggregates conflicts with the
tenant boundary. The cluster already runs VictoriaMetrics for metrics, and it
has a dedicated volume and Grafana datasource.

## Decision

Use a split telemetry store:

- Numeric Edge Sentinel samples are written by the server to VictoriaMetrics
  through `server/internal/telemetry`. Agents never receive VM credentials.
- VictoriaMetrics reads go through the same package, which injects the
  authoritative `org_id` matcher and rejects caller-supplied `org_id` matchers.
  This is application-level label scoping for the current single-node VM, not a
  hard isolation boundary for mutually distrusting tenants.
- Process snapshots are stored in the Postgres `device_processes` table created
  by `003_telemetry`; the table carries `org_id`, is forced through RLS, and
  cascades with `devices`.
- The agent control path resolves the enrolled device's actual organization
  after handshake and scopes all agent-originated writes to that org. Payload
  `org_id` fields are ignored for authorization.
- Telemetry dispatch has a small payload cap, interval floor, bounded
  non-blocking persistence slots, and drop accounting so telemetry cannot
  backpressure heartbeat, restart, session, or other control messages.
- The monitoring chart enables VictoriaMetrics stream aggregation for
  `opengate_edge_*` samples using the chart-owned config, while preserving raw
  matched input for short-range queries.
- The app chart passes the VM base URL through
  `OPENGATE_VICTORIAMETRICS_URL`; unset local/dev servers keep numeric
  telemetry disabled while process RLS persistence remains available.

## Consequences

- Numeric telemetry stays off the control-plane Postgres database and on the
  existing metrics store.
- Sensitive process metadata has the same fail-closed RLS boundary as other
  tenant tables.
- VM label scoping is centralized and testable, but future large-tier or
  external multi-tenant operation may require VM cluster multitenancy or
  per-tenant stores.
- Historical backfill must avoid the live stream-aggregation path and write
  correctly timestamped rollups directly, as proven by the Wave 0 backfill
  spike.
- Agent emission remains default-off until the ARM footprint artifact and soak
  gates pass. The default-on decision is gated on a sustained multi-tenant soak
  (health summaries + windows + minimal process + a fleet-wide reconnect storm)
  measured on the Edge-Sentinel Soak dashboard: it flips on only once
  control-plane query p99 regresses ≤ 20% under default telemetry, VM active-series
  cardinality and disk growth track the model, and the reconnect storm drains
  without starving live traffic. The ingest path is instrumented for that gate —
  `opengate_edge_telemetry_ingested_total`, `opengate_edge_telemetry_drops_total`,
  and the `opengate_edge_backfill_*` scheduler series (see
  [Monitoring](../Monitoring.md#sustained-soak-and-default-on-gate)).
