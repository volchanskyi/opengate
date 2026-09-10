---
number: 109
title: A sweep varies one thing, and somebody reads it
status: Accepted
date: 2026-09-10
---

# ADR-109 — A sweep varies one thing, and somebody reads it

## Context

Two of the load families are sweeps: one varies how many processors the server
has and holds everything else still, the other varies how much data is already
there. Both had been running nightly for months, and neither had ever produced a
comparison.

Three things were wrong at once, and each of them alone was enough.

**Nothing read the legs.** The workflow had two jobs and neither consumed the
other's output. There was no aggregation step, no trend, no gate, and the
uploads were set to warn on an empty file set. The 2026-09-05 sweep returned
four legs with identical phase results — offered equal to achieved, latency
absent, no errors, no faults — against a target fingerprint of one processor and
one byte on every rung, and passed.

**The generator moved with the server.** The runner has four processors. The
stack gave the database one and the metrics store half, and the matrix gave the
server half, one, two or four — so what was left for the generator was two,
one and a half, half, and less than nothing. The generator's share shrank as the
server's grew, which makes a flat curve attributable to neither, and raising the
load alone would have made the top rungs worse. The generator declared no share
of its own, so nothing said this was happening.

**Nothing bound.** The sweep held 500 machines while varying the processors, and
500 machines arriving is the cheapest thing the server does. The ladder this
same stack ran at one processor puts the wait times at 19, 20, 26 and 62 ms for
500, 1,000, 2,000 and 4,000 machines — flat to two thousand and bending past it
— so every rung of the sweep was measuring the flat part. Two nights from the
same code disagreed about the resulting shape.

The volume family had its own version of the same emptiness. Its three fixture
names planned 500, 2,000 and 2,000 machines, and the two larger ones differ only
in a distribution that was never applied, so a three-point sweep was two points
and a broken third.

Underneath all of it sat a reading that did not exist. A phase carried the wait
times it saw and nothing about what the target did with the allowance it was
given, and from wait times alone a server that has run out of processor and a
server that is idle but slow are the same picture. Every statement about which
of the two a night showed was an inference.

## Decision

**A phase says how hard the target worked.** The target's own processor counter
is read off the page the harness already fetches registration timing and
goroutine counts from, bracketed around each phase, and divided by the phase's
own clock and by the processor allowance the target was declared with. What it
states is *the target used this share of what it was given*, which compares
across a rung with a quarter of a processor and a rung with two. A run that read
the target's own account of itself is held to producing the figure for every
phase, because both come off the one page; a reading that could not be taken is
absent, never nought, for the reason
[ADR-100](ADR-100-a-bundle-field-is-a-reading-or-it-is-absent.md) gives. The
phase field arrived at bundle schema 4.

**The generator declares its own share.** It runs inside a transient processor
and memory allowance on the throwaway venue, so the stack's four consumers are
four declarations rather than three and a remainder — and so the harness
measures its room against an allowance rather than against the box, which is the
only reading that can say a run measured the generator
([ADR-104](ADR-104-a-reading-names-whose-room-it-measures.md)). A machine that
cannot grant one says so and the run goes ahead unbounded; the bundle then
carries the shared-box scope, so nothing claims a share it does not have.

**The sweep's rungs sit below what the rest of the stack leaves**, at a quarter,
a half, one and two processors, so the generator's share is the same at every
rung. **And the fleet moves to where the rungs can differ**, at two thousand
machines. Both halves are needed: either alone leaves the sweep unreadable, one
because the generator moves with the server and the other because nothing binds.

**The legs are read together.** An aggregation job downloads every leg,
publishes the curve, and refuses a sweep that could not measure: a leg that
measured nothing, legs that name the same processor share, fewer legs than a
curve needs, or legs that all came back saying the same thing. It does
**not** refuse a curve that fails to rise. One night is one sample per rung and
the two nights on record disagreed about the shape from the same code and the
same profile, so a gate asserting the curve moves with the variable would have
failed one of them and passed the other. The shape is published for a reader;
only the ability to measure is enforced.

**The volume family sweeps machines, not fixture names**, at 500, 2,000 and
8,000 enrolled — because the fixture name does not decide how much data is
there and the machines that enrolled do.

**The breakpoint ladder reaches past where the last one stopped**, adding steps
at 8,000 and 16,000, with every step lengthened to five minutes. The lower steps
stay so a step that degrades is attributable — a single jump from four thousand
to sixteen would answer whether the system gives out without answering where —
and the length is also what keeps the arrivals inside the rate the server
refuses past: sixteen thousand machines in two minutes is 133 a second against a
ceiling of about a hundred, and in five it is 53.

**Every artifact something downstream reads fails on an empty file set**, now
that something downstream reads them.

## Consequences

The sweep can answer its own question, and says so when it cannot. A night whose
legs are identical fails rather than passing, and a night whose curve is flat is
published as the finding it is.

The generator's allowance is a bound the kernel enforces, so a leg that needs
more memory than it was given is killed rather than swapping. The figure it is
set to is measured — about 400 KB per machine over a 1.25 GiB baseline, from the
peak and breakpoint bundles at two and four thousand machines — and it is a
number to revisit when the ladder reaches higher.

The breakpoint run goes from about eleven minutes to about thirty-five, and its
job's ceiling from sixty minutes to ninety.

Where the lopsided estate comes from is a decision of its own, taken in
[ADR-111](ADR-111-an-estate-is-filed-as-it-arrives.md): a machine can only be
filed under a customer once its row exists, which is after it has registered, so
the filing follows each arrival rather than the whole load. On this venue what it
shapes is the data the volume family weighs rather than a measurement, because
nothing here reads a customer-scoped or building-scoped list yet.
