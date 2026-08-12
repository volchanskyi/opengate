---
adr: 067
title: "Disk-Performance Vitals from Per-Device Kernel Counters, Linux Only"
status: Accepted
date: 2026-08-10
---

# ADR-067: Disk-Performance Vitals from Per-Device Kernel Counters, Linux Only

## Status

Accepted.

## Context

The vitals contract measured one thing about disks: how full they are. Nothing
measured how *slow* they are, and the two are unrelated facts about different
entities — capacity belongs to a mount, service time to a physical device.

That gap is a whole class of ticket nobody could close. DAL-WS-012's NVMe wears
out over a fortnight: service time drifts from 2 ms to 40 ms while capacity is
flat, CPU is idle and memory is fine. The user reports "it's slow", every gauge
the fleet collects says the machine is healthy, and the technician eventually
reimages a perfectly good operating system. FS01's 02:00 backup is the mirror
case: the data device runs 28 I/Os deep while answering each one in 3 ms —
saturated and entirely healthy — and an operator with no queue reading has no way
to tell that apart from a device in trouble.

The kernel already counts what is needed, per device: completed I/Os, the
milliseconds spent servicing them, and time-weighted outstanding I/O. These are
the counters `iostat` derives `await` and `avgqu-sz` from.

## Decision

**Three vitals, Linux only**: `disk.await_ms` (average time one I/O took),
`disk.await_ms.max` (its within-minute peak), and `disk.queue_depth` (average
outstanding I/Os). With these a Linux device occupies **exactly the 24-series
cap** ([ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md)).
The reserved headroom is now spent, which is the intended friction: the next
vital of any kind is a decision about the cap rather than an addition that fits
quietly.

**No busy-percentage vital.** It is the obvious CPU-utilization analogue and it
is the wrong metric for this fleet. SSD and NVMe service many I/Os in parallel,
so a busy-time percentage pins at 100 % with substantial headroom remaining — a
confident, constant, meaningless saturation. Queue depth keeps scaling where it
saturates, so "busy but healthy" stays distinguishable from "overloaded". The
omission is recorded here so it is not added later for symmetry with `cpu.total`.

**Worst device, per vital, independently.** The highest service time and the
deepest queue routinely name different devices, and each vital answers its own
question; a mean across an idle data disk and a struggling system disk describes
neither. This is the worst-mount reduction
([ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md)) applied
on the axis where the entity is a device. Per-device detail rides alert evidence,
so the reduction costs no central cardinality.

**The device filter is the kernel's own block-device listing**, which excludes
partitions by construction, minus `loop*`, `ram*` and `zram*`. `dm-*` and `md*`
are **included deliberately**: LUKS and RAID overhead is latency the user
actually experiences, and worst-of selection cannot double-count the way summing
would.

**Service time ships a maximum; queue depth does not.** The same arithmetic that
justifies `cpu.total.max`: a five-second I/O freeze inside a minute moves a 3 ms
average to 69 ms and pins the maximum at 800. That pairing is what turns "the
desktop froze at 12:05" into "the freeze was I/O-bound", which is a different fix
from a CPU-bound one. Queue depth is already a time-weighted average over the
interval rather than an instantaneous reading, so a maximum of it answers nothing
its own value does not.

**A virtual machine needs no special handling, and guest-observed latency is the
reading worth having.** It already includes host contention and volume
throttling, which is exactly what makes the customer's application slow. A cloud
volume pinned at its provisioned IOPS cap shows service time climbing while the
queue backs up and throughput flatlines against a ceiling — a diagnosis capacity
monitoring cannot reach.

**A containerized agent reports these vitals as unsupported.** The kernel's disk
counters are not per-container, so an agent inside one reads host-wide figures
and would attribute its neighbours' I/O to itself. cgroup I/O accounting carries
bytes and I/O counts but **no service time**, so there is no honest substitute to
fall back to, and none is invented. What the container keeps is a genuine
I/O-stall signal through `stall.io.*`
([ADR-066](ADR-066-stall-vitals-from-kernel-pressure.md)), read from its own
cgroup.

**Never a wrong number.** A counter that went backwards (a reboot or a wrap), a
device that appeared or disappeared between two readings, and an interval with no
wall time yield no reading for that device rather than a negative or astronomical
rate — the contract the network rates already keep. A device that completed no
I/O has **no** service time: reporting 0 ms would read as "instantaneous", the
opposite of the truth, and would drag the fleet's idea of normal service time
toward zero on every quiet host. An empty queue, by contrast, is a real
measurement of nothing waiting.

**Readings publish at milli resolution.** A healthy NVMe answers an I/O in a
fraction of a millisecond and queue depth is fractional, so the centi scale the
percentage gauges use would round both into something else. Readings are
quantized to the scale as they are produced, so the number the local store holds,
the number a live window averages and the number alert evidence quotes are the
same one.

**Every path resolves under an injectable filesystem root**
([`diskperf.rs`](../../agent/crates/mesh-agent-core/src/ml/diskperf.rs)), so a
container, a kernel that publishes no per-device counters, both column layouts
the kernel has used, and every malformed line are ordinary fixture directories.
No test reads the host's own `/proc` or `/sys`: the reference host is bare-metal
Linux, so a host-reading test would pass and prove nothing about the environments
this decision is mostly about. The "am I containerized" question is now resolved
in one place ([`cgroup.rs`](../../agent/crates/mesh-agent-core/src/ml/cgroup.rs))
and shared with the stall reader, so two collectors cannot disagree about it.

## Consequences

The fleet gains the signal that separates a slow machine from a full one, at one
file read and one directory listing per second and no central cardinality.
DAL-WS-012's wearing NVMe is visible as a fortnight-long drift days before the
user complains; FS01's backup reads as a deep queue at healthy service time
rather than as an incident.

The per-device cap is now fully spent, and the vitals contract is closed to
additions until someone re-opens the cap deliberately.

Coverage becomes load-bearing in a second place: a device with no
disk-performance vitals is a container or a platform that cannot supply them, and
any rule watching these dims has to distinguish that from a device that is simply
healthy.
