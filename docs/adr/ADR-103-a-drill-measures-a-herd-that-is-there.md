---
number: 103
title: A drill measures a herd that is there, and a reconnect it can read
status: Accepted
date: 2026-09-06
---

# ADR-103 — A drill measures a herd that is there, and a reconnect it can read

## Context

The nightly link drill stands twenty simulated machines behind its shaper so the
thin-uplink scenario measures a site catching up rather than one machine alone.
Two of its published figures turned out to be readings of something else.

**The herd was gone before the scenario that needs it measured.** The load
harness dials once and carries no reconnect, which is correct for a load
generator and is not a thing that survives an outage. Half a minute into the
first scenario's blackhole every simulated connection was gone, and none came
back, so the three scenarios after the first ran against a fleet of one.

Two complete nights measure it. In run `34025636166` the first blackhole drew
399 dropped datagrams out of the population behind the link and the second drew
39; the thin-uplink scenario then published a staleness figure identical to the
lossy-link scenario's — 57 seconds against 57 — and the lossy-link scenario has
no herd contention by design. The night before read 53 and 54.

Nothing could see it. Every row the scenario emitted was about the real machine,
so nothing read how much of the herd was left; the harness's own verdict went to
a file inside the fleet pod that nothing read, because the drill called only
`start` and never `collect`; and the pod's own output carried a single unrelated
warning line. The shaper's `machines` field is an idle-mapping expiry rather
than a count of live machines — it went on reading 21 for ten minutes after the
herd had left — so reading it is not the fix either.

The cost lands on the trend rather than on the night: staleness is one of the
figures checked for window growth, so an uncontended reading becomes the
baseline a night with a real herd is measured against, and the night that fixes
the fleet reads as the regression.

**The reconnect figure was the phase clock.** `netdrill_reconnect_seconds` read
13 one night and 18 the next, in both scenarios that measure it. The machine's
own log says why:

```
09:51:58.036  connection lost, will reconnect
09:53:28.039  connection attempt failed  attempt=1  QUIC establish: timed out
09:53:28.353  reconnected successfully   attempt=2
```

The outage was 180 seconds and the agent's idle timeout and establish timeout
are both 90, so the shape repeated exactly: notice the loss, spend one whole
doomed establish, and the link returns part way through it. The reconnect itself
cost 0.31 seconds; the published 18 was the tail of the attempt it interrupted,
and the floor it was gated against was 120 — a number the measurement could not
reach.

**A machine that never came back published nothing.** `wait_until_online`
deliberately answers empty rather than recording a machine that never returned
as having taken exactly as long as the drill was willing to wait, and `emit`
skips an empty value. So the worst outcome the first scenario exists to find
would have published no reconnect row at all.

## Decision

### The herd is the simulated fleet, and it is taught to persist

Twenty copies of the shipped agent were measured rather than estimated — the
binary from CI, a real server, real enrolment:

| | Twenty simulated | Twenty real agents |
|---|---|---|
| Processor | 1.2 millicores, all twenty | ~406 millicores (20.3 each) |
| Memory | 17 MiB, all twenty | ~700 MiB (35–38 MiB each) |
| Names in the product | twenty distinct | twenty identical |

The worker carries production and has 350 millicores unclaimed, so twenty real
agents do not fit; and the figure is not an artifact of the measuring machine's
core count, because pinned to two processors the agent drops from 30 threads to
8 and its processor use stays at 20.3 millicores. The cost is the sampler
scoring host readings every second, which is work rather than overhead. They
would also all carry the same name: the agent takes its name from the operating
system and accepts no setting to change it, and five real agents on one host
produced five machines in the product with one name between them.

So the fleet stays simulated and gains three behaviours, each behind its own
switch and each off by default, because the run that wants them is not the run
that wants the opposite:

- **It comes back after its connection breaks**, on the shipped agent's own
  full-jitter window — a base of one second to a cap of thirty — so twenty
  machines behind one link return spread out rather than together.
- **It asks again when the server tells it to wait** for a catch-up slot. The
  scheduler admits four drains per customer and shortens a deferred machine's
  wait the longer it waits; the harness used to shed the load on the first
  deferral, so sixteen of twenty would never catch up and the site's catch-up
  ended when the first four finished.
- **It carries the run's own name.** Sharing the load run's left the drill
  unable to count its own twenty, and left them to be swept the next morning by
  a cleanup belonging to another workflow.

A load run keeps the old behaviour in all three, and it is the default: a
severance it repaired quietly would be a measurement of a fleet that was not
there, and a deferral it queued through would hide the shedding it exists to
measure.

### The scenario reads the herd it claims to be measuring against

The thin-uplink scenario counts its own machines in the product's device list
before it measures, publishes the count, and goes inconclusive below the point
where a queue exists at all — eight, which is four draining with four waiting.
One machine catching up over a two-megabit link uses under half of it; four ask
for nearly twice what the link has, and that contention is the whole finding.

The count is a row rather than a floor. A scenario that cannot find its herd
publishes nothing, so a floor on it could never fire, and a gate that cannot
fire is the defect this record pays down.

### A reconnect figure is the machine's own account

Two numbers, both read from the machine's log on the machine's own clock:

- **`netdrill_reconnect_seconds`** — the link is back, and the machine is
  registered this many seconds later. What a site waits through, floored at 120,
  which is the worst healthy behaviour can produce.
- **`netdrill_reconnect_attempt_seconds`** — from the machine's last failed
  attempt to being back. It carries no luck about where in its cycle the machine
  met the restored link, so it is floored tightly at 35: the backoff cap plus an
  establish margin.

The second includes the wait before the final try as well as the try itself. The
backoff delay is logged at debug level and the machine runs at info, so the two
cannot be separated without changing what the pod logs — a spec the staging
browser suite shares. The figure is bounded by backoff cap plus establish time
either way, which is what makes its floor mean something.

The first scenario's outage length is drawn from the run's own seed and
published beside them, so it stops being a whole number of the machine's own
timeouts and the figure it decides has somewhere to land other than the same
value every night.

### A machine that never came back is a reading, not an absence

Every scenario that takes a machine offline publishes `netdrill_reconnected` as
one or nought, floored at one. The duration stays absent when there was no
reconnect, because a machine that did not come back has no reconnect duration.

This deliberately does not follow the shape used where a scenario cannot
observe the system. The rule there is that a scenario which could not observe
emits nothing — and a machine that never returned was observed perfectly. What
was observed is the failure that scenario exists to find, and calling it
inconclusive would hide it. The two are kept apart by where each reading comes
from: whether the machine returned is decided by the status poll, which refuses
outright when it never read a status at all, while the durations come from the
log and are simply absent when the log could not be read.

### The drill removes the machines it enrolled

Through the same call a technician's delete uses, with the credential the run
already holds, before the in-cluster client every request travels through is
taken down.

The drill used to remove its pods and leave its rows, following the staging
browser suite — which removes its pods and lets the next deploy's database reset
take the rows. That holds for a suite whose own workflow does the reset. Here
the herd was swept a day later by the load run's cleanup, which selects on a
name the drill merely happened to share, and the real machine matched no sweep
at all: no background loop removes a machine on age.

## Consequences

- The thin-uplink scenario measures a site or it measures nothing. Its first
  honest reading will not be comparable with the fortnight of uncontended ones
  before it, and that is the point.
- The reconnect figures spread, because the outage does. The window comparison
  gains something to compare and the floors gain headroom.
- The load run is unchanged, and a test holds each of the three switches to its
  default so a future edit cannot lose the severance signal by accident.
- The drill's own machines stop accumulating on staging between deploys.
