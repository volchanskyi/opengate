---
number: 102
title: A drill reading is the scenario's own, or it is not a reading
status: Accepted
date: 2026-09-06
---

# ADR-102 — A drill reading is the scenario's own, or it is not a reading

## Context

The nightly network drill runs four scenarios in order against one link shaper,
and publishes a row per measurement into a trend a regression check reads. Run
33945139320 was the first complete night, and two of the things it published
were not readings of the scenario that published them.

**The proof that a fault happened counted every earlier scenario's.** The shaper
counts what it did with the datagrams it handled for the life of its process, and
its control endpoint offers no reset
([`control.go`](../../server/tests/netfault/control.go)). The check that
separates "the machine coped with an outage" from "the outage never happened"
asked only whether the count was above zero, so from the second scenario onward
it was satisfied by an outage that had finished twenty minutes earlier under a
different label. The third scenario's own fault dropped three datagrams and the
guard read 446; had the shaper ignored the instruction entirely it would have
read 443 and passed. The rows each scenario published about the link carried the
same running total:

| Scenario | dropped by this scenario | published |
|---|---|---|
| s1 | 401 | 401 |
| s2 | 42 | 443 |
| s3 | 3 | 446 |
| s4 | 0 — its fault is delay, which drops nothing | 446 |

So the scenario that drops nothing by design published 446 drops into the trend
under its own name, and the machine-facing row did the same with 387 for the two
scenarios that dropped none in that direction. The tenfold difference between the
first blackhole and the second — the clearest fingerprint of the simulated fleet
leaving after the first outage — was invisible in every row published.

**A status the drill could not read was published as a machine that dropped.**
Every comparison the drill makes is against `online`. A request that did not land
answered `unknown` and a reply with nothing in it answered the empty string, and
both lose that comparison exactly as an offline machine does: an offline
transition in the two scenarios that count them, and a migration that failed in
the one that reads the status either side of the address change. The direction is
a false alarm rather than a false green, and what it costs is the nightly's
credibility — a run that reds because one request through the in-cluster client
did not arrive is a run people learn to re-read as noise.

## Decision

### A scenario's figures are what changed while it held the link

Each scenario records where the shaper's totals stood when it opened and measures
everything it publishes from there. The proof that a fault happened asks what
this scenario dropped, so a total left on the clock by an earlier outage is not
evidence that this scenario's instruction reached the link.

The two rows are named for what they now carry:
`netdrill_shaper_dropped_to_server` and `netdrill_shaper_dropped_to_machine`. The
`_total` suffix said running counter, which is what they had stopped being.

The alternative was a reset on the shaper's control endpoint. Subtraction was
chosen because it needs no new command, and because the opening reading is
already taken at every scenario's first phase boundary and was being discarded.

### A reading the drill could not take is not a status

`device_status` answers `unreadable` when it could not read — a request that did
not land, a machine missing from the list, or a reply with nothing in it. No
machine is ever in that state, so nothing can mistake it for one, and every
caller treats it as the drill failing to observe rather than as the machine
having moved.

The polls the scenarios share refuse rather than answer when they never once read
the status: a window of readings nobody could take reports no staleness and no
crossing of the offline line, which is indistinguishable from a machine that
behaved. A scenario that could not observe the system emits nothing, which is the
rule the drill already held itself to.

### A helper cannot end a scenario from inside a command substitution

The refusals above are raised in the scenario's own shell. A command substitution
runs in a subshell, and ending a scenario from inside one ends only that
subshell — the run carries on with an empty reading and decides the phase on it.
The reading is therefore assigned first and the refusal raised on the assignment,
never at the point of use.

This was already live: the shaper's counters were read this way at every phase
boundary, so a shaper that stopped answering left the run continuing with nothing
in hand. It reached an inconclusive verdict by luck, through a later check, under
a message about the wrong thing.

## Consequences

The trend's two link rows change meaning, and their old names carry the readings
taken before this. The panel that draws them
([`network-drill-trend.json`](../../deploy/grafana/provisioning/dashboards/network-drill-trend.json))
matches the new pair.

The suite states the world it puts the runner in: every counters fixture opens on
a total the scenario did not earn, and the direction toward the machine inherits
one that never moves, so a missing subtraction on either side is visible rather
than absorbed by a zero. Breaking each half of this decision turns a named test
red — the proof-of-fault check, the published figure, the unreadable status, and
the machine-direction subtraction each have one.

What this does not touch: the reconnect figure is still bounded by the drill's own
phase clock and cannot reach its floor, and the simulated fleet still does not
survive the first blackhole. Both are recorded in the debt register. The second
is now easier to see from the trend alone, because the drop rows carry each
scenario's own figure and the fleet's departure is a tenfold fall between the
first blackhole and the second.
