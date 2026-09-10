---
number: 112
title: A climb is offered at the rate it declares
status: Accepted
date: 2026-09-10
---

# ADR-112 — A climb is offered at the rate it declares

## Context

A phase declares a level and a length, and the two together are an offer. A step
going from eight thousand machines to sixteen thousand over five minutes is
offering fifty-three arrivals a second. That arithmetic is written into
[`breakpoint.yaml`](../../load/profiles/breakpoint.yaml) in as many words: it is
why the ladder's steps are five minutes rather than two, because sixteen thousand
arrivals inside two minutes is a hundred and thirty-three a second against a
server that refuses enrolments past about a hundred.

Nothing made the arithmetic true. The fleet broke each phase into ten
instructions and dialled every machine of each instruction the instant it
received it, so an offer written down as a rate went out as ten bursts. The
`within` argument the sequencer passed for exactly this purpose was ignored by
the only implementation that had a network on the other end.

The 2026-09-10 performance stack is where it surfaced, on both families that had
just been raised past the burst the server tolerates:

| Leg | Offered | Reached | What the server said |
|---|---|---|---|
| `volume-8000`, phase `steady` | 8,000 machines over three minutes | 34% | `enrollment refused with 429` |
| `breakpoint`, step `step-8000` | 8,000 machines over five minutes | 52% | the same |
| `breakpoint`, step `step-16000` | 16,000 machines over five minutes | 21% | the same |

The refusals are the server working. Its per-address allowance is a hundred
requests a second with a burst of two hundred, the whole fleet dials from one
address, and a step of the top rung is sixteen hundred machines arriving at once.
Two thirds of them were turned away, counted as machines that could not arrive,
and the night was scored invalid — on a target that was never asked to carry the
load at all.

It had been invisible for as long as the profiles stayed small. Every rung below
four thousand puts fewer machines in a burst than the burst allowance holds, so
the whole family passed for the life of the harness, and the two legs that first
crossed the line crossed it on the night they were raised.

## Decision

**The fleet spreads a climb across the window it is given, and the window is the
gap until the next instruction.**

The sequencer already had the figure — a phase's length divided by the ten steps
it climbs in — and already passed an argument in that position. It now passes the
window, and the fleet dials the machines it is adding one every window-over-count.

Two things about the shape, both of which are what makes it usable:

- **The level is claimed at once and only the dialling waits.** A machine takes
  its place in the level the moment it is asked for, so the step after this one
  asks for the level it was going to ask for anyway, and a machine still waiting
  its turn is not dialled twice.
- **A machine let go before its turn came is neither an arrival nor a failure to
  arrive.** It offered nothing and saw nothing. Counting it as a machine that
  could not connect would report an error rate for load that was never offered —
  the same mistake, one field over, that this decision exists to remove.

Winding down is not paced. A machine leaving is not an arrival, and nothing on
the other end rations departures.

## Consequences

Every profile's declared arrival rate is now the rate that is offered, and all of
them sit inside what the server accepts: the ladder's top rung at 27 a second,
`volume-8000` at 53 on its ramp and 36 on its steady phase, `spike` at 50 across
its thirty-second burst, and the staging profile at 4. No profile changes to
achieve it — the arithmetic they already declared is simply now performed.

`spike` is the one to watch, because a spike is meant to be sudden. Its
thirty-second climb from five hundred machines to two thousand was already
arriving at about fifty a second under the ten-burst shape, and paced it arrives
at fifty a second exactly, so what the family measures is unchanged.

The staging series are not re-based. What changed there is a hundred machines a
minute arriving smoothly rather than in bursts of twenty-five, which is inside
the noise of a fleet that size, and the profile, the level and the fixture are
all untouched — the conditions
[`loadtest-summarize.sh`](../../scripts/loadtest-summarize.sh) names as reasons
to re-base. The two runner families do change materially, because their top rungs
now offer load they had never once offered, and neither publishes into that
trend.
