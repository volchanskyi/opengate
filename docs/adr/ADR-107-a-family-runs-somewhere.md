---
number: 107
title: A family runs somewhere, and the venue is what it asks for
status: Accepted
date: 2026-09-09
---

# ADR-107 — A family runs somewhere, and the venue is what it asks for

## Context

Seven profiles existed. Two were named by a workflow. The other five — the
everyday shape, the busiest ordinary morning, a site's link coming back, the
load raised until something gives, and the endurance run — were named by
nothing at all, verified by grep across the workflows, the scripts and the
server.

So there was no endurance test, against a leak class that had walked staging
from 29 to 334 MiB over four nights before it was killed; no capacity test,
against a documented ceiling of about 20,000 machines that nothing had ever
gone looking for; and no burst-recovery test, against the one event a fleet
actually produces.

`docs/infrastructure/Testing.md` placed them on "staging at night" and "staging
overnight", in the present tense.
[`docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh)
structurally cannot catch that: its phrase list looks for a thing described
after it was removed, and this is the opposite — a thing described before it
ever existed.

Three of the five could not have run on staging anyway. The node offers 1,830
millicores with 1,680 reserved; `peak` and `spike` ask for two thousand
machines and `breakpoint` for four thousand, and the most ever connected to
staging is a hundred. Saturating the node also throttles production's own
health probes, and no guardrail low enough to prevent that leaves room to find a
breaking point.

The everyday run had a subtler version of the same problem. It passed no
`-profile`, so it took the flat path — every machine at once, no phases, no
gates — and recorded `profile_name: "ad-hoc"`. `normal.yaml`'s phases were read
by nothing while its limits were read by the gate.

## Decision

**Every profile is named by a workflow, and the venue follows from what the
profile asks for.**

| Venue | Families | Why |
|---|---|---|
| Staging, nightly | normal | the everyday shape on the hardware production runs on |
| Throwaway runner, nightly | peak, spike, breakpoint, volume, scaling | four processors and a box the job destroys |
| Throwaway runner, weekly | soak | the run owns the machine, which is what makes a debugger possible |

**Staging is production-shaped.** Its server reserves and is capped at 250
millicores and 384 MiB, exactly what production is. A load run against a server
sized differently from the one customers use answers a question about a machine
nobody has — and reserving what it is capped at is the other half, because a pod
that reserves less than it uses is evicted before pods that reserved properly.
The generator pods come down from 200 millicores of reservation to 150 and burst
instead; reservation governs admission, the machine is three-quarters idle, and
its processor limits are already oversubscribed several times over.

**The endurance run is five hours with churn, not eight holding still.** Holding
a connection is not work: an unchanging fleet finishes one operation per machine
for the whole run, so a leak detector dividing retained bytes by finished
operations has almost nothing to divide by. Ten cycles of 500 down to 250 and
back finish ten times what eight idle hours would, and five hours fits inside
the six a scheduled job is killed at.

**A cron names an order, not a time.** Measured across five workflows over six
nights, every scheduled run starts four and a half to six and a half hours after
the hour it names, and the spacing compresses as it slips — the load test has
begun inside the mutation matrix's window on two of the last three nights. Every
comment describing a wall-clock slot now describes a position in the order, and
what actually serialises the two runs that share the staging namespace is the
claim they take on it ([ADR-106](ADR-106-a-venue-lasts-as-long-as-the-run-it-holds.md)).

**The table is checked against the workflows.**
[`loadtest-family-venue.test.sh`](../../scripts/tests/loadtest-family-venue.test.sh)
reads both directions — every profile is run by some workflow, and every row of
`Testing.md`'s table links a profile and a workflow that exist and that name
each other. Either direction alone is satisfied by an empty table.

## Consequences

- Every load-shape a profile describes now happens. A profile nobody schedules
  fails the gauntlet rather than reading as coverage.
- Three families move off the shared cluster, where the answer would have been
  about contention rather than about the server.
- The staging nightly's numbers re-base once. The server halved its processor,
  the run walks phases instead of offering everything at once, and the fleet is
  five hundred rather than a hundred — so every series takes a new
  `workload_name`, and until three nights of the new work exist the gate reads
  the profile's absolute ceilings alone. The widest of those sits thirty times
  above what the measurement produces, so none of the three changes can reach
  one.
- The shape families' limits are machine-side only, because no browser-side
  generator runs on that venue and a limit naming a measurement the venue cannot
  produce is a limit that passes forever. A browser-side leg there makes those
  limits legal without the check being edited.
- A matrix that assembles a profile path at run time is invisible to every sweep
  that reads a workflow as text, so the paths are spelled out.
