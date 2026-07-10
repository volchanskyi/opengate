---
adr: 051
title: Edge Sentinel Local TSDB Substrate (WS-14a spike)
status: Accepted
date: 2026-07-09
---

# ADR-051: Edge Sentinel Local TSDB Substrate

## Status

Accepted. Gates WS-14b (build the store), WS-15 (backfill), WS-20 (edge erasure),
and the offline-charts promise — none may depend on a committed local TSDB until
this decision is recorded.

## Context

The **avg-only-central** cardinality decision ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md))
makes an agent-local store **load-bearing**: it is the only home for min/max/last
+ 1 s raw history that central VictoriaMetrics does not keep. A local persistent
time-series store is a storage-engine project — the hard part is crash safety,
corruption recovery, write amplification, and footprint, cross-platform — so
WS-14a is an evidence-first bake-off, not a feature slice. The candidate rubric
(embeddable in-process, permissive-licensed, pure-Rust/no-CGO, minimal footprint,
crash-safe) admitted five options: **A** bespoke append-only files, **B** `redb`,
**B2** `fjall` (LSM), **D** `tsink` (purpose-built TSDB), **C** no-persist control.

The spike lives in [`agent/crates/edge-tsdb`](../../agent/crates/edge-tsdb): a
deterministic fixture corpus, a shared Gorilla compression + tier-rollup layer,
substrates A/B/C behind one trait, and a fault-injection harness. Gate thresholds
are asserted in `tests/gates_test.rs` (always run); the full table is reproduced
by `cargo bench -p edge-tsdb`.

## Measured results

Corpus: 40 series × 3600 s = 144 000 samples of realistic host telemetry (sticky
gauges with idle plateaus + monotonic counters), lossless `f64`. Raw encoding
would be 16 B/sample (8-byte ts + 8-byte value).

| Substrate | B/sample | Write-amp vs A | Ingest | Commit (fsync) | Range query | Crash story | Bespoke crash LOC | Dep footprint |
|---|---|---|---|---|---|---|---|---|
| **A** append-only | **2.6** | 1.0× | 2.9 M samp/s | 0.9 ms | 325 µs | self-built (scan/repair/quarantine) | ~500 (owned) | 0 crates |
| **B** `redb` | 7.3 | ~2.8× | 34 M samp/s (buffered) | 2.9 ms | 135 µs | inherited (2-phase commit, quick-repair, `check_integrity`) | ~165 glue (0 crash) | 0 crates |
| **C** baseline | 0 (volatile) | — | 78 M samp/s | 0 | 30 µs | none (lost on restart) | — | — |

Crash-recovery (substrate A, `kill -9`-class tail truncation after a durable
commit): store reopens in ~2.7 ms, the durably-committed prefix (144 000 samples)
is fully preserved, best-effort tail beyond the last commit marker is dropped, a
mid-file bit flip is quarantined (chunk skipped, never a panic), and a byte cap
evicts oldest segments rather than filling the host disk.

**Maturity / dependency sub-gate (measured, not assumed):**

- **`redb` 4.1** — MIT/Apache, **zero transitive dependencies**, most mature of
  the set, no background threads.
- **`fjall` 3.1 / `tsink` 0.2** both pull `hashbrown 0.14.5`, a **third** hashbrown
  major alongside the workspace's 0.16/0.17, which fails the strict
  `multiple-versions = "deny"` policy in [`agent/deny.toml`](../../agent/deny.toml);
  each also adds ~15–90 crates (`dashmap`, `crossbeam`, `lz4_flex`, `memmap2`, …)
  and a background compaction thread that spends the agent's <1 % CPU budget.
  `tsink` resolves to 0.2.1 against a current 0.10.2 (single-author, format churn).
  Neither clears the dependency sub-gate as-is; both are excluded from the
  workspace on that measured basis, not on reputation.

## Decision

**Substrate B — `redb` — with the bespoke Gorilla tiering + compression layer on
top.** This is the *simplest option that clears every gate*:

- **Crash safety is inherited, not owned.** redb brings a two-phase commit,
  quick-repair, and `check_integrity`. The bespoke append-only substrate (A) is
  ~2.8× denser, but only by owning ~500 lines of segment framing, torn-tail
  repair, quarantine, and eviction code — a permanent maintenance liability and
  the exact risk the rubric flagged ("we own all crash code"). redb's ~165 lines
  are buffering + transaction glue with zero crash code.
- **Predictable latency under the CPU budget.** redb has no background threads,
  so ingest latency won't fight a live control/session — the axis that separates
  it from the LSM candidates (fjall/tsink) whose compaction threads spike CPU.
- **Passes the security surface cleanly.** Zero-dependency, pure-Rust, MIT/Apache;
  it does not perturb `deny.toml`, unlike fjall/tsink.
- **Density is acceptable and improves at scale.** The 7.3 B/sample in the table
  is floor-inflated at spike scale — redb over-allocates a ~1 MB minimum file, so
  small stores read artificially high. At steady state it is ~4.9 B/sample
  lossless, and the compact block format (see the Path-1 follow-up below) halves
  it again to ~2.4 while keeping redb's crash-safety. The ~1 B Netdata target is a
  block-encoding optimisation, not a substrate property.

Substrate A remains in the crate as the density/write-amp reference and the
fallback if redb's footprint later proves limiting; C stays as the control that
quantifies what persistence buys. The shared Gorilla + tier + frame layer is
substrate-independent and carries into WS-14b unchanged.

## Consequences

- WS-14b builds the multi-tier store (T0 1 s / T1 min / T2 hour, inline anomaly
  scores) on `redb`, reusing this crate's Gorilla codec, tier rollups, corpus,
  and fault harness; the bespoke-layer skip clause (had `tsink` won) does not
  apply.
- The `f64`-lossless density baseline (2.6 B best case) sets the WS-14b target:
  fixed-point gauge quantization is the lever to approach ~1 B, evaluated there
  against precision loss.

### Path-1 measurement follow-up (compact codec)

A second spike measured a Netdata/tsink-grade **compact block codec**
([`compact.rs`](../../agent/crates/edge-tsdb/src/compact.rs): float32 values +
implicit fixed-step timestamps + adaptive per-block codec — XOR32 Gorilla for
gauges, lossless integer delta-of-delta for counters — + an inline anomaly bit,
RLE-packed) and a **big-block redb store**
([`redb_compact.rs`](../../agent/crates/edge-tsdb/src/redb_compact.rs)).
Reproduce with `cargo bench -p edge-tsdb`; gated by `tests/compact_test.rs`.

- **The compact encoding roughly halves density and is substrate-independent:**
  **1.26 B/sample** vs 2.47 for f64 Gorilla (**~2×**), float32 error **5.7e-8
  relative** (negligible for telemetry), counters bit-exact (adaptive selector
  picks int-DoD), the inline anomaly bit +0.02 B/sample. This is the WS-14b
  density lever, and it applies to *either* substrate.
- **redb's true steady-state cost is lower than the floor-inflated snapshot:**
  once data outgrows redb's ~1 MB fixed floor (≥ ~6 h of 40-series data),
  small-block f64 redb is **~4.9 B/sample** and **big-block compact redb is
  ~2.4 B/sample** — the two levers together halve it, landing it level with the
  original append-only-f64 (2.6) while keeping redb's crash-safety/MVCC. This
  **erases append-only's headline footprint advantage** at equal encoding.
- **redb keeps a residual ~1.9× page overhead** over the raw compact encoding
  that big blocks do not remove (COW free pages + page fragmentation), so redb
  will not reach the ~1.3 B an append-structured store would, nor Netdata's
  ~0.6 B (which also adds ZSTD). **Sharpened off-ramp:** substrate A earns a
  switch only if, after the compact codec + tiering, redb's ~2.4 B/sample still
  blows the smallest fleet tier's disk budget in the WS-15b soak — a measured
  number, not a preference.
- **WS-14b default becomes redb + the compact block format** (Path 1), not the
  f64 Gorilla blocks: same substrate, ~2× denser blocks, inline anomaly bit for
  free. Adding pure-Rust ZSTD over the block is the next measured lever toward
  the ~0.6–1 B class.
- The fault-injection harness and gate tests are the regression guard for WS-14b:
  crash-recovery, corruption-quarantine, disk-full-cap, and clock-jump behaviour
  are asserted, always-run, on every commit.
- Real ARM64 + Windows integration passes (Windows file-locking, `F_FULLFSYNC`)
  remain a WS-14b release gate; the CI harness here runs the simulated-fault pass
  cross-platform-agnostically.
