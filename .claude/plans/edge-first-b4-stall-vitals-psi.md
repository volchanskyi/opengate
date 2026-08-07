# EF-B4 — Stall vitals from kernel pressure (PSI), Linux only

**Master plan:** `edge-first-telemetry-and-investigations.md` §5.3 (C), §6.2, §6.3, §6.3.2, §14.1,
D23, step 7.
**Acceptance criteria owned:** **B5**, **B11**.
**Dependencies:** EF-B2 (the vitals contract and the cap test).
**Blocks:** EF-B5 (which asserts the exact 24-series count once both Linux sets exist).

## Context

PSI is the ideal edge reduction: **the kernel has already collapsed the dimension**, so a stall
vital costs one file read and zero cardinality. Each vital is "percent of time tasks were stalled"
in `[0,100]`, read from `avg60` — which matches the 60 s vitals cadence exactly.

**Five vitals, Linux only:** `stall.cpu.some`, `stall.mem.some`, `stall.mem.full`, `stall.io.some`,
`stall.io.full`. `cpu.full` is omitted because the kernel defines it as always zero.

**No analogue is synthesized for a platform without PSI** (D23). Counters that measure different
things in different units, published as `stall.*`, would repeat the FS01 defect — one name, two
meanings, silently. Such a platform keeps `cpu.total.max`, its own event rules (EF-B6) and the disk
reductions, and its gap is reported as `unsupported`, never implied.

*Extension point:* a Windows agent would add its own stall-class vitals under Windows-native names
and report `unsupported` for `stall.*` — the support-state machinery built here is what carries
that answer.

**Containers read their own cgroup** (§6.3.2): when `/proc/self/cgroup` shows a non-root path, PSI
comes from the agent's cgroup (`…/cpu.pressure`, `memory.pressure`, `io.pressure`) so a containerized
agent measures its own pressure, not the host's.

## File inventory

- **Create:** `agent/crates/mesh-agent-core/src/ml/pressure.rs` — PSI reader, environment detection,
  support state.
- **Modify:** [ml/mod.rs](../../agent/crates/mesh-agent-core/src/ml/mod.rs) — register the module.
- **Modify:** [sampler.rs](../../agent/crates/mesh-agent-core/src/ml/sampler.rs) — carry the five
  readings on `MetricSample` as `Option`s.
- **Modify:** [store_sink.rs](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs) — five
  appended `SeriesId`s, dim names, `BACKFILL_SERIES`, scale.
- **Modify:** [host_metric_stream.rs](../../agent/crates/mesh-agent-core/src/ml/host_metric_stream.rs)
  — an absent reading emits **no dim**, never a zero.
- **Create:** fixture tree under the crate's test data — synthetic `/proc/pressure/*` and cgroup
  files.
- **Regenerate:** [testdata/golden/](../../testdata/golden/) metric-window fixtures.
- **Docs:** [Monitoring.md](../../docs/Monitoring.md) — the vitals table and the platform gap.

## Steps (TDD-first)

1. **Test first (B5):** parse a fixture `/proc/pressure/cpu` and `/proc/pressure/memory` containing
   real kernel text (`some avg10=0.00 avg60=1.23 avg300=… total=…`, plus a `full` line) → assert the
   parsed `avg60` equals the file's value within tolerance, for every one of the five vitals, and
   that `cpu`'s `full` line is **ignored**.
2. **Test first — malformed input** (I5): truncated file, missing `avg60`, non-numeric field, a
   `full` line absent on a kernel that omits it, and a value > 100 → each yields `None` or a clamped
   value, never a panic and never a partially-applied reading.
3. **Test first (B11):** with the fixture root missing `/proc/pressure` entirely, the reader reports
   `Unsupported`, the sampler emits **no** `stall.*` dims (assert absence, not zero), and the support
   state is available for coverage accounting.
4. **Test first — container detection (E26 half):** a fixture `/proc/self/cgroup` with a **non-root**
   path routes the read to that cgroup's `*.pressure` files; a root path (`0::/`) reads
   `/proc/pressure/*`. Assert the *chosen path*, not just the value — the whole point is that a
   containerized agent never reports the host's pressure as its own.
5. Implement `pressure.rs` behind an injectable filesystem root so **every test uses a fixture
   directory and none reads the host's `/proc`** — the reference host has PSI, so a host-reading
   test would pass locally and prove nothing about a host without it.
6. Wire into the sampler and the vitals contract; five new `SeriesId`s appended.
7. No-PSI path: the module reports `Unsupported` when the pressure files are absent — assert it
   against a fixture root with no `*.pressure` files, so the answer is proven by an executed test on
   the platform CI builds rather than by inspection of a target CI never compiles.
8. Regenerate goldens; docs.

## Traps

- **Absent ≠ zero.** A host without PSI must emit no series at all. A zero would read as "never
  stalled" — the silent-wrong-answer class this program exists to remove.
- PSI `avg60` is already a 60 s average; do **not** average it again in the windower. Take the last
  reading of the bucket (or state and test whichever rule is chosen — but not a double average).
- `#[cfg]`-gated code is invisible to coverage and mutation on the other platform. Keep the parser
  itself platform-neutral (it parses text) and gate only the path resolution, so the bulk stays
  tested everywhere.
- `SeriesId`s are a persistence contract — append only.
- Cap arithmetic: after this plan a Linux host emits 21 of 24. EF-B5 completes it.

## Out of scope

`disk.await_ms` / `queue_depth` and the cgroup `io.stat` fallback — EF-B5. Any synthesized stall
analogue for another platform — forbidden by D23. Rules that *watch* these vitals — EF-B8/EF-B9.

## Reviewer checklist

- [ ] Five vitals, `cpu.full` excluded, values parsed from `avg60`.
- [ ] Every test runs against a fixture root; no test reads the host's `/proc`.
- [ ] Missing PSI → dims **absent** (asserted), support state `unsupported`.
- [ ] Non-root cgroup routes to the agent's own pressure files, asserted by path.
- [ ] Malformed input never panics and never yields a partial reading.
- [ ] The no-PSI answer is an executed fixture test, not an assumption.

## Verification

`cd agent && cargo test -p mesh-agent-core`, `make golden`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
