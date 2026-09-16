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

**A limit is bracketed by nights of the load its leg offers, and a leg whose
load changes re-earns its limits.** They are two halves of one statement, and
leaving the second unsaid cost a red night that read like a product regression.
The volume family's registration ceiling of 500 ms was bracketed by four nights
reading 396 to 482 ms; the family then began offering the technician load its
profiles declare, and its largest leg went to 4,510 to 5,773 ms across the next
four, with the target's processor use going from 38–62 to 85–87 per cent. Its
two smaller legs carry the same technician load and read 6.5 and 5.0 ms, so the
step is the venue at the top of a sweep driven to what it has been shown to
hold, not the write path. A limit inherited across that change is a limit on an
experiment that is no longer being run, and it says nothing about the one that
is.

**A technician-side figure travels as an offer, never as an achievement**, until
something on that side measures it.

**Where a night has no browser-side half, its limits are read off its own
evidence.** Seven profiles run on a throwaway stack with no browser-side
generator on it, so the join that builds canonical rows for the everyday night
cannot happen there and every limit they declared was read by nothing. The
evidence bundle is turned into the same canonical rows and handed to the same
one evaluator; a second evaluator would be a second set of numbers to keep
level. The reader emits only the series a limit names, the two sides are held
level in both directions by a sweep, and a bundle it cannot read refuses rather
than answering with an empty set — an empty set is a night where every limit
passed for want of anything to compare.

**A limit sits inside the range the generator can drive.** A floor on a rate is
a statement about the system only where the generator is capable of exceeding
it; where it is not, it is breached on every run ever taken and says nothing —
the same shape as a limit on a measurement nothing produces, one field over. The
relay path was held to five requests a second by a generator holding five
sessions, each making one request and then waiting a full second: five a second
is its arithmetic ceiling, reachable only against a server that answers in no
time. Five nights read 4.926 to 4.935 against a server opening a session in
4.9 ms. A sweep now computes each session-driven scenario's ceiling from the
sessions the profile declares, the requests a journey makes and the pause it
takes, and refuses a floor at or above it; how far underneath a floor sits is a
judgement about the server, made in the profile with the nights on record beside
it.

**A night that crossed a limit is red.** A breach is a finding about the system,
so the night fails and its rows still enter the trend — but the verdict was
worked out, written into the night's record and then returned as nought, so five
nights recorded themselves as failed and reported success. One of them was
carrying four registration limits held against a measurement that came back
empty on every run ever taken. A failed night now exits on its own code, which
the workflow passes through, and the reasons are printed as errors rather than
as warnings nobody reads.

**A limit is read off a run that measured something.** The bundle carries the
run's verdict beside its numbers, so where the verdict is invalid those numbers
are readings of something else and the limits over them decide nothing. They are
still printed — the leg is red on the verdict's account, and a reader who can see
the figures should say what they were — but a leg whose fleet count had been
refused reported a registration tail of 4.6 seconds against a limit of 500 ms as
its headline, on a profile that had read 396 ms the night before. The rows
reader, the evaluator and that rule are one script the four venues share, so a
pair spelled out in four workflow steps is not four places to keep level.

**A limit sits inside the range its instrument can report.** Registration timing
is a bucketed histogram, so a tail past its last finite boundary was never kept
and is reported at that boundary. The reading is then a floor, which fails a
ceiling below it correctly and can never rise to meet one at or above it — so no
registration limit may sit at or above the histogram's last boundary, and a
sweep holds the profiles to the buckets the server declares.

**A measurement a family exists to drive past carries no ceiling.** The ladder
climbs until arrivals fail, so its aggregate share of machines that did not get
in is the finding rather than a fault: any ceiling it could pass is one it
passes by not finding an answer. What is worth holding there is whether the
system came back, which is the recovery phase rather than the whole walk, and
the per-phase ceiling the run's own verdict applies already holds it. The
measurement is declared deliberately unlimited with that reason written beside
it, because a measurement in neither list reads from the outside exactly like
one nobody decided about.

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
sample per step, and a sweep is for seeing a shape, not for gating on it. Each
sweep has one: `perf-scaling-curve.sh` over the processor rungs, and
`perf-volume-curve.sh` over the estates.

**A sweep offers the technician load it varies things against.** Whichever
variable a family holds constant has to be something the variable can move, and
machines arriving is not: the cost of a machine arriving barely changes with the
size of the estate already in the database, or with the second processor. So a
sweep leg runs a browser-side generator from its own profile beside the fleet,
folds the journeys into its bundle, and its curve refuses a leg that carries no
technician reading — a leg without one varied its variable against nothing that
could feel it, which is how a scaling curve came to be flat from one processor
upwards while every leg looked fine.

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
