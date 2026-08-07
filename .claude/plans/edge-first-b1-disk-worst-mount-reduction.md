# EF-B1 — Disk reduction fix: worst mount + `disk.mounts_critical`

**Master plan:** `edge-first-telemetry-and-investigations.md` §2.4, §3.1, §6.2, step 5.
**Acceptance criteria owned:** **B3**.
**Dependencies:** none.
**Blocks:** EF-B2 (the ≤ 24 cap test must count `disk.mounts_critical`).

## Context — a shipped RMM defect, not a design gap

[`disk_used_percent`](../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L72) is fed by a fold that
**sums every disk's bytes first** and divides once
([sampler.rs:79](../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L79), consumed at
[:245](../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L245)) — a capacity-weighted average.

FS01 (120 GB system volume at 98 %, 2 TB data volume at 10 %) therefore reports **15.0 %**. The
volume is about to fill, and `disk.used` is one of only three metrics the shipped rule vocabulary
supports ([alert_rules.go:61](../../server/internal/agentapi/alert_rules.go#L61)) — so **no
threshold rule can fire**. Servers are worst affected, because a small OS volume beside large data
volumes is the normal shape.

The fix needs **no new collector**: the code already iterates every disk. It is a pure O(1)
reduction — two honest numbers replacing one meaningless one.

## Design (fixed by the master plan — do not re-derive)

| Vital | Definition |
|---|---|
| `disk.used_percent` | **worst mount** — `max` over per-mount `(total − free) / total` |
| `disk.mounts_critical` | count of mounts ≥ **90 %** used |

`disk.used_percent` keeps its name and changes **meaning**. That is a deliberate correctness change
and must be called out in the ADR and the docs (EF-Z1 owns the ADR; this plan owns the docs line).

## File inventory

- **Modify:** [sampler.rs](../../agent/crates/mesh-agent-core/src/ml/sampler.rs) — per-mount
  reduction, new `disk_mounts_critical` field on `MetricSample`.
- **Modify:** [store_sink.rs](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs) — append
  `SERIES_DISK_MOUNTS_CRITICAL` (**next free id, never renumber**), extend `BACKFILL_SERIES`,
  `series_dim_name`, `dim_series`, `record`, and set its fixed-point scale.
- **Modify:** [host_metric_stream.rs](../../agent/crates/mesh-agent-core/src/ml/host_metric_stream.rs)
  — `DIMS` follows `BACKFILL_SERIES.len()`; the new dim rides the window automatically once the
  reading array is extended.
- **Modify:** [backfill.rs](../../agent/crates/mesh-agent-core/src/ml/backfill.rs) — the same dim
  must roll identically, or live and backfilled points land in different series.
- **Regenerate:** [testdata/golden/](../../testdata/golden/) `control_agent_metric_window*` fixtures
  (both directions).
- **Docs:** [Monitoring.md](../../docs/Monitoring.md), [Wire-Protocol.md](../../docs/Wire-Protocol.md).

## Steps (TDD-first)

1. **Test first — the B3 fixture:** two mounts, 120 GB @ 98 % and 2 TB @ 10 % → assert
   `disk_used_percent ≈ 98` and `disk_mounts_critical == 1`. **Assert the current code fails it**
   (it returns ≈ 15 and 0) before touching the source; that failing run is the evidence B3 is real.
2. **Test first — the boundary:** a mount at exactly 90.0 % counts; 89.99 % does not. Pick `>=` and
   pin it, so the threshold is a test, not a reading of the code.
3. **Test first — degenerate inputs:** zero-byte mount (`total == 0`) is excluded, not counted as
   100 %; `free > total` clamps to 0 (the existing
   [clamp test](../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L388) must keep passing);
   **no mounts at all** yields `None`/absent rather than 0 — a host with nothing mounted is not a
   host with empty disks.
4. Implement the reduction in `sampler.rs`; keep `disk_used_percent(total, free)` as the per-mount
   helper so its existing unit tests stay meaningful.
5. **Test first:** `series_dim_name`/`dim_series` round-trip for the new id, and `BACKFILL_SERIES`
   contains it exactly once.
6. **Test first:** a live-streamed window and a reconnect-backfilled rollup of the **same** 1 Hz
   input produce byte-identical values for the new dim (the existing live/backfill equivalence
   invariant — extend it, do not add a parallel test).
7. Regenerate goldens; `make golden` green both directions.
8. Docs.

## Traps

- **`SeriesId`s are a persistence contract.** They key rows in the on-disk redb store
  ([edge-tsdb](../../agent/crates/edge-tsdb/)), so an agent upgrading in place reads its old rows
  with the old ids. Append the next free id; never renumber, never reuse.
- Choose the fixed-point scale deliberately: `PERCENT_SCALE` (centi) suits a percentage, but
  `mounts_critical` is a **count**. A scale of 1 is exact and cheaper; whatever is chosen, the
  encode/decode round-trip must be lossless and asserted.
- The reduction runs over the same `sysinfo` disk list already collected — do not add a second
  enumeration pass, and do not filter by filesystem type here (that is a per-entity collector
  concern, deferred by §4.2).
- Network mounts and removable media count if `sysinfo` lists them. That is the honest reading; do
  not silently exclude them, and say so in the docs.

## Out of scope

Disk **performance** (`await_ms`, `queue_depth`) — EF-B5. Per-mount detail series — deferred
program (§4.2). The cadence change and the cap test — EF-B2.

## Reviewer checklist

- [ ] B3 fixture asserts ≈ 98 / 1 and was proven to fail before the fix.
- [ ] 90 % boundary, zero-byte mount, `free > total`, and no-mounts cases are all tested.
- [ ] New `SeriesId` appended (not renumbered); scale round-trips losslessly.
- [ ] Live stream and reconnect backfill agree byte-for-byte on the new dim.
- [ ] Goldens regenerated, `make golden` green both directions.
- [ ] Docs state the new meaning of `disk.used_percent` explicitly.

## Verification

`cd agent && cargo test -p mesh-agent-core`, `make golden`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
