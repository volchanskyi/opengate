---
number: 74
title: Where an alert lands and how it becomes an incident
---

# ADR-074 — Where an alert lands and how it becomes an incident

## Context

Three hundred machines losing the same upstream produce three hundred alerts and
one problem. A technician needs the one problem, without losing the ability to
see the three hundred.

## Decision

**An alert's identity is the machine, the rule, the rule's version and the
window it fired for.** Not a generated key — an identity the sender can
reproduce, so a reconnect replaying alerts inserts nothing twice.

**Evidence is a column on the alert row, written in the same statement.** The
alert carries what the device saw when it fired: ranked dimensions, the series
around the moment, the processes, redacted. It is self-contained, because the
machine may be offline by the time anyone opens it. Evidence is decoded under a
stated size bound before it is believed.

**The vocabularies are closed at the database** — severity, status, scope — so
an unknown value is a write that fails rather than a screen that renders
nothing.

**A ceiling of 500 alerts per customer per rolling hour.** A refusal is filed as
a typed suppression and counted, so the ceiling is visible in the numbers rather
than silently shaping what anyone sees.

**Timestamps are refused, not clamped.** The telemetry path clamps a skewed
clock; an alert with an impossible time is rejected, because an alert is a claim
about when something happened.

**An alert joins an open incident when the customer, the scope and the rule all
match, and the incident is still inside its window.** Keyed on the rule, never
the rule's version, so retuning a rule mid-incident does not split the room in
two.

**The customer is the widest an incident may be.** Grouping never crosses one.

**The scope is derived on the server from the machine's own place in the
tenancy ladder**, not taken from the alert.

**The window is measured against the incident's own span, not the wall clock.**
An alert arriving past it closes the lapsed incident on its way through and
opens a new one.

**Counts are restated from the incident's own alerts**, not incremented, so they
are right after an erasure removes some of them.

**One open incident per grouping key, enforced by a partial unique index** —
the database refuses a second, rather than the application trying not to create
one.

## Consequences

Erasing a device restates the affected incident counts from the surviving rows
before it finishes, so no incident is left claiming machines that no longer
exist.
