---
number: 107
title: Where a load run happens
---

# ADR-107 — Where a load run happens

## Context

Five profiles were written and never scheduled anywhere. Three time limits were
shorter than the work they bounded. A run was placed on a node with no room left
and waited until something timed out.

## Decision

**Every profile is named by a workflow, and where it runs follows from what
it asks for.** A table binds the two and is checked against the workflows,
so a profile with nowhere to run fails rather than sitting unscheduled.

**Staging is production-shaped where a run can feel it.** Its server reserves
and is capped at exactly what production's is, and its database is capped where
production's is, so a number measured there means something about production.
The database keeps a smaller reservation than production's: it holds a night's
fixture rather than anything a customer depends on, and the node's reservations
are nearly spoken for. Production's pair reserves what it is capped at, which is
what puts it last in the eviction order.

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

**A pod reserves what it uses, and the total a run reserves is checked against
what the node has left.** A wait that fails prints the cluster's own reason
rather than a timeout.

**A busy-machine ceiling is read differently depending on who owns the machine.** On a
box the run owns, the run queue at the instant of the check, uncapped — a node
committed to four times what it has and one exactly full are different findings.
On a box the run is a guest on, the last minute, because the instant says more
about the neighbour than about the run. A measure that could not read reports
so; an unread figure is not a machine at rest.

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
