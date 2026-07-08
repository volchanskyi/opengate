---
adr: 046
title: Edge Sentinel Raw-Log Broker (No Central Persistence)
status: Accepted
date: 2026-07-07
---

# ADR-046: Edge Sentinel Raw-Log Broker (No Central Persistence)

## Status

Accepted.

## Context

Edge-Sentinel centralizes cheap log-*rate* signals in VictoriaMetrics
([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md)) while raw log
lines stay at the edge and are pulled on demand. Raw endpoint logs are
secret-dense (tokens, credentials, PII), so the master plan locks "nothing raw
persisted centrally".

The reused on-demand path did not honor that: the agent's `DeviceLogsResponse`
was DELETE-then-INSERTed into a central `device_logs` table and served back from
a 5-minute cache. Even under the RLS added by
[ADR-041](ADR-041-postgres-rls-multitenancy.md), that left secret-dense raw text
sitting in the control-plane database — a standing privacy/compliance liability
and a redundant, weaker copy of what the agent's own journald / Event Log
already retains.

## Decision

Retire the central raw-log cache and broker every raw pull transiently.

- The `GET /devices/{id}/logs` handler resolves the connected agent, sends a
  `RequestDeviceLogs` control message, and **blocks** on a per-connection
  single-flight waiter until the agent's response arrives (or a bounded timeout
  fires). The bounded lines are redacted and returned in the same HTTP response.
  Nothing is persisted, so raw-log tenant isolation is the agent connection's
  scope, not an RLS row.
- Responses carry no correlation id, so one raw pull runs per connection at a
  time; a concurrent caller gets `409`. Timeouts surface as `504`.
- Reading raw logs is an **elevated** action gated on admin, and **every pull
  writes an audit event** (`device.logs.read`) recording who, which device, and
  the requested window/filters — never raw content.
- The response is **bounded** (line count, per-line bytes, and the blocking
  time cap) and passes a **server-side redaction guard** that strips known
  secrets (auth headers, key/value credentials, AWS keys, PEM private keys) even
  when agent-side redaction is off.
- The `device_logs` table, its RLS policy, and its repository are dropped
  (`004_retire_device_logs`).
- Log-*rate* dims continue to flow to VictoriaMetrics through the existing
  scoped telemetry client, scoped by the server-resolved device org.

## Consequences

- The "nothing raw persisted centrally" guarantee is now structural: there is
  nothing to leak in a database dump, nothing to TTL-purge, and nothing for the
  data-lifecycle erasure workstream to cascade for raw logs.
- Raw logs are readable only while the device is online; a disconnected device
  exposes no central history. This is acceptable because the agent's log source
  is the durable, time-queryable store of record under the edge-first design.
- The pull is a blocking round trip to the edge, so the endpoint is synchronous
  with `409`/`504` failure modes and the web viewer fetches directly rather than
  polling a cache.
- Single-flight-per-connection is the deliberate simplification (no wire
  correlation id); if concurrent per-device pulls ever matter, a correlation id
  on `RequestDeviceLogs`/`DeviceLogsResponse` is the additive follow-up.
