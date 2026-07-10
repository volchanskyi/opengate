# WS-14b — Build the multi-tier persistent local TSDB (redb + compact codec)

**Objective:** Promote the WS-14a spike crate [`agent/crates/edge-tsdb`](../../../agent/crates/edge-tsdb)
into the production agent-local multi-tier persistent TSDB on **`redb`** (chosen by WS-14a,
[ADR-051](../../../docs/adr/ADR-051-edge-sentinel-local-tsdb-substrate.md)). T0 1 s / T1 1 min / T2 1 hr,
on-the-fly downsampling, inline anomaly scores, durable across restart/offline — the full-resolution
sovereign copy that holds **min/max/last + 1 s raw** (per the avg-only-central cardinality decision).

**Dependencies:** **WS-14a — DONE** (spike passed; substrate = redb; compact codec + tier rollups +
fault harness already built and 94%-tested in `edge-tsdb`). WS-2 (sampler/ensemble — this
**supersedes** its ephemeral-RAM T0). **Blocks:** WS-15. **Wave:** Phase 5, after 14a.

## Context

Per the cardinality decision, VM stores **avg only**; the local store is the **only** home for
min/max/last and 1 s raw, fetched on-demand (WS-15). So this store is load-bearing, and its
robustness was de-risked in 14a. It shards cardinality **per host** (each agent indexes only its own
metrics) — the central >20k limiter remains VM-side, not here.

**This is mostly a graduation, not a greenfield.** WS-14a landed a tested, substrate-independent core
that carries in unchanged: the compact block codec, the tier rollups, the redb store, the fixture
corpus, and the fault-injection harness. WS-14b promotes it, chooses per-metric precision, wires it to
the sampler, and adds the retention/migration/cursor production surface.

## Decisions (locked; measured in 14a)

- **Substrate = `redb`** ([ADR-051](../../../docs/adr/ADR-051-edge-sentinel-local-tsdb-substrate.md)):
  inherited COW crash-safety (2-phase commit / quick-repair / `check_integrity`), MVCC snapshot reads
  (the WS-15 read-while-sampling path is free), no background threads, zero transitive deps, passes
  the strict [`deny.toml`](../../../agent/deny.toml). `fjall`/`tsink` were rejected on the measured
  dependency sub-gate (3rd `hashbrown` major → `multiple-versions=deny` + compaction threads). The
  bespoke tiering+compression layer sits **on top of** redb (redb owns the crash code, not us).
- **Block format = the compact codec**
  ([`compact.rs`](../../../agent/crates/edge-tsdb/src/compact.rs)), **not** f64 Gorilla — ~2× denser:
  - **Fixed-point-per-metric values are the default** (the measured density lever): store
    `round(value × scale)` as an integer, delta-of-delta encoded — **lossless to 1/scale precision**,
    **1.10 B/sample** (7% under float32-XOR *and* more controlled loss). `scale` is chosen **per metric
    family** (e.g. ×100 for percentages/load; ×1 for integral counters, already lossless int-DoD). The
    adaptive selector keeps the smallest of {fixed-point int-DoD, float32-XOR} per block.
  - **Implicit fixed-step timestamps** (Netdata's design): regular cadence stores only `first_ts` +
    `step`; per-point timestamps are free, NTP steps/gaps become sparse exceptions (lossless in time).
  - **Inline anomaly bit** per sample, RLE-packed — ~free (+0.02 B/sample). This *is* the "anomaly
    scores stored inline" requirement.
- **Big-block packing** (~3000 samples/block, Netdata's extent idea): one redb key → one fat block, so
  redb's per-entry B-tree overhead amortises. Measured: halves redb's steady-state footprint
  (~4.9 → **~2.4 B/sample**), level with the original append-only-f64 while keeping redb's crash-safety.
- **Optional cold-tier DEFLATE**, T1/T2 **only** — pure-Rust (`flate2`/`miniz_oxide`, **already in the
  agent lock**, no CGO, zero new deps). Fixed-point + DEFLATE reaches **0.99 B/sample** (sub-1 B).
  **Do NOT add zstd** — measured to buy only ~3% over the compact codec and no better than pure-Rust
  deflate, so a C dependency is unjustified. DEFLATE adds decompress CPU on every read, so it is gated
  on the **<1 % CPU budget** and applies to cold tiers, never hot T0.
- **Tiers T0/T1/T2 = separate redb tables**, written in **one atomic transaction** (redb commits all
  tables together), so a T0 chunk + its T1/T2 rollups + anomaly scores land or roll back as a unit.
  Rollups ([`tier.rs`](../../../agent/crates/edge-tsdb/src/tier.rs)) are keyed by **sample timestamp**,
  not arrival — an NTP step can never misbucket (already tested).
- **Durability:** redb `Durability::Immediate` for durable commits (bounded-loss boundary);
  `Durability::None` fast path for hot writes, flushed by a periodic durable commit (bounded loss
  window). Never fsync-per-sample.
- **Host-disk courtesy:** cap = `min(configured, % host-free)`; hard cap; **never fills the host disk**;
  agent never crashes. Coarsest-first eviction (drop oldest T2, then T1, then T0 chunks).
- **Store format versioned + migrates across agent updates** (read-old/migrate; never orphan the
  WS-15 backlog).
- On **deprovision** (WS-20), purge the local store completely on next reconnect.
- **Off-ramp (measured, not a preference):** a bespoke append-structured engine (~1.3 B/sample) is only
  warranted if redb's **~2.4 B/sample** — after fixed-point + tiering — still blows the smallest fleet
  tier's disk budget in the WS-15b soak. redb's residual ~1.9× page overhead is a B-tree property big
  blocks do not remove; DEFLATE-cold-tier is the cheaper lever to try first.

## Measured baselines (14a; provisional production targets)

| Metric | Measured | Source |
|---|---|---|
| bytes/sample, redb + compact (steady state) | ~2.4 B | `cargo bench -p edge-tsdb` |
| bytes/sample, fixed-point codec alone | 1.10 B | compressor bake-off |
| bytes/sample, fixed-point + cold-tier DEFLATE | 0.99 B | compressor bake-off |
| float32 error (when float32 path used) | 5.7e-8 rel | codec bake-off |
| crash-recovery open time (`kill -9`) | ~2.7 ms | fault harness |
| range-query latency (redb) | ~135 µs | bench |

The existing always-run gates (torn-tail recovery, bit-flip quarantine, disk-full cap, clock-jump,
density, tolerance) in [`edge-tsdb/tests`](../../../agent/crates/edge-tsdb/tests) are the regression floor;
WS-14b extends them, it does not replace them.

## File inventory

- **Graduate:** promote [`agent/crates/edge-tsdb`](../../../agent/crates/edge-tsdb) to the production
  local-store crate. Keep `compact.rs`, `tier.rs`, `redb_compact.rs` (→ the production store),
  `frame.rs`/`crc.rs`/`bitio.rs`/`gorilla.rs` (codec deps), `corpus.rs` + `fault.rs` (as test
  fixtures/harness). Move the bake-off-only substrates — `append_only.rs` (A) and `baseline.rs` (C) —
  behind a `bench`/`test` feature (retained as the off-ramp reference + comparison, not shipped).
- **Extend the store:** add per-metric `scale` (fixed-point policy), the durable **backfill cursor**
  (for WS-15), **retention/eviction** (coarsest-first under the disk cap), **format version + migration**,
  and **optional cold-tier DEFLATE** on T1/T2 (feature-gated, CPU-budget-checked).
- **Modify:** `mesh-agent-core/src/ml/` sampler + ensemble — write raw **and** anomaly bits to the
  store; detection reads T0 + recent context (redb MVCC snapshot read while the sampler writes).
- **Modify:** [`main.rs`](../../../agent/crates/mesh-agent/src/main.rs) — depend on the store crate;
  open/recover on start (default-off flag) under `OPENGATE_DATA_DIR`; enforce the disk cap. (The shipped
  `mesh-agent` binary gains the redb dependency here for the first time — it was isolated to the spike
  crate until now.)

## Steps (TDD-first)

1. Graduate the crate; gate the A/C substrates behind a feature; `mesh-agent(-core)` depends on it;
   `/precommit` still green (redb now in the shipped binary's tree — re-check `deny.toml`/audit).
2. **Test first:** per-metric fixed-point `scale` (lossless to 1/scale; adaptive vs float32) → implement.
3. **Test first:** T0/T1/T2 atomic multi-table commit + on-the-fly rollup (sample-ts-keyed) → implement.
4. **Test first:** disk-cap eviction (coarsest-first, cap never exceeded) + host-disk-pressure backoff.
5. **Test first:** durable backfill cursor (survives restart; resumes where WS-15 left off).
6. **Test first:** inline anomaly bits persisted + read back; ML reads past context via an MVCC snapshot.
7. **Test first:** store-format version + migration across an agent update (read-old → migrate).
8. **Test first (measure):** optional cold-tier DEFLATE on T1/T2 hits the 0.99 B target and stays within
   the CPU budget on read; **no zstd** → implement, feature-gated.
9. Wire sampler→store; ARM64 + Windows footprint/recovery benchmark recorded; default-off until it passes.

## Reviewer checklist

- [ ] Built on **redb** by graduating `edge-tsdb`; only the tiering+compression layer is bespoke; redb
      owns the crash code. A/C substrates retained behind a feature as the off-ramp reference.
- [ ] Value codec is **fixed-point-per-metric** (lossless to 1/scale), adaptive vs float32; implicit
      timestamps; **inline anomaly bit**; big-block packing.
- [ ] T0/T1/T2 written atomically (one redb txn); rollups sample-ts-keyed; density meets the 14a
      numbers (~2.4 B, or ~1 B with cold-tier DEFLATE); **no zstd / no C dependency**.
- [ ] Disk-cap coarsest-first eviction; host-disk-safe; crash/corruption tolerant (redb repair);
      durable cursor; format migration across updates.
- [ ] Deprovision purge hook (WS-20); default-off until the footprint benchmark passes; `/precommit`
      green including the new redb-in-shipped-binary `deny.toml`/audit pass.

## Verification

`cd agent && cargo test -p edge-tsdb` (graduated crate) + `-p mesh-agent-core` + clippy `-D warnings`;
footprint/compression/recovery bench recorded (ARM64 + Windows, real integration pass — Windows
file-locking + `F_FULLFSYNC`). `/precommit` green. `/docs`: agent architecture page + ADR-051 kept
current.
