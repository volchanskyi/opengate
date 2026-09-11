---
number: 119
title: Finding a leak in a long run
---

# ADR-119 — Finding a leak in a long run

## Context

The conservation reading says whether a run gave back what it took
([ADR-093](ADR-093-relay-session-lifetime.md)). It does not say where, and the
distance between the two is a working week: a red endurance run hands back one
number and an instruction to reproduce five hours of load with a profiler
attached. The run that produced the evidence is gone, the machine is destroyed,
and the next one is seven days away.

The answer was already on a port the harness reads. The server publishes its own
profiler on the cluster-only listener
([ADR-095](ADR-095-two-listeners.md)), and a goroutine profile is not a hint
about a leak of this shape — it is the finding: a count, and the stack it is
parked on.

## Decision

**A run asked for an interval takes the target's goroutine and heap profiles on
that interval, keeps every one, and reports the difference between consecutive
readings as growth at a named line.**

**Every profile is kept.** The finding is a difference, and a difference cannot
be recovered from a summary of either side. Sixty readings of both profiles
across a five-hour walk is about fifteen megabytes.

**They are kept as text the target has already resolved**, not as the binary
form. Resolving the binary form needs the exact binary that produced it, which
is destroyed with the job, and a profile nobody can resolve names no line at
all.

**The site is the first frame that is not standard library.** Every parked
goroutine's own top frame is the runtime parking it, so a site taken from the
top would report every leak in the product at the same line.

**A profile that could not be taken is counted, not skipped.** A page that is
not a profile parses as no stacks, and no stacks reads as nothing grew — the
healthiest answer a leak detector can give. Each reading is checked, every
unanswered fetch is counted, and fewer than two readings fails the run outright,
because one reading has no difference in it.

**Growth is reported with how many intervals it grew in**, which is what
separates a leak from a working set that got bigger once and then held.

### Following what holds an object

**The endurance run takes a core off the running server and walks what is
keeping the heaviest live types alive.** Go's heap profile records where an
object was born, and a leak is not about where something was made — it is about
what is still pointing at it.

**The target is built with its debugging information kept**, or the walk names
nothing.

**Every way the walk cannot happen is a failure, never an empty report.**

**The core is read where it is taken and never carried out.** It is most of the
process's memory and it is the machine's, not the artifact store's.

## Consequences

The trail is diagnostic and gates nothing: the conservation slope already
decides whether a run failed, and it decides it on a reading of the resource
rather than on a profiler's account of where allocations came from. What the
trail adds is the line to open once that verdict is red.
