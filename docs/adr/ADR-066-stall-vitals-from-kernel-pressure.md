---
adr: 066
title: "Stall Vitals from Kernel Pressure, Linux Only"
status: Accepted
date: 2026-08-10
---

# ADR-066: Stall Vitals from Kernel Pressure, Linux Only

## Status

Accepted.

## Context

A resource can be at 40 % and still be the reason an application is unusable.
Contoso's terminal server shows 38 % memory used and 55 % CPU all morning while
users report that saving a document takes twenty seconds: the machine is not out
of memory, it is spending a fifth of every minute reclaiming it, and every task
waits during that time. Nothing in the vitals contract
([ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md))
measures waiting. Utilization answers "how much of the resource is in use"; the
operator's question is "how much time did work spend blocked on it", and those
are different numbers that diverge exactly when it matters.

`cpu.total.max` recovers a five-second freeze inside a minute, which is real, but
it is still a utilization reading: it cannot see a machine that stalls
continuously without ever pinning a gauge, and it says nothing at all about
memory reclaim or I/O waits.

The Linux kernel already computes the missing number. Pressure stall information
publishes, per resource, the share of the last 10/60/300 s that tasks spent
stalled — the `some` line when at least one task was blocked while others ran,
the `full` line when every runnable task was blocked. It is a reduction the
kernel performs for free, and its 60 s window is exactly the cadence the vitals
contract publishes on.

## Decision

**Five stall vitals**, each the `avg60` field of its own line: `stall.cpu.some`,
`stall.mem.some`, `stall.mem.full`, `stall.io.some`, `stall.io.full`. CPU `full`
is not published — the kernel defines it as always zero, and a constant is not
worth one of the twenty-four series a device may occupy. With these, a Linux
device occupies 21 of that cap.

**Linux only, with no analogue invented anywhere else.** A platform with no
time-in-stall measurement of its own does not get one synthesized from counters
that measure something else in different units. Publishing such a number under a
`stall.*` name would put two meanings behind one name, silently — the same defect
class as a pooled disk average reported as "disk used". Such a platform keeps
every platform-neutral vital, its own event rules and the disk reductions; what
it lacks is the continuous stall gauge, and that gap is reported as unsupported.
A platform that later grows a genuine time-in-stall primitive adds its own
collector under this contract.

**Absent is absent, never zero.** A host whose kernel publishes no pressure ships
no `stall.*` dim at all — not a zero, and not a gap filled from anywhere. Zero is
the answer for a host that was measured and never stalled; a host that cannot
measure stalling has no answer, and the two must not read alike. The same holds
per vital: a kernel that publishes only the `some` line ships only that vital.

**A containerized agent reads its own cgroup.** When `/proc/self/cgroup` names a
non-root unified cgroup, the source is that cgroup's `cpu.pressure`,
`memory.pressure` and `io.pressure`. `/proc/pressure` is host-wide, so a
container reading it would report its neighbours' stalls as its own. If the
cgroup publishes no pressure files there is deliberately **no fallback** to the
host's: the answer is unsupported.

**A stall vital's 60 s bucket carries its latest reading, not the mean of the
bucket.** Every other series is an instantaneous gauge whose minute is summarized
by its average. A stall reading is already the kernel's own 60 s average, so the
last reading of a bucket *is* that bucket's answer, while the mean of sixty
overlapping 60 s averages spreads the minute across two and damps exactly the
stall the vital exists to show. The rule lives in one place
([`store_sink.rs`](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs)) and
is applied by the live windower and by reconnect-backfill alike, so a live point
and a gap-filled point for the same `(dim, ts)` stay identical. The hourly rollup
spans sixty kernel windows rather than one, so it summarizes by mean like every
other series.

**Every path is resolved under an injectable filesystem root**
([`pressure.rs`](../../agent/crates/mesh-agent-core/src/ml/pressure.rs)), so a
host without pressure, a container, and a malformed kernel file are ordinary
fixture directories. The reference host has pressure; a test that read its
`/proc` would pass and prove nothing about the hosts this decision is mostly
about. The parser stays platform-neutral — it parses text, and a platform without
the files simply resolves nothing — so no branch of this code is invisible to
coverage on any build.

A value outside `[0, 100]`, a non-numeric field, a missing line and a truncated
file all yield no reading rather than a clamped or partial one, for the same
reason a wrong number is worse than a missing one.

## Consequences

The fleet gains the signal that distinguishes a busy machine from a stuck one,
at one file read per second and no cardinality: the kernel performed the
reduction. Contoso's terminal server now reads `stall.mem.some` near 20 while
memory utilization sits at 38 %, which is the fact that matches what users
report.

Three series of the per-device cap remain, reserved for the disk-performance
vitals. After those the next vital of any kind re-opens the cap — the intended
friction.

Fleet coverage becomes something that must be reported rather than assumed: a
device with no stall vitals is a device that cannot supply them, and the
distinction between unsupported and quiet is now load-bearing for any rule that
watches these dims.
