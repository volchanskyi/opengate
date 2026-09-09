---
number: 104
title: A reading names whose room it measures
status: Accepted
date: 2026-09-08
---

# ADR-104 — A reading names whose room it measures

## Context

[ADR-100](ADR-100-a-bundle-field-is-a-reading-or-it-is-absent.md) made the
generator's room a reading instead of the literal `100` it had always been. The
first four nights it ran, it invalidated runs it should not have and passed runs
it should not have, and both failures came from the same two unstated
assumptions.

**When.** It was one look at `/proc/loadavg`, taken once, after the fleet had
already been wound down. On 2026-09-07 the four-leg scaling sweep and the volume
leg all connected every machine they asked for; two of the five came back at
`0.0%` headroom and three did not. The difference between them was which instant
the single sample landed on.

**Whose.** `/proc/loadavg` is not namespaced. Inside the staging generator pod
it reports the node's run queue, and that node carries production. The same
nightly that reads `0.0%` there is reading a figure that belongs to the
workloads beside it, under a field named for the generator.

The two mistakes point in opposite directions on the two venues, which is why
neither showed up as a consistent wrong answer:

| Venue | What the field claimed | What it was reading |
|---|---|---|
| Staging pod | the generator's own room | the whole node's run queue, production included |
| Throwaway runner | the generator's own room | the box the generator shares with the stack under test, at one instant |

On the throwaway venue there is a further problem, and it is not a defect in the
reading: the generator and the system under test are *meant* to share the box.
`scaling.yaml` and `volume.yaml` both declare no processor ceiling, because
driving the processor there is the experiment. A figure that says the box was
busy is a true statement about that venue and no statement at all about whether
the generator had room to produce the load.

## Decision

**A reading of room says whose room it is, and only a reading of the generator's
own room decides whether a run measured the generator.**

Two scopes, and the bundle carries which one it holds:

- `generator` — the generator was measured against an allowance of its own, read
  from its cgroup: the processor quota in `cpu.max`, the memory ceiling in
  `memory.max`, and the two accounts the kernel keeps in `cpu.stat` — the
  processor time the generator has spent, and the time it was runnable and
  refused. Verified live on the staging cluster: the load-test pod's limits of
  400 millicores and 384 MiB arrive as `cpu.max 40000 100000` and
  `memory.max 402653184`.
- `machine` — the generator has no allowance of its own, so it shares whatever
  the box has with the system it is measuring. The figure is carried as evidence
  and gates nothing.

Every figure is **bracketed around the load** rather than sampled after it. The
processor accounts are running totals, so the difference between the two ends is
the whole run; the box, which keeps no such total, is sampled on an interval and
reported as its mean commitment with the most memory it was ever holding.

Three rules fall on a `generator` reading and none on a `machine` one: below 20%
processor headroom, above 90% memory used, or more than 20% of the run spent
runnable and refused the processor. The last is new, and it is the one that
answers a question the other two cannot: a generator kept waiting measured its
own wait into every round trip it timed, whatever room it had left over. A
kernel that keeps no refusal account reports none rather than nought — nought is
a generator that was never kept waiting, and an unasked question is not that.

**What polices the throwaway venue instead is attainment.** A generator squeezed
onto the same processors as the stack it drives either offers the load or does
not, and whether it did is a reading of the fleet — how many machines arrived
against how many the phase asked for — which is namespaced by construction and
owes nothing to what else the box was doing.

## Consequences

The staging nightly reads its own pod rather than the node, so production's load
no longer invalidates it. The sweep's legs are no longer decided by which instant
a single look landed on, and the top rung — where the generator is squeezed onto
the same four processors as the stack it drives — reports its own starvation
through attainment rather than through a figure that describes the pair.

The `machine` scope is a smaller claim than the field used to make, and saying so
is the point: on the venue where the two halves share a box, the harness cannot
separate them, and a run there rests on the fleet's own account of what arrived.

Bundle schema goes to 3.
