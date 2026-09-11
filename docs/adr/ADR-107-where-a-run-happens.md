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

**Staging is production-shaped**, reserving and capped at what production
reserves, so a number measured there means something about production.

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
still exercises nothing after the first minute.

**The cron names an order, not a time.** The families are sequenced by measured
duration across five workflows, so they do not collide.

## Consequences

Adding a profile means adding its row, which means choosing where it runs, which is
the step that was being skipped.
