---
number: 108
title: The venue picks how a busy machine is read
status: Accepted
date: 2026-09-09
---

# ADR-108 — The venue picks how a busy machine is read

## Context

Every profile declares what the run must not push its machine past, and one of
those ceilings protects a neighbour: staging shares one node with production, so
`normal.yaml` says the run stops if the node goes past 85% committed. The
throwaway stack has no neighbour and declares no such ceiling, because driving
its processor is what the scaling sweep is for.

That ceiling had never fired, because staging had never walked a profile — the
flat path skips the phase walk entirely. [ADR-107](ADR-107-a-family-runs-somewhere.md)
put the staging nightly onto `normal.yaml`, and the first run of the first night
was refused before its first phase:

```
phases: stopping before phase "ramp": the node's processor is 200% committed
against a limit of 85% — production shares it
```

The node was not busy. Sampled live from inside a staging pod twice that day,
ten looks a second apart each time, its one-minute average ran between 0.18 and
0.77 across two processors — somewhere between a tenth and two fifths of the
machine — with one to three tasks runnable out of some 680.

The reading is the fourth field of `/proc/loadavg`: the tasks runnable at the
instant of the look, less the reader itself, divided by the processor count. On
a two-processor node that quantises to fifty-point steps — one runnable task
reads as 0%, three as 100%, five as 200% — so against an 85% ceiling it is a
coin flip on a machine a technician would call idle. The run that was refused
caught the node with five.

The instant is not a mistake in itself. It was chosen deliberately for the
throwaway runner, and the reasoning written beside it holds there: the run owns
that box, and the minute before the reading is the job's own image build, so the
one-minute average would report the build as the run's own commitment and stop
every performance run before its first phase.

This is [ADR-104](ADR-104-a-reading-names-whose-room-it-measures.md)'s finding
one field over. That one said a reading of room has to name whose room it is.
This one says the same about *when*: on a box the run owns, the minute before
the look belongs to the run; on a cluster node the run is a guest on, the minute
before is production going about its business — which is exactly what a ceiling
protecting a neighbour is asking about.

| Venue | What the ceiling asks | Which measure answers it |
|---|---|---|
| Cluster node | is there room beside production | the last minute, which is production's |
| Throwaway runner | is this box saturated now | the instant, because the minute was the build |

Underneath sat a second defect of the class the file's own header names. The
instant measure returned nought for a `/proc/loadavg` it could not parse, and the
reading around it reported itself measured regardless — so an unreadable
processor figure reached every ceiling as a machine at rest, which is the one
answer that always passes.

## Decision

**A ceiling reads the machine the way its venue calls for, and the venue picks
rather than the call site.**

Two measures of processor commitment, one honest on each venue:

- **The instant run queue**, for a box this run owns. Unchanged, for the reason
  already recorded beside it.
- **The one-minute average**, for a box this run is a guest on. It does not
  quantise, so a node a third busy reads as a third busy rather than as whichever
  multiple of fifty percent the look landed on. Nothing is subtracted from it:
  the average covers a minute this reader spent almost all of asleep.

The venue is settled by the same question ADR-104 asks of the generator's own
room — **does the kernel give this process a processor allowance of its own?** A
quota on its own cgroup is what being scheduled onto somebody else's machine
looks like from inside. Both answers are confirmed live: the staging load-test
pod's 400 millicores arrive as `cpu.max 40000 100000`, and every one of the
eight legs of the 2026-09-09 throwaway perf stack reported the `machine`
headroom scope, which is that same question answering no.

**A measure that could not read reports so, and an unread figure leaves the
whole reading unmeasured.** An unmeasured node already fails the check rather
than passing it; this is what makes the processor half reach that rule instead
of filling itself in with a nought.

## Consequences

The staging nightly walks its profile against a reading of the node production
actually put there, and stops for a node that is genuinely full rather than for
one that had five tasks runnable at the wrong microsecond. The throwaway stack's
reading is untouched, on both the ceiling path and the generator meter — that
venue reaches the box reading only where the generator shares the box with the
stack it drives, which is the same venue by construction.

No profile changes: `normal.yaml`'s 85% is a sensible ceiling against a measure
that reads between a tenth and two fifths on an ordinary day, where against the
instant it was a number no small node could reliably sit under. Nothing the trend
is judged by moves, because a safety ceiling stops a run rather than publishing a
series.

What the harness reaches for is unchanged — the same two files, on the same
paths, that it already reads.
