---
number: 54
title: Erasure and retention
---

# ADR-054 — Erasure and retention

## Context

A customer can require that a machine's data be erased, and the product holds
that data in several stores at once. Separately, alerts and incidents only ever
grow, and a table that only grows eventually decides its own retention by
falling over.

## Decision

### Erasure

**Immediate and irreversible. No soft delete, no grace period, no undo.** A
request to erase is a request to erase.

**The tombstone is written first.** The subject goes into a persisted deny-list
before anything is deleted, so a reconnecting agent cannot re-create what is
being removed while the removal is in progress.

**Every write path checks the deny-list.** The agent server keeps it in memory
and refuses writes for a tombstoned subject.

**Jobs are verified and resumable.** `purge_jobs` records progress per store, so
an interrupted purge continues rather than restarting or silently stopping
half-done.

**The proof of erasure is retained.** The audit events and the tenant row stay.
Erasing the record that something was erased defeats the point.

**Erasure is server-side only.** The metrics-store delete credential never
leaves the server.

### Retention

**Alerts, evidence and closed incidents are removed after a year.**

**Age is counted from when it was received, not when it happened.** A
retroactive scan can find something from months ago; counting from the event
would delete it on arrival.

**An open incident is never removed, at any age.** It is somebody's outstanding
work.

**An incident outlives its alerts.** A closed incident is removed only once
nothing points at it any more.

**The pass is batched and repeats until drained**, because the first run after
this shipped had a year of rows to get through and a single statement would have
held a lock for all of it.

## Consequences

The declared retention period is the one the tables observe, which is checkable
rather than a claim in a document.
