# EF-B5 — Disk-performance vitals: `await_ms`, `await_ms.max`, `queue_depth` (Linux only)

**Master plan:** `edge-first-telemetry-and-investigations.md` §2.4, §6.2, §6.3.1, §6.3.2, D37, D38,
E26–E29, step 8.
**Acceptance criteria owned:** **B15**, **B16**, **B17**, **B18**, **B19** — plus the **exact
24-series count**, because this is the last vital set to land.
**Dependencies:** EF-B2 (contract + cap), EF-B4 (the other Linux-only set; the exact count can only
be asserted once both exist).
**Blocks:** nothing.

## Context — today there is no disk *performance* signal at all

`MetricSample` carries `disk_used_percent` and nothing else about disks
([sampler.rs:23-40](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L23-L40)). Capacity answers
"is it full"; **nothing answers "is it slow"**. `sysinfo 0.39`'s `Disk::usage()` gives bytes, not
service time.

`/proc/diskstats` carries what is needed and was verified live on the reference host: per-device
completed I/Os, ms spent reading and writing, in-flight count and weighted ms — the inputs `iostat`
derives `await` and `avgqu-sz` from. `/sys/block/` was verified to list whole devices only (no
partitions), and cgroup v2 is live with both `io.stat` and `io.pressure`.

**`%util` is excluded by decision (D37).** The fleet is purely SSD/NVMe, which services many I/Os in
parallel, so `%util` pins at 100 % with substantial headroom remaining. Recording the omission here
so nobody adds it later for symmetry with `cpu.total`.

## The three vitals (fixed)

| Vital | Derivation | Catches |
|---|---|---|
| `disk.await_ms` | `Δ(ms_read + ms_write) / Δ(reads + writes)` | Average service time per I/O — a wearing, thermally-throttled or GC-stalling device shows here while capacity and throughput look normal |
| `disk.await_ms.max` | max over the bucket's 1 Hz samples | A 5 s I/O stall barely moves a 60 s average but pins `max` |
| `disk.queue_depth` | `Δ(weighted_ms) / Δ(wall_ms)` (`iostat`'s `avgqu-sz`) | Honest saturation where `%util` would saturate |

**Worst device, per metric, independently.** The highest-`await` device and the highest-`queue_depth`
device may differ; each answers its own question. Per-device detail rides alert evidence (EF-C1),
never central series.

## File inventory

- **Create:** `agent/crates/mesh-agent-core/src/ml/diskperf.rs` — parser, device filter, worst-device
  reduction, environment detection.
- **Modify:** [ml/mod.rs](../../../agent/crates/mesh-agent-core/src/ml/mod.rs),
  [sampler.rs](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs),
  [store_sink.rs](../../../agent/crates/mesh-agent-core/src/ml/store_sink.rs),
  [host_metric_stream.rs](../../../agent/crates/mesh-agent-core/src/ml/host_metric_stream.rs).
- **Create:** fixture trees — bare-metal, VM guest (`vda`), container (non-root cgroup), wrap/reset,
  and mid-window disappearance.
- **Regenerate:** [testdata/golden/](../../../testdata/golden/) metric-window fixtures.
- **Docs:** [Monitoring.md](../../../docs/infrastructure/Monitoring.md).

## Steps (TDD-first)

1. **Test first (B17) — the device filter:** a fixture containing `nvme0n1`, `nvme0n1p1`, `vda`,
   `loop0`, `ram0`, `dm-0` counts `nvme0n1`, `vda`, `dm-0` **exactly once each** and excludes the
   partition and the pseudo-devices. Membership of `/sys/block/` is the filter (it excludes
   partitions by construction), minus `loop*`, `ram*`, `zram*`. `dm-*` and `md*` are **included
   deliberately** — LUKS and RAID overhead is latency the user actually experiences, and worst-of
   selection cannot double-count the way summing would.
2. **Test first (B15) — worst device, independently per metric:** one device at 40 ms await /
   shallow queue, another at 2 ms await / deep queue → `disk.await_ms` = 40 and `disk.queue_depth`
   comes from the *other* device. A mean would report neither.
3. **Test first (B16):** a 60 s bucket where 5 samples show a stall → `disk.await_ms.max` ≈ the stall
   latency while `disk.await_ms` stays low (the disk analogue of B4).
4. **Test first (B19) — VM guest:** a guest fixture exposing `vda` produces all three vitals. Guest
   observed latency **is** the wanted signal: it includes host contention and volume throttling,
   which is exactly what makes the customer's application slow.
5. **Test first (B19, E29) — counter wrap or reset:** the sample yields `None`, never a negative or
   an enormous rate. **(E28)** a device present in the previous reading and absent now is dropped
   from the reduction with no rate computed across the gap — mirror
   [`byte_rate`](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L42)'s existing
   never-a-wrong-number contract rather than inventing a second convention.
6. **Test first — no I/O in the interval:** `Δ(reads + writes) == 0` yields `None`, not `0`. A quiet
   disk has no service time; reporting 0 ms would read as "instantaneous", the opposite of the truth.
7. **Test first (B18) — container:** with a non-root `/proc/self/cgroup`, `await_ms` and
   `queue_depth` report **`unsupported`**, `stall.io.*` still come from the cgroup's `io.pressure`
   (EF-B4), and **host-wide `/proc/diskstats` values never appear**. `/proc/diskstats` is not
   namespaced, so a container reading it attributes its neighbours' I/O to itself — assert the
   host-wide numbers are absent, not merely that some number was produced.
8. Implement `diskperf.rs` against an injectable filesystem root; **no test may read the host's
   `/proc` or `/sys`**.
9. **Test first — the exact series count:** a host emits exactly **24** central series, and a host
   without `/proc/diskstats` or PSI emits exactly **16** (the platform-neutral set) with the missing
   eight reported `unsupported`, never zero. This is the assertion EF-B2 deferred; it lands here
   because this is the last set.

   *Extension point:* a Windows agent would land in the second case until it supplies
   Windows-native equivalents under their own names.
10. Regenerate goldens; docs.

## Traps

- **Column count varies by kernel.** The reference host's `/proc/diskstats` has **20 columns**
  (discard + flush stats); older kernels have 14. Parse by position from the left and tolerate extra
  trailing fields; a fixed 14-field expectation silently mis-parses on modern kernels, and a
  `split_whitespace().nth(13)` on a short line silently reads the wrong number.
- `weighted_ms` is the **11th stat field** (14th column). Getting this wrong produces a plausible
  number, which is the worst failure mode here. Pin it with a fixture whose expected value is
  computed by hand in the test.
- Both `await` and `queue_depth` are **rates between two readings**. The first sample after start,
  after a device appears, or after a counter reset has no predecessor → `None`.
- Fixed-point scale: `await_ms` needs sub-millisecond resolution and `queue_depth` is fractional —
  neither is a percentage. Choose scales deliberately and assert lossless round-trip through the
  local store.
- `SeriesId`s are a persistence contract — append only.

## Out of scope

`%util` (D37). Per-device central series (§4.2 — the deferred collectors program). Alerting on these
vitals (EF-B8 adds them to the rule vocabulary).

## Reviewer checklist

- [ ] B15–B19 each have a named test; every one runs against a fixture root.
- [ ] Worst device chosen **independently** per metric.
- [ ] Container path proven to never surface host-wide figures.
- [ ] Wrap, reset, disappearance and zero-I/O all yield `None`, never a wrong number.
- [ ] Kernel column-count variants covered (14 and 20).
- [ ] Exact 24-series count asserted, and the 16-series no-source case with the missing eight
      `unsupported`.

## Verification

`cd agent && cargo test -p mesh-agent-core`, `make golden`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
