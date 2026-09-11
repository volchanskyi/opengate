---
number: 101
title: One measurement, one limit, one file
---

# ADR-101 — One measurement, one limit, one file

## Context

The numbers a night was judged by were spread between the profile, the
regression script and the workflow. Two of them could disagree and nothing said
which was authoritative.

## Decision

**The profile is the only home for the numbers.** The script keeps the method —
how a window median is taken, how a tolerance is applied — and none of the
values.

**A measurement carries one limit that can fail a night, and any number of marks
that only report.** Two ceilings on one measurement means one of them is
decorative.

**Every measurement the extraction produces carries a decision** — a limit, or
an explicit note that it is reported only. A number with no decision attached is
a number nobody reads.

**Limits are read where both halves of the night exist**, so a limit cannot be
compared against a measurement taken somewhere else.

**A limit names a measurement that can actually be produced where the run
happens.** A ceiling on something nothing there measures never fails.

**A limit names something the system decides, not something the profile
declares.** A walked profile paces its own arrivals, so a floor under the rate
machines turn up at measures the profile. What the system decides there is
whether the machines asked for arrived, which the error rate holds, and how long
the server took over each, which its own registration timing holds.

**A measurement nobody has a reading for is watched before it is enforced.** A
limit set from a belief rather than from nights of readings fires on the
measurement's own noise the first time it is able to fire, and a limit everybody
has learned to ignore protects nothing.

**A technician-side figure travels as an offer, never as an achievement**, until
something on that side measures it.

### The sweeps

**A phase says how hard the target worked**, read from the target's own
processor counter rather than inferred.

**The generator declares its own share** and runs inside a processor allowance
of its own, so a squeezed generator reports its own starvation rather than
answering wrongly.

**Each step sits below what the rest of the stack leaves free**, so a step is a
question about the target and not about the node.

**The legs are read together.** An aggregation job downloads every leg and reads
the curve. It does not refuse a curve that fails to rise — one night is one
sample per step, and a sweep is for seeing a shape, not for gating on it.

**A ladder declares what counts as failing**, and the run reports the last step
that held, the step that failed, and the reading that decided — plus how many steps it
read, because "nothing gave out" is a different answer from "nothing was tried".

**The profile offers the load and names the window.** The profile's own shape is
projected into the generator, journeys arrive at a rate rather than all at once, and the
percentile is taken over the phase the profile marks rather than over the whole
run including its ramp.

## Consequences

Changing a limit is a change to one file, visible in a diff, in the same place
as the measurement it governs.
