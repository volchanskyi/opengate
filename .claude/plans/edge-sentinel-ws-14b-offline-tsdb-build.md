# WS-14b — Build the multi-tier persistent local TSDB (chosen substrate)

**Objective:** Implement the production agent-local multi-tier persistent TSDB on the **substrate
chosen by the WS-14a spike** (candidates: append-only / `redb` / `fjall` / `tsink` / no-persist;
`redb` leads on predictability, `tsink` could remove the bespoke layer if its 0.2.x durability holds).
T0 1 s / T1 1 min / T2 1 hr,
on-the-fly downsampling, inline anomaly scores, durable across restart/offline — the
full-resolution sovereign copy that holds **min/max/last + 1 s raw** (per the avg-only-central
cardinality decision).

**Dependencies:** **WS-14a (spike must pass + substrate chosen)**, WS-2 (sampler/ensemble — this
**supersedes** its ephemeral-RAM T0). **Blocks:** WS-15. **Wave:** Phase 5, after 14a.

## Context

Per the cardinality decision, VM stores **avg only**; the local store is the **only** home for
min/max/last and 1 s raw, fetched on-demand (WS-15). So this store is load-bearing, and its
robustness was de-risked in 14a. It shards cardinality **per host** (each agent indexes only its own
metrics) — the central >20k limiter remains VM-side, not here.

## Decisions (locked; carried from 14a)

- **Substrate = the 14a winner** (build the bespoke tiering+compression layer on top for
  append-only / `redb` / `fjall`; **skip it** if `tsink` wins, since it ships its own).
- Multi-tier (T0/T1/T2), **on-the-fly downsampling**, configurable **disk cap** + coarsest-first
  eviction, **compression to the 14a-measured density**, **inline anomaly-score column**.
- **Host-disk courtesy:** cap = `min(configured, % host-free)`; hard cap; never fills the host disk;
  agent never crashes.
- **Store format versioned + migrates across agent updates** (read-old/migrate; never orphan the
  WS-15 backlog).
- **Crash/corruption tolerant** (use the substrate's repair primitives); bounded data-loss window.
- On **deprovision** (WS-20), purge the local store completely on next reconnect.

## File inventory

- **Create:** `mesh-agent-core/src/tsdb/` — `store.rs`, `tiers.rs` (T0→T1→T2 rollup), `compress.rs`
  (delta/Gorilla-style, density per 14a), `retention.rs` (disk-cap eviction), `cursor.rs` (durable
  backfill cursor for WS-15), `anomaly.rs` (inline scores).
- **Modify:** `mesh-agent-core/src/ml/` sampler + ensemble — write raw **and** anomaly scores to the
  store; detection reads T0 + recent context.
- **Modify:** [`main.rs`](../../agent/crates/mesh-agent/src/main.rs) — open/recover on start
  (default-off flag), under `OPENGATE_DATA_DIR`; enforce the disk cap.

## Steps (TDD-first)

1. **Test first:** store write/read + on-the-fly tier rollup → implement on the chosen substrate.
2. **Test first:** compression meets the 14a-measured density; non-finite rejected → implement.
3. **Test first:** disk-cap eviction (coarsest-first, cap never exceeded) + host-disk-pressure backoff.
4. **Test first:** crash/restart recovery + corruption tolerance (truncate-and-continue); cursor durable.
5. **Test first:** inline anomaly scores persisted + read back; ML reads past context.
6. **Test first:** store-format version migration across an agent update.
7. Wire sampler→store; ARM + footprint benchmark recorded; default-off until it passes.

## Reviewer checklist

- [ ] Built on the 14a-chosen substrate; only the tiering+compression layer is bespoke.
- [ ] Multi-tier + on-the-fly downsampling; density meets 14a numbers; disk-cap eviction; host-disk-safe.
- [ ] Crash/corruption tolerant; cursor durable; format migration across updates; inline anomaly scores.
- [ ] Deprovision purge hook (WS-20); default-off until footprint benchmark passes; `/precommit` green.

## Verification

`cd agent && cargo test -p mesh-agent-core` + clippy `-D warnings`; footprint/compression bench
recorded (ARM64 + Windows). `/precommit` green. `/docs`: agent architecture page.
