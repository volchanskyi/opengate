---
number: 82
title: What makes a load run valid
---

# ADR-082 — What makes a load run valid

## Context

A load run that measured nothing looks exactly like a load run that found no
problem. Runs were reporting numbers about their own harness: timing their own
send buffer instead of the server, opening no relay session and passing, reusing
one machine's identity across many connections, and counting refusals the server
made on purpose as faults.

## Decision

### Three outcomes, not two

Valid and failed are both measurements — one of a system that held, one of a
system that did not — and both belong in the trend. **Invalid** is the third:
the run did not measure the system, because a scenario produced no rows, the
generator ran out of room, or a safety ceiling stopped it. An invalid run never
moves a window median. A scenario past its error ceiling has produced a finding,
not a series.

The run's own verdict is what gates it, not a count of failures, and every
workflow reads that verdict back rather than inferring one.

**A capacity ladder is judged against the answer it went looking for.** A
profile that declares what giving out means is sent to find the rung where the
system gives out, so a rung at or above that one is what the run measured rather
than a run that measured nothing — and the machines lost reaching it are the
reading rather than a fault. Every rung below it is held to the ceiling exactly
as any other phase is, which is what keeps the recovery phase able to report a
system that gave out and stayed broken.

### Every number is a reading

**A field carries a reading or it is absent.** Nothing is copied from one field
into another to fill it, and a missing mandatory section fails the run — a
bundle nobody wrote is silence, not a pass.

**Where a run can say why a reading is absent, it says so, and the absence
stands.** A phase carrying no reading of how hard the target worked is a reading
somebody dropped and voids the bundle; a phase saying the target would not
answer is a fact about the target, which is the answer a capacity ladder climbs
to find. A phase may carry one or the other and never both.

**Offered and achieved load are separate.** Collapsing them hides the case the
validity rule exists for: a generator that could not produce the load reads
exactly like a system that could not absorb it. The technician side stays absent
until something on that side measures it.

**Registration is timed where the device row lands**, from what the server
records, with connection-pool occupancy beside it — a registration queued behind
a connection and one executing slowly are the same latency until the pool says
otherwise. The outcome the harness reads is named by the server's own constant
rather than a copy of it: a vocabulary with two homes gives a reading that is
always empty and limits that can never fire.

**The relay is measured through a relay.** The generator opens the technician's
side of a real session and times its own frame coming back while the harness
holds the machine's side and echoes.

**A phase is the difference between two readings of a running tally**, which is
the only way to separate a phase from the run around it when a machine reports
once at the end of its life. Refusals the server made on purpose are counted
apart from faults; counting an enforced limit as a defect buries the real ones.

**The target is read at both ends of the run.** Replaced mid-run makes the run
invalid; not giving back what it took makes it failed. Both readings travel in
the bundle, taken off the exposition rather than the cluster.

### The fleet

**An identity is minted once and no two live connections are ever the same
machine.** The set of simulated machines is a fixed roster; a start takes one nobody is
connected as and gives it back when it leaves. Reuse would be worse than the
defect it closes — one device on two connections means the server keeps
whichever registered last and the fleet's level silently drops by the one
displaced. A roster with nobody free says so rather than doubling up, and a
profile asking for more machines than it holds is refused before the clock
starts.

**A ramp is spread across the window it is given** rather than offered in
bursts the server refuses. A machine still queued is not asked for twice, and
one let go before its turn is neither an arrival nor a failure to arrive.

**Each machine is recorded as it arrives**, under its customer and building,
identified from its own certificate rather than by asking the server what
exists. A refused filing is counted and the run continues; a scenario that reads
a building waits until there is one.

**Filing waits for the row.** A machine's row is written when the server has
finished reading its register frame, and the machine's own write returns as soon
as the bytes are buffered locally — so the filing that follows an arrival can
reach the server first and be told there is no such machine. It asks again for a
bounded window, and what it reports when it stops is the server's own words: the
same status covers a machine that is not there yet and a customer that is not
there at all.

**The fleet is wound down before it is read.** The order is the property.

**The harness is launched detached inside the pod, and the pod holds the
fleet**, so a dropped connection costs the answer rather than the fleet. A
launch that never happened is retried and one that happened is never made twice
— decided by asking the pod what it holds, not by reading an error string,
because a second harness would build a second fixture over the first one's
names.

### Independence

**A run's names carry its own seed**, so two nights never ask the server for the
same thing whatever the night before left behind.

**No authority key leaves the cluster.** The harness enrols the way an installer
does, with a token minted for the run and deleted after it.

**Cleanup counts every kind it removes**, before and after. Counting is the
removal's only check — a kind removed but never counted is residue nobody can
see, which is how eight customers survived a week of cleanups that each reported
success.

**A safety ceiling belongs to the environment that has the thing it protects.**
Staging shares a node with production and declares one; a runner created for the
job has no neighbour and may not, because driving the processor is the
experiment and a ceiling nothing consults reads as protection that is not there.

## Consequences

Every gate row is provably reachable: a test reads the series names out of the
regression check's own case labels and asserts the extraction produces each one,
from fixtures shaped the way the pinned tool writes them.
