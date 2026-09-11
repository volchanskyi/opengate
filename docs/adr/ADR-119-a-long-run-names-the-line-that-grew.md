---
number: 119
title: A long run names the line that grew
status: Accepted
date: 2026-09-10
---

# ADR-119 — A long run names the line that grew

## Context

The endurance family exists because of a defect that stranded two goroutines and
their retained heap on every finished session, forever. Staging walked 29 → 134 →
230 → 334 MiB across four nightly load runs and was killed against a 384Mi limit,
sitting at 7,148 goroutines with no sessions live, while every liveness number
the server published read healthy throughout — because the code that decrements
those numbers ran ([`resource-conservation.md`](../../.claude/rules/resource-conservation.md)).

The conservation reading answers that class of question now. It brackets the run,
divides what the target did not give back by the completed operations between the
two readings, and fails the run on a slope
([ADR-101](ADR-101-one-measurement-one-limit-one-file.md) holds its limit). That
is *whether*.

It is not *where*, and the distance between the two is a working week. A red
endurance run today hands back one number and an instruction: reproduce five
hours of load with a profiler attached and find out what it was. The run that
produced the evidence is gone, the machine it ran on is destroyed, and the next
one is seven days away.

What makes this worth fixing rather than enduring is that the answer was already
sitting on a port the harness reads. The server publishes its own profiler on the
cluster-only listener the exposition comes from
([`internal_listener.go`](../../server/internal/app/internal_listener.go)), and a
goroutine profile is not a hint about a leak of this shape — it *is* the finding:
a count, and the stack it is parked on. Against the defect above it would have
printed `7148` beside a line in the relay. Nothing was asking for it.

## Decision

**A run asked for an interval takes the target's goroutine and heap profiles on
that interval, keeps every one, and reports the difference between consecutive
readings as a growth at a named line.**

Four things about it are decisions rather than mechanics.

**Every profile is kept.** The finding is a difference, and a difference cannot
be recovered from a summary of either side. Sixty-one readings of both profiles
across a four-hour-forty-four-minute walk is about fifteen megabytes, which is an
artifact rather than a problem.

**They are kept as the text the target symbolised, not as the protocol buffer.**
A pprof protocol buffer carries addresses, and resolving them needs the exact
binary that produced them — which on this venue is destroyed with the job. A
profile nobody can symbolise names no line at all. The `debug=1` form is
symbolised by the target on the way out, so a reader months later needs nothing
but the file.

**The site is the first frame that is not standard library.** Every parked
goroutine's own top frame is the runtime parking it, so a site taken from the top
of the stack would report every leak in the product at the same line of
`proc.go`. The test that separates the two is the one Go itself uses to tell a
module path from a standard-library one: the first element of an import path
carries a domain, and no standard-library path ever does — plus `main`, which has
no path at all.

**A profile that could not be taken is counted, not skipped.** A page that is not
the profile it was asked for parses as no stacks, and no stacks reads as nothing
grew — which is the healthiest answer a leak detector can give. So each parser
refuses a page that does not announce itself, every unanswered fetch increments a
count the bundle carries, and a trail of fewer than two readings fails
`Bundle.Validate()` outright: a single reading has no difference in it, so it
cannot have found that nothing grew.

Growth is reported per stack as the delta between the first and last reading,
together with **how many of the intervals it grew in**. That second number is
what separates a leak from a working set that got bigger once and then held: a
leak grows in nearly every interval, a filled cache grows in one.

## Consequences

The bundle schema is 7. A soak carries `leak_trail`: the interval, how many
readings were taken, how many the target would not answer with, where the
readings are kept, and the growing stacks ranked heaviest first. Absent for every
run that was not asked to watch, which is deliberately not the same document as a
run that watched and found nothing.

The trail is diagnostic and gates nothing. The conservation slope already decides
whether a run failed, and it decides it on a reading of the resource rather than
on a profiler's account of where allocations came from; adding a second verdict
over the same question would put two ceilings on one measurement, which
[ADR-101](ADR-101-one-measurement-one-limit-one-file.md) refuses. What the trail
adds is the line to open once that verdict is red.

What it still cannot answer is what *holds* a leaked object. Go's heap profile
records where an object was born, and a leak is not about where something was
made — it is about what is still pointing at it. That is
[ADR-120](ADR-120-what-holds-an-object-is-followed-on-the-box-the-run-destroys.md),
and it needs a different instrument.
