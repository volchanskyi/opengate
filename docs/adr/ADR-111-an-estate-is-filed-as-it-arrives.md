---
number: 111
title: An estate is filed as it arrives
status: Accepted
date: 2026-09-10
---

# ADR-111 — An estate is filed as it arrives

## Context

The load harness plans an estate — customers, the buildings their machines sit
in, and how the fleet is spread between them — and walks that plan through the
public API the way a technician would. Four profiles ask for a lopsided estate,
where one customer holds 80% of the fleet, because the page that is slow in the
field belongs to the customer holding most of the machines.

None of it reached a machine. `FileDevices` was written and tested and called
from nowhere, so `fixture: lopsided` changed the customer rows and the building
counts and nothing else: every machine sat under the tenant's own customer, in no
building at all.

Two things follow from that, and the second is the one that costs a number.

**A device is filed by two calls, not one.** `PUT /api/v1/devices/{id}/organization`
says which customer answers for it and `PATCH /api/v1/devices/{id}` puts it in one
of that customer's buildings. `FileDevices` made the first call only, and the
building is what every list narrows by. The building a connection carries is read
off the machine's existing row, and a first registration creates that row with
none — so nothing anywhere put a machine in a building.

**Two scenarios in the nightly trend were already asking for filed machines.**

| Scenario | What it timed |
|---|---|
| [`api-baseline.js`](../../load/k6/scenarios/api-baseline.js) | `siteWithDevices` walks every building looking for one that holds machines, finds none, and falls back to the tenant-wide read — the narrowing branch had never once run |
| [`concurrent-agents.js`](../../load/k6/scenarios/concurrent-agents.js) | picks a building at random each iteration and reads *its* machines, so every read matched nothing |

The second is the sharper one: `journey_device_list_ms` and the concurrent-agent
reads have been the cost of a query against an empty set, published as the
night's numbers. Neither is a read anyone in the field makes.

Wiring the filing was listed as if it were free, and it is not. A machine can
only be filed once its row exists, and the row exists when the machine registers
— so filing follows the load rather than preceding it, and any run that files
has to decide when.

## Decision

**A machine is filed the moment it arrives, under the customer the plan gave it
and into one of that customer's buildings.**

Arrival is the first moment the machine can be filed and the first moment
anything knows it has arrived, and the run already holds everything else it
needs. It does not have to ask the server which machines exist: the harness
chooses each machine's identifier when it enrols and puts it in the certificate's
common name, which is the field the server parses back out on every connection.
Listing the fleet to match it up by name would be a second source of truth for
something nobody has to ask about.

Filing one machine at a time, as it arrives, also spreads the writes across the
ramp that brought it in, rather than gathering them into a pass that would have
to wait for the last arrival and then land as a burst.

**Filing is not the load.** A refusal is counted and the run carries on, because
a run that died on one refused filing would throw away a measurement it had
already taken. The count travels in the bundle — schema 5 — so a reader can ask
whether the fleet a night measured was one the product could find. What must not
happen is the opposite: a run reporting a filed estate it did not file.

**A scenario that reads a building waits until there is one to read.** Each
chooses its building in its own setup, once, before its first iteration, so a
scenario started against an unfiled fleet reads an empty building for the whole
of its run whatever arrives afterwards. The run announces a filed estate,
[`loadtest-quic-incluster.sh`](../../scripts/loadtest-quic-incluster.sh) waits for
that line the way it already waits for the fleet's own, and the workflow asks
between starting the fleet and running the first scenario. The wait is the gate:
an estate that is never filed fails the step rather than being measured around.

The level announced is the profile's **first phase**, not the whole estate. A
profile climbs, so the estate is only complete once its tallest phase is reached,
and a step waiting for that would stand idle through the ramp it was meant to
overlap. What the scenarios need is a building that holds machines.

**No flag.** Filing is what a run does when it built a fixture. A run with no
administrator has nobody to file for and files nothing, which is the ordinary
shape of a bare run against a local stack.

## Consequences

The staging nightly's device reads become reads the product makes. Both scenarios
above re-base from the first night after this: one stops timing a tenant-wide
read under a narrowed read's name, and the other stops timing an empty result
entirely. Neither series is comparable across that boundary, and both are worth
more after it.

The throwaway venues file too, so `fixture: lopsided` is true where it is
declared — one customer holding 80% of up to eight thousand machines. Nothing
there reads it yet: the volume family has no browser-side generator, so what the
filing shapes on that venue is the data the family weighs rather than a
measurement. Giving it a reader is the work that answers the question the filing
was written for, and it is not done here.

Two calls per machine are added to each arrival, made by the run's own
administrator session rather than by the browser-side generator — so they do not
spend the per-address budget the scenarios are paced against. At the staging
fleet's five hundred machines over a one-minute ramp that is a modest write rate
beside the ramp itself, and the arrival timing is unaffected: a machine's arrival
is timestamped before the filing is attempted.
