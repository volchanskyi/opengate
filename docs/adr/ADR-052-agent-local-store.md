---
number: 52
title: The agent keeps its own time-series store
---

# ADR-052 — The agent keeps its own time-series store

## Context

Central keeps one averaged reading a minute. Anything finer — the second the
processor spiked, the shape of a stall — exists only on the machine, and has to
survive the machine being offline and being switched off mid-write.

## Decision

**`redb` is the substrate**, chosen by measuring candidates rather than by
reading about them. It brings two-phase commit, so crash safety is inherited
rather than written; it runs no background threads, so latency stays inside the
agent's processor budget; it is pure Rust with no dependencies and a permissive
licence; and its density improves as the store fills.

**Three tiers in separate tables, written in one transaction** — raw seconds,
and two coarser rollups. One transaction means a crash leaves the tiers
consistent with each other.

**Values are fixed-point per metric**, which is the lever that actually moved
density, and raw samples are packed into large blocks of roughly three thousand
so the key index does not cost more than the data.

**A raw read opens only the blocks that reach into it.** Every raw block is
labelled with its earliest and latest second, and a read starts at the first
block ending at or after its start
([`raw.rs`](../../agent/crates/edge-tsdb/src/store/raw.rs)), so its cost
follows the span asked for, never how long the machine has kept history. A clock
set back widens a block's label and never hides a reading. Prometheus, InfluxDB
and VictoriaMetrics label their blocks with a first and last time the same way.
A store an older agent wrote has its blocks labelled once, when the updated agent
first opens it.

**Rollups are keyed by the sample's own timestamp and merged as they arrive**,
holding a sum and a count rather than a running average, so a late sample merges
correctly instead of dragging the average.

**The machine keeps the ranges the server confirmed.** A table beside the tiers
holds minute-aligned, coalesced ranges the server has confirmed, live or caught
up, at any resolution ([`confirmed.rs`](../../agent/crates/edge-tsdb/src/confirmed.rs)).
Catch-up skips every bucket inside one, so a machine that reconnects sends only
what never landed: its newest two days first, then older history, and never a
minute that has not ended. A confirmation is saved without forcing the disk and
made durable by the next forced save, so a machine crash repeats about a minute,
which the server's at-least-once delivery absorbs
([ADR-044](ADR-044-telemetry-ingest.md)). A request to send again from a time
forgets the confirmations after it. A store an older agent wrote has its per-tier
positions read once into confirmed ranges, and the positions are then removed
([`backfill/mod.rs`](../../agent/crates/mesh-agent-core/src/ml/backfill/mod.rs)).
Prometheus remote write keeps track of what its receiver accepted the same way,
and Netdata's receiver asks for the missing range; re-sending only the offline
gap was not taken.

**Time with nothing to send is marked done once read.** A stretch catch-up reads
and finds holding no reading the server lacks — a machine switched off, history
removed to stay under the cap — joins the confirmed ranges, up to the newest
minute the store has committed, since every reading still waiting in memory is
newer ([`drain.rs`](../../agent/crates/mesh-agent-core/src/ml/backfill/drain.rs)).
A request to send again forgets it like any other confirmation, and a clock set
back into marked time loses those readings, as it does for a confirmed minute.
Ranges that ended before the 30-day horizon are dropped as each one is saved, and
the list is loaded in one sorted pass.

**A catch-up keeps its place and reads without the lock.** The start-up check
finds the first batch owed and keeps it to send first; each later batch carries
on from the age band and position the last one stopped at, on its band's own
grid, so each stretch is read once per session
([`backfill_loop.rs`](../../agent/crates/mesh-agent/src/backfill_loop.rs)). The
store lock is held to take a read view and the confirmed ranges, then released:
`redb` gives every reader a view of its own that never blocks the writer, so the
once-a-second recorder never waits on a catch-up read. Prometheus remote write,
Vector's disk buffer and the OpenTelemetry Collector's disk queue carry on from a
saved position the same way; none reads its backlog to count it.

**The cap is on logical bytes and eviction is coarsest-first.** When the store
is full, the oldest raw seconds go before the rollups do — the shape of last
month matters more than the exact second.

**The store stamps a format version** and migrates forward, so an agent update
does not orphan what the machine already recorded.

## Consequences

A machine offline for days comes back with its own history intact and fills the
gap centrally, rather than leaving a hole nobody can reconstruct.
