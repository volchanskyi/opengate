---
adr: 057
title: Live Host-Metric Streaming to Central VictoriaMetrics
status: Accepted
date: 2026-07-24
---

# ADR-057: Live Host-Metric Streaming to Central VictoriaMetrics

## Status

Accepted.

## Context

The host system-resource series (`cpu.total`, `mem.used_percent`,
`disk.used_percent`, `net.rx_bytes`, `net.tx_bytes`) are sampled every second and
written to the agent-local store. They must also chart **live** on the central
Telemetry pane for a continuously-connected device — not only after a
reconnect-backfill or an on-demand deep-history pull. The reconnect-backfill path
already defines the central shape: 10 s-average points on the
`opengate_edge_metric_avg{dim}` series, keyed by a 10 s window and valued `sum/n`.

## Decision

Stream host metrics live over the existing `AgentMetricWindow` control message,
reusing the server ingestion ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md))
and the frontend family charts unchanged.

- The sampler folds its 1 s samples into a **10 s-aligned average** window and
  emits one `AgentMetricWindow` per closed window over a bounded channel, drained
  on the control-loop heartbeat. The channel drops under pressure, so a burst
  never backpressures the control stream.
- The live averaging is the **same computation** as reconnect-backfill's 10 s
  roll-up — shared window key (`floor(ts/10)*10`) and `sum/n` — so a live point
  and a later gap-filled point for the same `(dim, ts)` are equal and land in one
  series. An invariant test asserts the two paths agree for all five dims.
- Network bytes stream **cumulatively**, exactly as backfill writes them. A
  per-interval throughput series is deferred to a later change that spans the
  live, backfill, and frontend paths together under a new dim name.
- The partial (still-open) window is **discarded across maintenance** and is
  never emitted on its own; backfill fills any window that never closed.

## Consequences

- The Telemetry pane charts live cpu/mem/disk/net within ~1 min of a device
  connecting, closing the gap where a continuously-connected device showed no
  live telemetry.
- One bounded channel and one 10 s window per device add negligible control
  traffic; the sampler already computes the samples, so there is no new sampling
  cost.
- Because live and backfilled points are byte-identical, a reconnect after an
  offline gap produces a continuous series with no seam.
- The numeric channel now carries host resource metrics directly, keeping the
  endpoint-log model ([ADR-048](ADR-048-edge-sentinel-endpoint-log-model.md))
  purely raw-log and edge-first.
