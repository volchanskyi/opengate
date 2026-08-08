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
`disk.used_percent`, `net.rx_bps`, `net.tx_bps`, `disk.mounts_critical` — the net
dims are primary-interface throughput in bytes/second, and the disk dims are the
fullest mount's used percentage and the count of mounts at or above the
critical-usage threshold) are sampled every second and written to the
agent-local store. They must also chart **live** on the central
Telemetry pane for a continuously-connected device — not only after a
reconnect-backfill or an on-demand deep-history pull. The reconnect-backfill path
already defines the central shape: windowed points on the
`opengate_edge_metric_avg{dim}` series, keyed by the window start and valued
`sum/n`.

## Decision

Stream host metrics live over the existing `AgentMetricWindow` control message,
reusing the server ingestion ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md))
and the frontend family charts unchanged.

- The sampler folds its 1 s samples into a **60 s-aligned** window and emits one
  `AgentMetricWindow` per closed window over a bounded channel, drained on the
  control-loop heartbeat. The channel drops under pressure, so a burst never
  backpressures the control stream. The cadence, the window maxima that ride
  along, and the bounded dim vocabulary are
  [ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md).
- The live fold is the **same computation** as reconnect-backfill's roll-up —
  shared window key (`floor(ts/60)*60`), `sum/n`, and the same extremum — so a
  live point and a later gap-filled point for the same `(dim, ts)` are equal and
  land in one series. An invariant test asserts the two paths agree for every
  dim, maxima included.
- The net dims stream **primary-interface throughput** in bytes/second, exactly
  as backfill writes them, rounded to whole bytes so they stay on the lossless
  integer path and the two paths' averages match byte-for-byte.
- A dim a sample could not read — a net rate with no computable interval, or the
  disk reduction on a host with no measurable mount — is **absent** from the
  window rather than substituted, and backfill leaves the same gap.
- The partial (still-open) window is **discarded across maintenance** and is
  never emitted on its own; backfill fills any window that never closed.

## Consequences

- The Telemetry pane charts live cpu/mem/disk/net within ~1 min of a device
  connecting, closing the gap where a continuously-connected device showed no
  live telemetry.
- One bounded channel and one open window per device add negligible control
  traffic; the sampler already computes the samples, so there is no new sampling
  cost.
- Because live and backfilled points are byte-identical, a reconnect after an
  offline gap produces a continuous series with no seam.
- The numeric channel now carries host resource metrics directly, keeping the
  endpoint-log model ([ADR-048](ADR-048-edge-sentinel-endpoint-log-model.md))
  purely raw-log and edge-first.
