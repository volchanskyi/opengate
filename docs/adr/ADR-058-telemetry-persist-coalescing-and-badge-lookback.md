---
adr: 058
title: Telemetry Persist Coalescing and Fleet-Health Badge Lookback
status: Accepted
date: 2026-07-24
---

# ADR-058: Telemetry Persist Coalescing and Fleet-Health Badge Lookback

## Status

Accepted.

## Context

Each agent heartbeat delivers a burst of telemetry control messages: the agent
sends the heartbeat first, then drains its queued host-metric windows (~6 per
cycle), discovery reports, and a single tail-ordered `AgentHealthSummary`. The
server persisted each message independently through a best-effort, four-slot
writer that **dropped** a message (`persist_slots_full`) when every slot was
busy. The dense host-window firehose held all four slots for up to the persist
timeout, so the low-rate, high-value `AgentHealthSummary` — always last in the
burst — lost the slot race and was dropped the overwhelming majority of the
time. The fleet-health badge reads the node anomaly rate as an instant query, so
the starved series read empty and the badge showed "No data" on a healthy agent.

## Decision

Coalesce per-connection telemetry persistence, and give the badge query a
bounded lookback.

- Every telemetry handler (`AgentMetricWindow`, `AgentHealthSummary`,
  `HealthWindowResponse`, and the numeric samples of `ProcessReport`) appends its
  samples to a per-connection buffer instead of persisting independently. The
  control loop is a single per-connection read goroutine, so the buffer needs no
  lock.
- The buffer flushes as **one** `WriteSamples` through **one** persist slot. The
  agent's heartbeat opens each cycle, so a heartbeat flushes the previous cycle's
  burst; a size cap and connection teardown also flush, so buffered samples are
  never lost. With coalescing, per-connection write concurrency drops to about
  one flush per cycle, the four-slot pool stops saturating, and the tail-drop
  disappears.
- `ProcessReport`'s row upsert stays on its own write to the RLS process table;
  only its rank-numeric samples join the coalescing buffer.
- The fleet-health badge query uses `last_over_time(<selector>[10m])` instead of
  a bare instant selector, so a brief gap between low-rate anomaly summaries
  never blanks the badge. This is defense-in-depth: with coalescing the series is
  roughly one sample per minute and even a bare query resolves.
- A Grafana drop-ratio alert on `edge_telemetry_drops_total` /
  `edge_telemetry_ingested_total` catches any regression rather than absorbing it
  silently.

## Consequences

- The tail-ordered anomaly summary is persisted, not dropped; the badge shows a
  live rate on a steady connection.
- Telemetry persistence is delayed until the next heartbeat (bounded by the
  heartbeat interval); the badge lookback absorbs that staleness.
- Fewer, larger VictoriaMetrics writes per connection, which scales better than
  one write per message.

## Alternatives considered

- **Reserve a persist slot for `AgentHealthSummary`** — smallest, most targeted
  fix, but a band-aid: host-window thinning remains and per-type special-casing
  does not scale as telemetry types grow. Recorded as the minimal-diff fallback.
- **Bounded queue with backpressure** — most general (FIFO fairness, no silent
  drop), but its backpressure can delay other control messages if VM is slow;
  deferred to a dedicated review.
- **Widening only the badge query** — refuted by live investigation: the series
  was starved at the persist stage, not merely stale, so no lookback alone
  rescued a series receiving ~3 samples per day.
