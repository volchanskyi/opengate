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
and refuses writes for a tombstoned subject. It holds bare machine numbers and
costs 126 bytes per deleted machine, measured over 100 000 entries by
[`tombstone_cost_test.go`](../../server/internal/agentapi/tombstone_cost_test.go),
so it is kept for good.

**The machine is told, and finishes the job itself.** A deleted machine that
connects is sent its removal order before the connection closes: the server
writes it, closes its sending side and waits for the machine to hang up. The
machine wipes its own store and identity and uninstalls itself, in an order that
resumes after a power cut ([ADR-128](./ADR-128-machine-identity-lifecycle.md)).
The edge erasure is not a stage of the purge job — an offline machine may never
return — and a purge completes on the server's stores alone.

**The order cannot outlive the master key.** A removal order travels only over a
mutually authenticated connection, so a deleted machine still offline when the
master key its certificate was issued under is dropped can no longer be told to
remove itself, and keeps its own copy until someone uninstalls it by hand. Data
Erasure states the limit; keeping each deleted number's issuer and expiry was
weighed and rejected, since it breaks the bare-number deny-list and still could
not reach the machine.

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
