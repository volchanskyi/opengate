# WS-14a — Local TSDB spike (substrate bake-off, evidence-first)

**Objective:** A local persistent time-series store is a **storage-engine project**, not a feature
slice. Before committing (WS-14b), prototype three substrates against **one fixture corpus + fixed
gates** and pick the simplest that clears the bar. Nothing depends on the local store until this
spike passes.

**Dependencies:** WS-2 (sample shapes). **Gates:** WS-14b, and by extension WS-15 (backfill),
WS-20 (edge erasure), and the offline-charts promise — none may depend on a committed local TSDB
until 14a passes. **Wave:** Phase 5 (before 14b).

## Why a spike

The hard part isn't append/read; it's the failure modes a storage engine exists to handle: crash
safety, corruption recovery, write amplification, compaction, cross-platform fsync/locking, and
hitting Netdata-class density (~0.6–1 B/sample) — cross-platform, in one workstream. The
**avg-only-central** cardinality decision makes the local store **load-bearing** (it is the only home
for min/max/last + 1 s raw), so it must be proven, not assumed.

## Candidates

For **A / B / B2** the *same bespoke tiering+compression layer* is built on top of the substrate;
**D** ships its own (the substrate *is* the engine — no bespoke layer); **C** is the no-persist
baseline.

| Opt | Substrate | Shape | License / Rust | Crash-safety | Density | Key risk |
|---|---|---|---|---|---|---|
| **A** | custom append-only chunk files | bespoke | n/a / pure-Rust | self-built | best (~0.6–1 B) | highest bespoke LOC — we own all crash code |
| **B** | **`redb`** + compressed chunks | COW B-tree | MIT-Apache / pure-Rust ✅ | inherited (2-phase commit, quick-repair, `check_integrity`) | good (~1–3 B) | write-amp under append; COW free-page bloat |
| **B2** | `fjall` + compressed chunks | LSM-tree | MIT-Apache / pure-Rust ✅ | inherited (WAL replay + immutable SSTables) | good | background compaction → CPU/tail-latency spikes (fights the agent CPU budget); newer (v3) |
| **D** | `tsink` (purpose-built TSDB lib) | LSM + Gorilla/delta/RLE | MIT / Rust (⚠️ zstd = C — confirm feature-gated) | inherited (segmented WAL) | best (~0.4 B/pt claimed) | **maturity (0.2.x, single-author)**; carries PromQL/protocol/RBAC/cluster surface the edge never uses |
| **C** | no persistent engine / bounded RAM-spill | — | — | n/a | n/a | loses offline history; forecloses true min/max + deep backfill |

**Edge-substrate rubric (why these five — and what's excluded on category, not quality).** A viable
agent-local store must be (1) **embeddable in-process** (no daemon), (2) **permissive-licensed**
(passes [agent/deny.toml](../../agent/deny.toml)), (3) **pure-Rust / no-CGO** so it cross-compiles to
Windows-msvc + Linux ARM64, (4) **minimal-footprint** (<1 MB-RSS class), (5) **crash-safe**. This
rules out: **server-class TSDBs** — TDengine (AGPL + `taosd` daemon, Linux/macOS-only), InfluxDB,
Timescale, Prometheus-TSDB — fail (1); **multi-model app DBs** — SurrealDB (BSL-1.1 until 2030;
graph/vector/SurrealQL/auth surface) — fail (2) and (4); **C-cored embedded stores** — SQLite,
RocksDB, LevelDB — fail (3).

**`redb` leads, for stated reasons** (not familiarity): no background threads ⇒ predictable write
latency that won't fight a live control/session under the <1 % CPU budget; single-file; most-mature;
simplest crash story; `Durability::None` fast writes flushed by a periodic durable commit (bounded
loss window). **`fjall`** challenges it on the one axis redb is weakest — **write-amplification on
append-heavy monotonic keys** (LSM is near-optimal there) — but at our scale (hundreds of series/s,
chunk-blobbed) that axis is largely neutralized while fjall's compaction-thread CPU spikes count
against the budget. **`tsink`** is the **highest-upside / highest-risk** entry: the only candidate
that is *already a TS engine* (could collapse the bespoke tiering+compression layer entirely and hit
the best density), gated by an unproven 0.2.x durability story and a server-grade surface the edge
doesn't need. **A vs B/B2/D** is decided by measured write-amp + CPU-predictability + crash survival
+ maturity — not reputation.

## Coverage (use cases the spike must exercise)

- **Durability:** clean restart + cursor resume; `kill -9` mid-write **and** mid-compaction → bounded
  loss + repair time; power-loss/torn-page sim → no corruption or auto-repair; bit-flip/truncate
  injection → integrity-check/quarantine, **never panic the agent**; disk-full mid-write → graceful,
  cap = `min(configured, % host-free)`, **never fills the host disk**.
- **Platform:** cross-compile + run on **Linux ARM64, Windows-msvc, macOS**; Windows file-locking +
  antivirus interference + `ProgramData` path; macOS `F_FULLFSYNC`. CI uses captured fixtures +
  simulated faults; one Windows runner does the real integration pass (no manual box).
- **Time:** NTP step back/forward, suspend/resume, timezone change → tier bucketing stays correct.
- **Concurrency:** ML detection + on-demand pull read **while** the sampler writes (MVCC, non-blocking).
- **Lifecycle:** weeks-long accumulation → coarsest-first eviction, footprint bounded; **store-format
  migration across agent updates** (read-old/migrate, don't orphan the backlog); **deprovision wipe**
  (WS-20) complete + secure.
- **Resource:** RSS (mmap vs heap), CPU yields to control/session, cold-start open time vs store size.

## Acceptance gates (measured, on ARM64 + Windows, same corpus)

- **bytes/sample** (vs ~0.6–1 B target) and **write-amplification** (the A-vs-KV decider).
- **CPU predictability under sustained ingest** — p99 ingest pause and any background-compaction
  stalls (the axis that separates redb's no-thread model from the LSM candidates fjall/tsink).
- **startup repair time** after `kill -9`; **corruption behavior** (recover/quarantine, never panic).
- **ingest CPU**, **RSS**, **range-query latency** (for on-demand pull).
- **implementation complexity / LOC** — bespoke engine = permanent maintenance liability; an external
  engine **inverts** this (low bespoke LOC, but dependency risk on an unproven crate).
- **Maturity / dependency sub-gate (D especially):** for any external engine record version,
  author/maintenance health, format-stability guarantees, and **confirm no C dependency leaks into
  the cross-compile** (e.g. tsink's zstd must be feature-gated to a pure-Rust/Gorilla-only path, or D
  fails (3)). A `0.2.x` single-author engine must clear the *full* crash/corruption gauntlet before
  it can be load-bearing.
- Existing "<1 % CPU, <1 MiB RSS" targets are **provisional until measured**.

## Steps

1. Build a **fixture corpus** (realistic metric shape × long duration) replayable in CI.
2. Implement the **same tiering+compression layer** over A, B, and B2; wire D (`tsink`) via its own
   engine (no bespoke layer); keep the C baseline.
3. Build a **fault-injection harness** (kill, torn write, truncate, disk-full, clock jump).
4. Measure all gates; record results.
5. **Decision + ADR** (substrate choice + rationale + numbers). Decision rule: **simplest that clears
   the bar.**

## Reviewer checklist

- [ ] A, B, B2, D, C prototyped on one fixture corpus; fault-injection harness covers crash/corruption/disk-full.
- [ ] Gates measured on ARM64 + Windows: bytes/sample, write-amp, CPU-predictability/compaction stalls, repair time, RSS, query latency, LOC, maturity/dependency.
- [ ] Decision recorded in an ADR with numbers; simplest option that clears the bar chosen.
- [ ] No WS-14b/15/20/offline-charts work merged before this passes.

## Verification

Spike artifacts: fixture corpus, harness, measured table, ADR. `cargo test` for the prototype
layer + fault injection. `/precommit` green on whatever lands (harness + chosen-substrate skeleton).
`/docs`: an ADR for the local-TSDB substrate.
