---
number: 107
title: Where a load run happens
---

# ADR-107 — Where a load run happens

## Context

Five profiles were written and never scheduled anywhere. Three time limits were
shorter than the work they bounded. A run was placed on a node with no room left
and waited until something timed out. Two deploys failed while the nightlies went
on testing the newest commit against the server staging still ran: the older
server refused every one of the load test's 500 handshakes, and the network
drill's machine enrolled twice and never connected, so each night read as a
fault in the product.

## Decision

**Every profile is named by a workflow, and where it runs follows from what
it asks for.** A table binds the two and is checked against the workflows,
so a profile with nowhere to run fails rather than sitting unscheduled.

**Staging is production-shaped where a run can feel it.** Its server reserves
and is capped at exactly what production's is, and its database reserves and is
capped at production's processor, so a number measured there means something
about production. The staging database keeps a smaller memory reservation: it
holds a night's fixture rather than anything a customer depends on. Production's
server reserves what it is capped at, processor and memory, which puts it last
in the eviction order; production's database reserves its whole memory
ceiling, which keeps it last when memory runs short.

**A database's processor ceiling is one processor, and its reservation 175m**
([`values-production.yaml`](../../deploy/helm/opengate/values-production.yaml),
[`values-staging.yaml`](../../deploy/helm/opengate/values-staging.yaml)). A
ceiling is enforced per tenth of a second, and a database's own processes arrive
in bursts. Capped at 250m, production's database was paused in about a third of
its busy tenths of a second for a whole week, and staging's in a fifth of them
through a load run, which took the run's registration tail from tens of
milliseconds to over a second with the median unchanged. The busiest minute
either used in three days was 37m outside a load run and 40–80m through one, so
175m covers both with room and the pair reserves the same 350m of the node's
book it did. A reservation only decides the split once the node runs short of
processor, and it never has: its busiest minute in those three days was 845m of
its two processors.

**On the runner, a machine reaches the target the way it does in
production.** The transport asks the kernel for a 7 MiB receive buffer on every
socket and a runner allows a socket 1 MiB, so the generator and the server ran
on what the cap left them; and a generator dialling the loopback's published
port reached the server through Docker's userland proxy, a third process
relaying every datagram inside neither side's allowance. The runner is given the
buffer ceilings production's node has, and the generator dials the name on the
server's certificate mapped to the container's own address
([`perf-stack-quic.sh`](../../scripts/perf-stack-quic.sh)). A run either end of
which still got less buffer than it asked for fails. The runner's ceiling was
read on the relayed path and stands until a run on this one walks the rung.

**Every time limit around a run is derived from the run's own length** — how
long the pod lives, how long the verdict is waited for, how long the credential
lasts. Three limits shorter than the work are three ways to lose a night's
measurement to arithmetic.

**A namespace claim is renewed while its holder is working**, and a claim lost
mid-run fails the release rather than letting two runs share a namespace.

**A run against staging proves first that staging runs the build it tests.**
In the step after taking the claim, which keeps every deploy off staging until
it is released, the run reads the image each server pod started from and
requires it to name a commit that builds the same server as the commit under
test — the same paths that decide whether a commit builds a new image
([`image-inputs.sh`](../../scripts/lib/image-inputs.sh)). Pods from two builds,
a tag naming no commit, and a cluster that will not answer refuse the run as
well ([`staging-version-check.sh`](../../scripts/staging-version-check.sh)). A
refused run is **not started**, and its alert names the build staging runs and
the commit it was asked to test. The deployed fault drills are handed the image
the deploy installed rather than the newest commit, so a rollback is drilled as
the build it rolled back to. Every job that takes the claim runs the check in
its next step
([`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh));
the deploy itself is the one exception, since it installs the build.

**A pod reserves what it uses, and the total a run reserves is checked against
what the node has left.** A wait that fails prints the cluster's own reason
rather than a timeout.

**A busy-machine ceiling is read differently depending on who owns the machine.** On a
box the run owns, the run queue at the instant of the check, uncapped — a node
committed to four times what it has and one exactly full are different findings.
On a box the run is a guest on, the processor's busy share across the whole
node, from two readings of the kernel's processor counters — ten seconds apart
for the first reading, and since the last one after that but never less than a
second — because the instant says more about the neighbour than about the run.
The kernel counts processor time in ticks, so two readings taken back to back,
as a walk's phase boundary asks for, can find no time passed and leave the node
unmeasured. A load average was weighed and
rejected: it counts tasks waiting on more than the processor, carries a burst
that began before the run, and read a node 20–24% busy as 89% committed. A
measure that could not read reports so; an unread figure is not a machine at
rest.

**A guest finds the node busy, and waits for room.** Past the ceiling it reads
again after a wait that starts at 15 seconds and doubles to at most five
minutes, each a random time between half and all of its step so the readings
never lock onto a busy spell that repeats on a clock, and it walks at the first
reading under the ceiling
([`room_wait.go`](../../server/tests/loadtest/room_wait.go)). The wait runs before
the walk is announced, so the browser-side load joins a walk that has begun, and
is bounded by one figure, `LOADTEST_ROOM_WAIT_SECONDS` in
[`loadtest-run-budget.sh`](../../scripts/loadtest-run-budget.sh), which every
wait and lifetime downstream adds. A night that found no room is **not started**:
its readings go into the bundle, it stays out of the trend, and the alert quotes
them. The shared server's alerts stay on while the run waits, since a server busy
for a reason of its own is what they are for; the quiet period opens once the
harness has found room.

**The shared-processor ceiling is read once, before the run offers anything.**
Processor time is taken in turns rather than used up, so a reading during the
run is partly a reading of the run itself.

**The endurance run is five hours with churn**, not eight holding still. Holding
still exercises nothing after the first minute, and eight is not a length this
venue offers: a scheduled job is killed at six. Five is the longest run that
finishes, so the churn is where the operations come from.

**The cron names an order, not a time.** The families are sequenced by measured
duration across five workflows, so they do not collide.

**A venue's ceiling is a reading, and a profile stays inside the one its venue
has.** The number of machines a profile asks for is the largest number in it and
was the one number nothing checked. Asking for a fleet the venue has never held
does not fail loudly: it half-arrives, and a percentile taken over the half that
did still clears a limit written for a full one, so the leg is green and the
finding is that there is no finding. Both figures cost a run to learn, so they
are written down once — in
[`loadtest-venue-ceilings.sh`](../../scripts/lib/loadtest-venue-ceilings.sh),
each with the run that established it — and
[`loadtest-venue-ceiling.test.sh`](../../scripts/tests/loadtest-venue-ceiling.test.sh)
holds every profile to the row for the venue it names. A ceiling says what a
venue has been observed holding with its arrivals landing and its errors at zero,
never what it ought to manage: raising a row means walking the rung.

**The one shape allowed past a ceiling is a ladder, and a ladder says so
itself.** A profile carrying `gave_out:` has written down what counts as giving
out before going to look for it, which is the whole of what a capacity ladder is
— so no separate exemption list exists to go stale. What it owes instead is the
bracket: a rung at or below the ceiling and a rung above it, or no rung of it can
be named as the last that held. A ladder that has stopped reaching past the
ceiling has stopped being one, and is refused.

## Consequences

Adding a profile means adding its row, which means choosing where it runs, which is
the step that was being skipped.

Adding a venue means recording what it has been measured holding. A venue row no
profile names fails the sweep rather than sitting there going stale, which is the
same rule the tool-version manifest keeps for the same reason.
