---
number: 89
title: The declared retention period is the one the tables observe
status: Accepted
date: 2026-08-30
---

# ADR-089 — The declared retention period is the one the tables observe

## Context

Alerts, the evidence frozen with them, and the incidents they fold into were
declared to be kept for a year, and nothing removed them at any age.

Erasure was never the gap. The purge machinery in
[`internal/lifecycle`](../../server/internal/lifecycle/) is subject-triggered:
deleting a machine or a customer takes its alerts, evidence and rooms with it,
immediately and irreversibly. What was missing is the other axis — rows nobody
deletes, on tables that only grow. The aggregate counters
([ADR-076](ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md))
made that growth visible without bounding it.

A declared period nothing enforces is worse than an undeclared one: it is a
statement a reader can act on and the system does not keep.

## Decision

A periodic sweep removes what has been held longer than the horizon. Four
decisions shape it, and each is a rule about what the sweep may not take.

**Age is counted from receipt, not from the event.** A retroactive finding is
legitimately months old the day it arrives — the agent scans local history and
sends what it finds — so measuring on `observed_at` would erase evidence on the
day it was handed over. `received_at` says how long the row has been held, which
is what a retention period is about.

**An open room is never removed, at any age.** It is somebody's outstanding work.
Only the auto-resolve hold or a person closes a room
([ADR-075](ADR-075-incident-grouping-lifecycle-and-auto-resolve.md)); age does
not, and so age cannot remove one either.

**A room outlives its alerts.** A closed room is removed only once nothing points
at it. The foreign key clears `incident_id` rather than cascading, so removing a
room out from under a surviving alert would leave a finding attached to no
investigation — which reads as something nobody ever looked at. Alerts are
therefore swept first, and that pass is what makes a room eligible in the same
run. A room's history follows it out through the cascade on `incident_events`.

**The pass is batched and repeats until drained.** These tables only grow, and
the first pass after a horizon is introduced can face a year at once. Each batch
is its own transaction, so the locks held are bounded by the batch rather than by
how far behind the sweep has fallen.

The sweep runs as one more
[`janitor`](../../server/internal/app/background.go) beside the orphan-series,
session and auto-resolve passes, with one pass at boot: a process that was down
comes back to rows that went past the horizon while it was gone. Unlike its
neighbours it destroys a customer's records rather than reclaiming the system's
own leftovers, so a pass that removed anything says how much.

Both the horizon and the cadence are the binary's to choose, stated in
`productionSchedule` with the other worker intervals: a year, swept every six
hours. Against a horizon of a year the cadence decides only how far past it a row
can sit and how much one pass has to remove.

Migration 017 adds the two orders the sweep reads. Both lead with the timestamp,
because the sweep is asked about every tenant at once and an index whose leading
column it cannot constrain is one it cannot use; the incident index is partial on
`status = 'resolved'`, since nothing else is ever a candidate.

## Consequences

The year the product declares is the year the tables observe, and
[Data Erasure](../product/Data-Erasure.md) states the two bounds a reader needs:
open work is never taken, and age runs from receipt.

Audit events stay outside this, for the reason they survive a purge — they are
the proof of what happened.

The cadence and horizon are a starting choice rather than a measured one. The
alert rate at customer scale is still unmeasured
([ADR-076](ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md)
carries the projection and what to do if the measurement comes back high), and
both numbers are one edit in one place when it lands.

## Alternatives considered

**Wait for measured growth past ~10 GB/year.** What the debt register asked for,
and the reason this was deferred. It answers a capacity question, but the gap
being closed is not capacity — it is a declared period the system did not keep,
which is equally untrue at 1.8 GB/year. Waiting also guarantees the first pass
runs against the largest backlog the tables will ever hold.

**Age rooms on `last_seen` rather than on `resolved_at`.** Simpler, and it needs
no ordering between the two sweeps. It also removes rooms that are still open, and
a closed room's own clock is the one that says how long it has been finished.

**One statement with both deletes in CTEs.** A single round trip, and wrong: CTEs
in one statement see one snapshot, so the room half would test `NOT EXISTS`
against alerts the alert half had just removed, and no room would ever become
eligible in the pass that made it so.
