---
number: 79
title: Who may change a rule, and how
---

# ADR-079 — Who may change a rule, and how

## Context

Detection rules need tuning by the people who run the fleet, without giving them
a way to write an arbitrary expression that then runs on every customer machine.

## Decision

**A top-level Rules section, readable by every member of the tenant and
writable only by administrators.** Seeing what is watching your fleet is not a
privileged act; changing it is.

**A rule's logic is rendered as a description, never as a form.** Definitions
ship in the binary ([ADR-070](ADR-070-alert-rules.md)). What is editable is
which machines a rule applies to, at what boundary, and how fast it rolls out.

**Labels are the targeting dimension**, chosen from a list each customer
maintains, cutting across the tenancy ladder. Two labels matching one machine at
the same level are settled by a precedence that is written down rather than left
to insertion order.

**A value a new rule version no longer allows moves to the nearest one it
does**, rather than the binding breaking or silently switching off.

**Both alert ceilings are editable per customer**, each with a maximum that
lives above the customer, so a customer can be more conservative than the
platform but not less.

**A stop exists at two scopes** — one customer, and every customer in the tenant
at once — because the reason to stop a rule is usually that it is wrong
everywhere.

**A noise badge counts a rule's recent alerts** for the selected customer, so
the person tuning it can see what it is currently costing.

**The Rules list carries all four coverage states against the fleet size**, so a
split that does not add up is visible rather than rounded away. It groups rules
as host rules and Linux rules from the kind each rule already states, worked out
in the browser, so the catalogue gains no field for it.

**Host and site pick-lists belong to one chosen customer.** Under "All
customers" they wait and ask for one, and the customer picker is marked, because
a host or site list spanning customers offers picks that belong to someone else.
A value is aimed at a site picked from a list rather than an id typed in.

## Consequences

Rollout populations and waiting periods are per rule and per customer, so a
cautious customer's pace does not slow everyone else's.
