---
number: 76
title: Aggregate metrics about the alert pack
---

# ADR-076 — Aggregate metrics about the alert pack
## Context

Watching the detection pack means publishing numbers about it. A number per
device per rule would be the fleet size times the catalogue size, which is the
cardinality problem the whole edge design exists to avoid.

## Decision

**Five aggregate series on the existing exposition, none carrying an entity
label.** No device, no customer, no incident. Cardinality is bounded by
construction rather than by hoping nobody adds a label.

**The rule label is bounded by the shipped catalogue**, which is a fixed list in
the binary, not by anything a customer can create.

**Every value of every closed vocabulary is exported, including the zeros.** A
series that appears only once something goes wrong cannot be alerted on, because
its absence and its zero are the same thing to a query.

**The two gauges are refreshed on a timer, not computed while being scraped.** A
scrape must not run a query. A refresh that fails leaves the previous answer
standing, because a database hiccup should not look like the fleet going quiet.

**Only a stored alert counts as created**, so a reconnect replaying alerts
already recorded does not inflate the rate.

**The alert rate is a measured gate.** The pack does not advance past its stage
until the rate it produces has been measured, and a figure from a synthetic soak
is reported as a figure about the harness rather than about a fleet.

## Consequences

The series are listed in
[`Metrics-Reference.md`](../architecture/Metrics-Reference.md), with the
invariants that bind them.
