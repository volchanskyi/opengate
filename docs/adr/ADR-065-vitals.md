---
number: 65
title: What the agent measures and reports
---

# ADR-065 — What the agent measures and reports

## Context

Every dimension an agent reports becomes a series per machine in the metrics
store. Across a fleet, the number of distinct series — not how often they are
sampled — is what decides whether the store holds up.

## Decision

**One reading a minute centrally, sampled every second locally.** The agent
folds its own one-second samples into a 60-second window. The detail stays on
the machine ([ADR-052](ADR-052-agent-local-store.md)); the minute goes central.

**Extremes travel beside averages, on four dimensions** — processor, memory,
disk fullness and network. An average minute hides the spike that is the whole
reason anyone is looking.

**Disk fullness is the fullest mount**, and a separate count says how many
mounts are critical. A pool average says nothing useful about a machine whose
`/var` is full.

**A cap of 24 series per device**, and a server-side allowlist on every label an
agent can influence. The cap is the budget; the allowlist is what stops an agent
inventing a label that multiplies it.

**Stall readings come straight from the kernel's pressure accounting** — five of
them, each the 60-second average of its own line. Linux only, with no invented
equivalent elsewhere: a host whose kernel does not publish pressure reports
these as absent, never as zero, because zero means nothing was stalled. A
containerised agent reads its own control group rather than the host's. A stall
reading's minute carries its latest value, not the mean of the means.

**Disk performance comes from per-device kernel counters** — how long an
operation took, its worst value inside the minute, and how deep the queue was.
Reduced worst-device per reading, independently: the slowest service time and
the deepest queue may be different disks, and saying so is more useful than
picking one disk and reporting both of its numbers.

There is deliberately no busy-percentage reading. It is the obvious counterpart
to processor utilisation and it is misleading on anything with a queue depth
above one, which is every modern disk.

**Never a wrong number.** A counter that went backwards, a device that vanished,
a window with no operations in it — each reports absent. A containerised agent
reports disk performance unsupported, because the kernel's counters are the
host's.

## Consequences

Readings publish at thousandths, because a healthy NVMe answers in well under a
millisecond and whole milliseconds would round every healthy disk to zero.
