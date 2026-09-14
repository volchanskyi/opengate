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
workflow reads that verdict back rather than inferring one. A step that runs the
harness reads its status rather than being ended by it: a fleet that half
arrived is a measurement, and the verdict step beside it is what judges one.

**A capacity ladder is judged against the answer it went looking for.** A
profile that declares what giving out means is sent to find the rung where the
system gives out, so a rung at or above that one is what the run measured rather
than a run that measured nothing — and the machines lost reaching it are the
reading rather than a fault. Every rung below it is held to the ceiling exactly
as any other phase is. The recovery phase behind the ladder reaches for machines
of its own — the run empties its fleet first — because the generator never
replaces a machine it lost, so a recovery phase inheriting a crushed fleet holds
corpses rather than a level and can say nothing about whether the system came
back.

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

**A machine that arrived and a machine that ended cleanly are two facts, and
every count says which one it is.** The fleet that exists is the machines that
reached registered; whether one was later severed is recorded beside it and
never inside it. A summary that counted the survivors reported a fleet of 439
in a run that had filed 10,520 machines under a customer, and it took the
connect, handshake and registration series from those 439 alone — dropping the
slowest arrivals first, so a run reads faster the more of its fleet it loses.
It reaches the conservation denominator too: every machine that connected is one
operation the target has to give back. On a system that holds, the two counts
are the same number, which is why only a night that severed its fleet can show
the difference.

**A level the harness holds is published beside the target's own count of it.**
A phase's achieved level is the machines the fleet has not wound down, which is
bookkeeping the wind-down maintains: it answers whether the wind-down code ran.
So each phase also carries what the target says it was holding at that instant
and the goroutines under that count, taken from one reading of the target's own
page, and a phase whose target was holding materially fewer machines than the
phase counted did not measure the system at that load. Two counts of one
population kept by the two ends can disagree; one count cannot. The goroutine
count bounds them below, because the listener starts one per accepted
connection — measured at three per machine against twenty-nine at rest — so a
level with no population behind it is refused even where the target keeps no
count of its own. A target that could not be asked says so, and the absence
stands, exactly as the busy-ness reading beside it does.

**The share of a fleet that did not get in divides by the machines that asked.**
Those are the machine-lives the run produced, less the ones it stood down
itself: a wind-down cancels every start still reaching for the server when a
level comes down, and such a machine never asked for anything. It is counted off
the run's own results rather than off the fleet somebody declared, because an
endurance run replaces a machine when it leaves and arrives several thousand
against a declared five hundred — dividing by the declaration puts more arrivals
over the line than the line allows for, and a share below nought passes every
ceiling a profile can write. Counted from the results, a machine that arrived is
a machine that asked, so the share cannot leave nought-to-one whatever shape the
run had. Where a reader has only the printed block and not the results, it
refuses rather than publishing the difference.

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

**A machine the run stood down is the run's own doing, not the system's.** Every
start still reaching for the server when a level comes down is cancelled by the
wind-down, and it never registered — so it is counted apart from both, in the
fleet's tally, in the results block and in the rate the trend is given. Read as
failures they are indistinguishable from a server that would not take them, and
they land in whichever phase the wind-down happened in, and a phase that winds
down offers no arrivals of its own.

**A phase that reached for no machine has no arrival error rate.** An outcome is
known when a machine's life ends rather than when it began, so a dial that
started under the level before this one can end under this one — and a phase
that winds down reaches for nobody, which leaves the tail of the phase before it
as every outcome inside its window. A ladder's recovery read 0.588 over ten
failures and seven arrivals, out of a fleet of sixteen thousand, against a server
that was answering a fresh machine in fifty-seven milliseconds; that reading of
the crush was the only thing standing between the night and the first answer this
family has ever produced. So the ceiling falls on a phase that offered arrivals,
and a phase whose question is whether a crushed system came back offers them.

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

**The pod answers that question in a word, not in an exit code.** `test -e`
exits one for a file that is not there and the client exits one for a call that
never reached the pod, so an exit code makes an absence and a refusal the same
fact. One refused call, eight minutes into a twelve-minute hold, reported a pod
holding no fleet while its harness was still running, and the night was
discarded. A word can only be printed by a pod that heard the question; anything
else is the question going unanswered, and it is asked again.

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
