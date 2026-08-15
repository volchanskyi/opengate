---
adr: 074
title: "The Alert Store, Accounted Ingest, and the Erasure Cascade"
status: Accepted
date: 2026-08-14
---

# ADR-074: The Alert Store, Accounted Ingest, and the Erasure Cascade

## Status

Accepted.

## Context

[ADR-068](ADR-068-system-event-rules-and-the-edge-alert-sink.md) gave alerts a
transport and a shape. Until now they had nowhere to land: `handleAgentAlert`
validated an alert, wrote a debug line and returned. That was deliberate — the
ingest ledger's invariant is that everything counted as ingested either produced
state or was filed under a drop reason, and counting an admission that persists
nothing would have put a number on one side with nothing on the other.

Giving alerts a home turns three questions from "later" into "now", and each has
an answer that is easy to get wrong in a way nothing would notice for months.

**Where evidence lives.** An alert is the only carrier of the detail behind a
signal. Central keeps one 60 s average per dimension and there is no path for
asking the endpoint afterwards, so whatever arrives on the alert is the whole of
what will ever be known about that moment. An alert row stored beside evidence
that failed to write is worse than no row: it reads as a complete record of
something nobody can reconstruct.

**What a storm costs.** Contoso pushes a bad driver at 02:41 and forty machines
start reporting. Without a ceiling, one customer's bad night fills the table and
the queries over it; with a ceiling in the wrong place, that same night spends
the budget of every other customer Northwind looks after, and detection goes
quiet across the estate. The second failure is worse than the storm.

**What survives an erasure.** DAL-WS-012 is decommissioned mid-incident. The
foreign key takes its alerts and its evidence. It cannot touch `occurrences` and
`device_count` on the incident those alerts folded into — those are application
state, so a technician opening the room next week reads "40 machines" about an
estate that has 39, one of which does not exist.

## Decision

**Evidence is a `bytea` column on the alert row, written in the same statement.**
Not a side table, not an object-store key: both would make "the alert is here and
its evidence is not" a state the system can reach. Atomicity is then a property
of the row rather than of the code path, and it is proven by a forced failure —
evidence past the cap is refused by a check constraint and the alert row does not
appear. The cap, and the rule that a blob is always accompanied by the codec that
reads it, are check constraints for the same reason: evidence is immutable and
unfetchable, so anything that slipped past an application check would sit in the
table forever.

**The vocabularies are closed at the database.** `severity`, `status`, `scope`,
`cause_code` and the incident-event `kind` are check constraints, not application
convention. A severity nothing downstream can render would otherwise be stored
happily and discovered by whoever opens the incident.

**One open incident per grouping key, enforced by a partial unique index** on
`(organization_id, rule_id, scope, scope_key) WHERE status <> 'resolved'`. Two
alerts arriving on two connections at once would otherwise each open a room and
split one estate-wide event into two nobody can reconcile. Resolved incidents sit
outside the index, so the same condition recurring next month opens a new room
rather than colliding with a closed one.

**The alert's identity is `(device, rule_id, rule_version, window_start)`,** not
the id the device chose. An agent that lost its local store picks a new id and
would duplicate every alert it still had to send. A replay against the identity
is a no-op — not an error, and not a second row.

**The hourly ceiling is 500 per customer, measured over a rolling hour, and
enforced inside the insert.** Per customer because at the tenant one customer's
storm silences every other customer's detection. Inside the insert because a
count taken beforehand and an insert taken afterwards see different rows, and a
storm arriving on several connections at once is exactly when that gap opens. The
count's cost is bounded by the ceiling itself: nothing is stored past it, so the
window it scans cannot grow.

**Suppression is counted twice and folded once.** A refused alert files a typed
drop, so the ingest ledger still balances, and increments
`opengate_alerts_suppressed_total{reason}`, which is what an operator watches.
The suppressed alerts fold into one storm incident carrying the count, because a
number on a dashboard is not the same as a room in the customer's triage queue.
That room's `device_count` stays zero and means what it means everywhere else —
how many machines have alerts in this room — since a suppressed alert never
became one.

**Alert timestamps are refused, not clamped.** The telemetry path clamps a skewed
stamp and counts the correction, because a sample has no identity to lose. An
alert's window start *is* its identity, so clamping would make the same alert
resolve to a different row on every reconnect. A retroactive finding is
legitimately months old, so its backward bound widens to the backfill retention
rather than the live window.

**Evidence is decoded before it is believed,** under a stated inflation bound. A
codec check alone cannot tell a valid blob from a corrupt one, and 64 KB of
DEFLATE can name gigabytes of output.

**A device erasure restates the incident counts from the surviving rows, before
the device row goes.** Recomputing rather than subtracting is what makes a
resumed purge safe to run twice; running before the cascade is what makes it
possible at all, since afterwards nothing says which rooms the machine was in. An
incident the erasure empties is closed with a `resolution` event saying why, and
**no cause code** — those are a person's answer, and `false_positive` in
particular is the channel that decides whether a rule gets retuned. A tenant
purge erases alerts, incidents and their history by name: the tenant row is
retained as the audit anchor, so nothing cascades from it.

## Consequences

The alert store has exactly one writer, so there is no second source of truth for
an incident. Every alert that reaches the server now ends in one of four states —
stored, replayed, suppressed, or lost — and each of the last three is counted
under its own reason, so "we are not seeing alerts" is always answerable.

The ceiling costs one indexed count per stored alert. That is the price of a
brake that cannot be raced, and it is bounded by the ceiling it enforces.

Incident bookkeeping is repaired at erasure time rather than recomputed on read.
A reader is therefore trusting numbers a writer maintained, which is why the
repair recomputes from surviving rows instead of adjusting by a delta: an
adjustment that ran twice would be wrong, and a purge is resumable by design.

Grouping, lifecycle transitions and auto-resolve are not here — an alert's
`incident_id` is written by the incident engine, and until it lands the storm
room is the only incident anything creates.

## References

- [ADR-068](ADR-068-system-event-rules-and-the-edge-alert-sink.md) — the alert
  transport and evidence shape this stores.
- [ADR-054](ADR-054-edge-sentinel-data-lifecycle-erasure.md) — the purge ordering
  the erasure repair runs inside.
- [ADR-071](ADR-071-rule-catalogue-bindings-and-durable-coverage.md) — the
  catalogue an alert's rule id is checked against.
- [`014_investigations.up.sql`](../../server/internal/db/migrations/014_investigations.up.sql),
  [`internal/alerts`](../../server/internal/alerts/),
  [`conn_alerts.go`](../../server/internal/agentapi/conn_alerts.go).
