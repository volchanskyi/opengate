---
number: 44
title: The server writes telemetry, agents never do
---

# ADR-044 — The server writes telemetry, agents never do

## Context

Numeric readings need a time-series store and process listings need rows with a
tenant on them. Letting agents write to the metrics store directly would mean
giving every machine in every fleet a credential for it.

## Decision

**Agents send; the server writes.** Numeric samples go to VictoriaMetrics
through `server/internal/telemetry`. Agents hold no metrics-store credential.

**The tenant is resolved by the server, from the connection.** After handshake
the control path knows which enrolled device this is and therefore which tenant.
A tenant in the payload is ignored. Reads go through the same package, which
injects the caller's own tenant and rejects a caller-supplied one.

**Process listings go to Postgres**, in `device_processes`, with a tenant column
under forced row-level security, cascading with the device.

**Readings stream live over `AgentMetricWindow`.** The sampler folds its
one-second samples into a window aligned to 60 seconds and sends one message per
closed window over a bounded channel, drained on the heartbeat. The channel
drops under pressure, so a burst of readings can never hold up a restart, a
session or a heartbeat.

**Live and catch-up produce the same numbers.** The live fold and the
reconnect-backfill roll-up share the window key and the arithmetic, so a reading
that arrived live and a reading filled in afterwards for the same dimension and
minute are equal and land in one series. A test asserts the two agree for every
dimension.

**A reading that could not be taken is absent, not zero.** Backfill leaves the
same gap. A partial window is discarded rather than emitted early.

**Persistence is coalesced per connection.** Every handler appends to one
buffer, flushed as a single write through a single slot when the heartbeat opens
the next cycle, or on a size cap, or at teardown. Before this, four handlers
competed for a four-slot pool and the fourth reading of a cycle was dropped.

## Consequences

Drops are counted and the ratio is alerted on, so a regression here is visible
rather than absorbed. The fleet-health badge reads a ten-minute lookback rather
than an instant, so a gap between low-rate summaries never blanks it.
