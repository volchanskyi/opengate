---
adr: 052
title: Edge Sentinel Local TSDB Build (WS-14b)
status: Accepted
date: 2026-07-10
---

# ADR-052: Edge Sentinel Local TSDB Build

## Status

Accepted. Realises the forward guidance of
[ADR-051](ADR-051-edge-sentinel-local-tsdb-substrate.md) (the WS-14a substrate
bake-off) and gates WS-15 (backfill) and WS-20 (edge erasure). Live emission
stays **default-off** until the ARM64 + Windows footprint gate and the WS-15b
soak pass.

## Context

The avg-only-central cardinality decision
([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md)) makes an
agent-local store **load-bearing**: it is the only home for min/max/last + 1 s
raw that central VictoriaMetrics does not keep. WS-14a chose the substrate —
**`redb`** plus a bespoke tiering/compression layer — and measured a compact
block codec and big-block packing on a fixture corpus. WS-14b graduates that
spike crate ([`agent/crates/edge-tsdb`](../../agent/crates/edge-tsdb)) into the
production store and wires it into the agent sampler.

## Decision

The production store is `store::LocalTsdb` on `redb`, built by reusing — not
rewriting — the spike's `compact` codec and `tier` rollups. `redb` owns the
crash code; only the tiering + compression layer on top is ours.

### Storage model

- **Three tiers, separate `redb` tables, one atomic transaction.** T0 (1 s raw +
  inline anomaly bit) as big compact blocks; T1 (60 s) and T2 (3600 s) rollups
  (min/max/avg/last/count). Each commit writes T0 and its rollups in a single
  `redb` transaction, so a chunk and its rollups land or roll back together.
  `Durability::Full` → `redb` `Immediate` (fsync); `None` is the buffered fast
  path (never fsync-per-sample).
- **Fixed-point-per-metric value codec** (the measured density lever): each
  value stored as `round(value × scale)`, delta-of-delta coded — **lossless to
  1/scale**. Percentage gauges use ×100 (centi precision); the adaptive selector
  keeps the smallest of {fixed-point, float32-XOR, lossless int-DoD} per block
  and tags it, so a series that packs better as float32 is never made worse.
  Blocks self-describe their scale, so a policy change never breaks old blocks.
- **Sample-timestamp-keyed rollups, merged incrementally.** Rollups store `sum`
  and `last_ts` (not `avg`) so two partial rollups of one bucket — samples split
  across commits, or a late NTP-corrected sample — **merge exactly and
  order-independently without re-reading the raw T0 series**. Bucketing is by
  `floor(ts/interval)`, never arrival, so an NTP step cannot misbucket.
- **Big-block T0 packing** (~3000 samples/block) so `redb`'s per-key B-tree
  overhead amortises; the growing tail block is rewritten each commit until it
  seals.

### Production surface

- **Durable backfill cursor** — a per-series `redb` table WS-15 advances and
  resumes from across restarts, so the backlog is never re-shipped or orphaned.
- **Coarsest-first disk-cap eviction.** A hard cap on *logical* bytes (sum of
  stored block bytes) — `redb` reuses freed pages, so the file tracks that plus
  bounded COW overhead and never grows without bound. Eviction removes the
  **globally-oldest** block first; because a coarser tier's block keys are
  aligned to a coarser boundary they sort oldest, so on a timestamp tie the
  coarsest tier (T2, then T1, then T0) is dropped first. The newest raw block is
  always retained so live sampling is never evicted. The effective cap is
  `min(configured, host_free × fraction)`; the agent feeds host-free bytes so a
  nearly-full host disk shrinks the store further — it never fills the host.
- **Format version + forward migration.** The store stamps a format version;
  opening one written by an older agent migrates it forward and re-stamps, and a
  future-version store is refused with a graceful error (the agent recreates the
  cache). A corrupt file also fails to open gracefully — never a panic — so the
  device stays online.
- **MVCC snapshot reads.** Detection/backfill read a consistent snapshot that is
  unaffected by the sampler's concurrent writes (`redb` MVCC), so the
  read-while-writing path is free.
- **Deprovision purge** (WS-20): `purge()` clears every tier and cursor; the
  store lives under the agent data dir, so agent uninstall removes it too.

### Optional cold-tier DEFLATE (no zstd)

Sealed (non-tail) T1/T2 blocks can be DEFLATE-compressed
(`compact_cold_tiers()`, pure-Rust `flate2`/`miniz_oxide`, already in the agent
lock — zero new crates, no CGO). It is opt-in and off the hot path: T0 raw is
**never** deflated, and only cold reads pay inflate CPU, so it stays inside the
<1 % CPU budget. `zstd` is deliberately not used — WS-14a measured a C `zstd`
dependency buys no more over the bit-packed codec than pure-Rust DEFLATE, so it
is unjustified. DEFLATE is a compile feature (`cold-deflate`) defaulted **on**
so a store is never written by a build that cannot read it back.

### Crate graduation

The WS-14a bake-off substrates — bespoke append-only files (A), the no-persist
control (C), and the small/big-block `redb` references (B/B+) — plus the fault
harness are retained behind the default-on `bakeoff` feature as the measured
off-ramp reference and the `cargo bench` corpus. `mesh-agent` and
`mesh-agent-core` depend on the crate with `default-features = false` (plus
`cold-deflate`), so none of that ships in the binary — only `redb` and `flate2`
do. The shipped agent gains its first `redb` dependency here; it passes the
strict [`deny.toml`](../../agent/deny.toml) cleanly (both crates MIT/Apache,
`flate2` already in the lock).

## Measured results (x86_64 Linux)

`cargo bench -p edge-tsdb`, 40 series × 6 h = 864 000 samples, fixed-point ×100:

| Metric | Measured |
|---|---|
| logical B/sample (T0+T1+T2, fixed-point) | **~1.87** |
| logical B/sample after cold-tier DEFLATE | **~1.70** |
| codec: fixed-point + DEFLATE (per raw sample) | **~1.0** (sub-1 B class) |
| crash-recovery open (reopen 864 k samples) | ~1.1 ms |
| range query | ~0.59 ms |

The multi-tier logical **~1.87 B/sample** is already below WS-14a's ~2.4 target
and well under any fleet-tier disk budget, so the append-structured off-ramp
(ADR-051) is **not** triggered. The `redb` file size at spike scale is
region-quantised and floor-inflated; the logical bytes are what the cap bounds
and what converges at fleet scale.

## Consequences

- The sampler task persists each raw sample with its ensemble anomaly bit into
  the store on every agent, capped at a fixed footprint limit. The store is a
  cache: an open failure recreates it and, failing that, degrades to log-only —
  it never aborts the agent.
- The footprint/recovery numbers here are x86_64 Linux; ARM64 + Windows
  footprint/recovery (Windows file-locking, `F_FULLFSYNC`) remain unmeasured, an
  open follow-up tracked in [`techdebt.md`](../../.claude/techdebt.md). The
  always-run gate tests
  (persistence, precision, atomic rollups, eviction, cursor, anomaly/MVCC,
  migration, cold-tier, crash-recovery) run cross-platform-agnostically on every
  commit.
- WS-15 builds backfill on the cursor + MVCC snapshot; WS-20 calls `purge()`.

Working plan:
[edge-sentinel-ws-14b-offline-tsdb-build.md](../../.claude/plans/archive/edge-sentinel-ws-14b-offline-tsdb-build.md).
