---
adr: 065
title: "The Vitals Contract: 60 s Cadence, Extrema, and a Bounded Dim Vocabulary"
status: Accepted
date: 2026-08-08
---

# ADR-065: The Vitals Contract — 60 s Cadence, Extrema, and a Bounded Dim Vocabulary

## Status

Accepted.

## Context

The edge is the consumer of high-resolution telemetry; central holds the small,
constant-size set of numbers a fleet view and an alert need. What binds the
central store is **cardinality** — the number of distinct series — not the rate
at which any one of them is written. A device streaming every second and a device
streaming every minute cost the same in series; only the second one costs six
times less in samples for a fleet view that reads minute-scale windows anyway.

Two properties of the pre-existing shape worked against that.

**Averaging destroys the signal a stall produces.** Central kept `avg` alone, on
the argument that min/max/last are each their own series and would multiply
cardinality. That argument holds for keeping *all four* aggregates on *every*
dim. It does not hold for the specific case the operator cares about: inside a
60 s window sampled at 1 Hz, five seconds pinned at 100 % move a 20 % average to
26.7 % — indistinguishable from noise — while the maximum reads 100. Without an
extremum the freeze is unobservable centrally at any cadence, so raising the
sample rate would not have recovered it.

**The dim label was agent-controlled.** The ingest path copied
`AgentMetricWindow.Dims[].Name` straight into the VictoriaMetrics `dim` label
with no allowlist and no bound, and reconnect backfill did the same with
`BackfillSample.Name`. The number of central series was therefore a property of
what agents sent rather than of what the fleet agreed to send: one misbehaving or
compromised agent could multiply a whole tenant's series count, and a
cardinality budget measured against well-behaved agents would have been measuring
the wrong thing.

## Decision

A fixed **vitals contract**: what a device may write centrally, at what cadence,
and how much of the store it may occupy.

- **Cadence 60 s centrally.** The live windower folds 1 s samples into 60 s
  windows and emits one per minute. Local sampling stays at 1 Hz — the edge keeps
  the resolution; only the central stream slows.
- **Extrema beside averages, on four dims.** `cpu.total`, `mem.used_percent`,
  `net.rx_bps`, and `net.tx_bps` each ship a companion `.max` carrying the
  window's largest reading. `disk.used_percent` moves too slowly for a
  within-minute peak to mean anything and `disk.mounts_critical` is already a
  threshold count, so neither has one. With the five stall vitals of
  [ADR-066](ADR-066-stall-vitals-from-kernel-pressure.md) that is fifteen dims of
  `opengate_edge_metric_avg`.
- **A per-device cap of 24 series**, counted as the metric dims plus the
  node-wide anomaly rate plus the five per-family rates. Today a Linux device
  occupies 21; the remaining 3 are reserved for the disk-performance vitals. The
  next vital of any kind past that spends headroom that no longer exists and
  re-opens the cap.
- **A server-side allowlist** on both write paths — live windows and reconnect
  backfill, which write the same series. A name outside the vocabulary is dropped
  and counted under
  `opengate_edge_telemetry_drops_total{reason="unknown_dim"}`, so central
  cardinality is a compile-time constant of the server rather than an input.
- **Backfill rolls to the same 60 s grid**, and takes each bucket's maximum from
  the **stored** rollup rather than recomputing it from the averages it just
  read. A max-of-averages is a different and smaller number, and it hides exactly
  the stall the maximum exists to show.

The agent builds the vocabulary from one series mapping
([`store_sink.rs`](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs)); the
server holds the allowlist
([`vitals.go`](../../server/internal/agentapi/vitals.go)); the cross-language
golden fixture for a metric window pins the two together, and the per-device cost
is measured against a real VictoriaMetrics in
[`spike_test.go`](../../server/tests/vmcardinality/spike_test.go).

## Consequences

- A five-second freeze is visible centrally, on a stream that costs a sixth of
  what the 10 s stream cost in samples. That is the trade the whole decision
  turns on: resolution where averaging destroys the signal, not resolution
  everywhere.
- Chart bands are min/max across 60 s averages, and the band provenance the API
  reports says so (`avg_of_60s`). The narrowest bucket a metrics window can
  request is 60 s, because a finer one would ask the store for detail it does not
  hold.
- Central cardinality no longer depends on agent behavior. A window of a thousand
  invented dims creates zero series and reports one drop.
- Adding a vital is now a decision with a visible price: the cap is asserted, so
  the eleventh dim fails a test rather than quietly growing the store.
- No percentiles. The local rollup holds min/max/sum/last/count and nothing
  percentile-shaped, and a t-digest would be a new persistence format for a
  number no rule asks for.

## Alternatives rejected

- **Keep 10 s and add extrema.** Six times the samples for a fleet view read at
  minute scale, and the extremum — not the cadence — is what recovers a stall.
- **Centralize all four aggregates.** Four series per dim against a cap of 24 is
  the multiplication the avg-only decision
  ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md)) exists to avoid.
- **Bound the dim label by length or count instead of by name.** A bound on shape
  still lets an agent choose distinct names, which is what creates series; only a
  fixed vocabulary makes the count a constant.
