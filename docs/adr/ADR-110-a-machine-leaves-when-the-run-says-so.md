---
number: 110
title: A machine leaves when the run says so
status: Accepted
date: 2026-09-10
---

# ADR-110 — A machine leaves when the run says so

## Context

A profile is a walk between levels, and the levels go down as well as up. The
soak's ten cycles end and restart two hundred and fifty machines each, and the
whole reason the soak is five hours rather than eight is that the churn is what
finishes enough operations for a leak detector to divide by
([ADR-105](ADR-105-a-simulated-machine-is-one-machine-for-the-whole-run.md)).
The two shape families end on a recovery step, which is the phase that answers
whether the system comes back after being pushed.

None of those wind-downs happened.

A machine's whole life is a dial, its traffic, and then a hold that keeps it in
the run. The hold's only stopping condition was its own clock. A profiled run
gives every machine a hold as long as the whole walk — it has to still be there
for the last phase, so the breakpoint ladder holds for forty minutes against a
thirty-five minute walk — which leaves the wind-down reaching nothing at all.
The machine stayed connected. The step below it, which does watch for the
wind-down, was never entered, because the hold above it never returned.

Nothing said so, and each instrument was blind for its own reason:

- **The run's own count went down.** It is `len(running)` over the machines the
  fleet is holding, and winding one down removes it from that map. The count
  answers *did my wind-down code run*, and the wind-down code ran.
- **The whole-run conservation bracket was clean.** It reads the target at the
  start and at the end, and by the end the run has stopped and every machine has
  gone. The defect is entirely between those two readings.
- **The fleet's tests pass.** They drive the fleet with a stand-in machine that
  honours the wind-down, so they exercise the fleet's bookkeeping and never the
  hold underneath it. No test climbed, wound down, and climbed again — which is
  the shape the soak is made of and the only shape that shows it.

The estate is what turned it from a wrong number into a red night. A machine is
given an identity when it starts and gives it back when it leaves, so a machine
that never leaves never gives one back. The 2026-09-10 endurance run
([34422964318](https://github.com/volchanskyi/opengate/actions/runs/34422964318))
climbed to five hundred, wound two hundred and fifty down, and then found nobody
free for the rest of the night: nine of its ten busy phases offered no load at
all, 2,250 machines could not arrive, and the bundle came back invalid. Three
readings agree on the mechanism.

| Reading | What it was | Why |
|---|---|---|
| Machines that could not arrive | 2,250 | nine busy phases × the 250 that never came back |
| Run length | 5h16m25s | last arrival at 16m25s, plus the five-hour hold — the walk itself is 4h44m |
| `quiet-01` connected | 250 | the count said 250; the target was holding 500 |

The two shape families do not go red, because neither climbs again — and that is
the worse half. `spike` winds two thousand machines back to five hundred and
`breakpoint` winds sixteen thousand back to five hundred, and both report the
recovery they were asked for while the target goes on carrying the full load.
The phase that exists to say whether the system comes back has never once
measured a system coming back.

## Decision

**A hold ends when the run winds the machine down as well as when its own clock
runs out, and the first of the two decides.**

The hold takes the machine's context and stops the moment it is cancelled. It is
the same condition the step below it already uses, which is what makes the two
consistent: a machine is in the run until the run says otherwise, and its own
clock is a bound rather than the decision.

**The wind-down is exercised by a test that watches a machine leave**, rather
than by a stand-in that leaves on request. The gap that hid this was a suite
that tested the fleet's arithmetic and the hold's behaviour, with nothing
standing where the two meet.

## Consequences

The soak churns as it says it does: two hundred and fifty machines end and
restart on each of ten cycles, five thousand operations rather than five hundred,
which is the denominator the endurance question needs. It also ends when its walk
ends rather than thirty-two minutes later, since the run no longer waits out a
hold that nobody is inside.

`spike` and `breakpoint` measure their recovery steps against a target that has
actually been let go of. Both have published a recovery figure on every night
they have run, and those figures describe a target still under full load; the
series re-bases from the first night after this.

What this does not settle is the instrument. The run's account of its fleet is
bookkeeping the wind-down maintains, and nothing reads what the target is
holding while the walk is in progress — the conservation bracket covers the run
as a whole, which is precisely where this defect is not. A per-phase reading of
what the target holds is what would have named this on the first spike run, and
[`process_open_fds`](../../server/tests/loadtest/target_health.go) is already on
the page the harness fetches. Setting the tolerance for it needs a night's
readings from the repaired code to bracket, so the reading is taken first and
the gate follows it.
