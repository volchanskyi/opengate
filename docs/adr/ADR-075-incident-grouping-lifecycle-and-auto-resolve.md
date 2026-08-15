---
adr: 075
title: "Incident Grouping, Lifecycle, and Auto-Resolve"
status: Accepted
date: 2026-08-15
---

# ADR-075: Incident Grouping, Lifecycle, and Auto-Resolve

## Status

Accepted.

## Context

[ADR-074](ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md) gave
alerts a home and gave incidents a table, an index and a closed vocabulary. It
deliberately stopped short of the thing that makes an incident worth having:
until now the only room anything opened was the storm room, and every ordinary
alert landed as a row with `incident_id` null.

That gap is the whole product question, not a missing feature. Contoso pushes a
bad driver at 02:41 and forty machines start reporting; by morning there are 312
alert rows. Nobody on call reads 312 rows. One room saying "forty machines, since
02:41" is a thing a person acts on, and the difference between the two is
entirely in how the rows are grouped.

Three decisions inside that turn out to be easy to get wrong in ways nothing
notices for months.

**Grouping has two axes and only one of them is obvious.** How *wide* a room is —
the machine, the office, the customer — is the axis everybody implements. How
*long* firings stay one room is the axis that carries WS-4471: a workstation that
freezes once a day for a month. No single freeze is worth a callout and each
looks like a one-off. The pattern is the diagnosis, and only grouping across time
makes it visible. Get the second axis wrong and the same thirty alerts are thirty
rooms, each individually dismissible.

**A room has to close by itself, and the number that closes it is not free to
choose.** A queue that only grows is a queue nobody trusts. But an auto-resolve
hold shorter than the grouping window closes rooms whose next occurrence is still
due — which is exactly how WS-4471's thirty freezes fragment — while a hold
longer than it leaves a room open that an arriving alert is no longer allowed to
join, and the one-open-room-per-key rule then has nowhere to put that alert.

**Silence is not recovery.** A device in maintenance stops sampling, so its room
goes quiet for the same reason the host work is happening. Reading that as
recovery closes the incident the maintenance is *about*, and the technician comes
back to a queue that says everything is fine.

## Decision

**An alert joins an open room when all hold:** same `organization_id`, same
`rule_id`, a grouping key the rule's declared scope resolves to the same value,
and an event time inside the room's window. The fold runs in the same transaction
as the alert's own insert — an alert filed outside its room is invisible to the
only surface anybody looks at, which is worse than one that never arrived,
because nothing says it is missing.

**Keyed on `rule_id`, never `rule_version`.** Retuning a curated rule while
somebody is working the incident it raised must not fork the room out from under
them. Two *different* rules firing on one underlying condition stay two rooms:
two findings, two remedies, and merging them hides whichever the technician did
not read.

**The customer is the widest a room may be.** Grouping never crosses a customer
boundary — at the tenant, Contoso's driver rollout and Fabrikam's unrelated
outage land in one room with no correct assignee. Nothing in the rule grammar
spells `tenant` today, and a test refuses one that tries, because an
unreachable-by-convention ceiling is exactly how a ceiling comes back.

**Grouping keys that are not rungs of the tenancy ladder do not decide the
room.** A rule may group on `mount` or `metric`, which say which volume or
dimension a firing was about — a property of the alert, not of the room. A server
with a full data volume and a full system volume has two alerts and one room to
visit it in; the schema has no narrower room to offer, and two rooms for one
machine is a worse answer than one. The room is the narrowest tenancy rung the
rule actually names, and a rule naming none is about the machine that raised it.

**The scope key is derived on the server from the machine's own place in the
tenancy ladder** — never accepted from the endpoint, which could otherwise name
another customer's room and file its alerts into it. A machine filed into no
office cannot be grouped at office scope, and pooling every unfiled machine under
one absent key would put unrelated estates in a room with no correct assignee, so
the room narrows to the machine: the narrowest thing that can honestly be named.

**The window is measured against the room's own span, not the wall clock, and it
is two-sided.** A retroactive scan produces thirty findings from a month of local
history in the same second; judged against now, twenty-nine of them look a month
stale and open a room each. Two-sided because a scan walking history backwards
emits its findings newest-first, and a fold written only to extend forwards
fragments it. A finding older than the room's whole span is **stored and filed
under no room**: it is not part of the story a live room is telling, and the room
is not stale, so neither back-dating it in nor closing live work on the strength
of it is right.

**An alert past the window closes the lapsed room on its way past** and opens a
fresh one, stamping the closure at the instant the room *became* closeable rather
than when the alert happened to arrive. This is what makes the periodic sweep a
matter of promptness rather than of correctness, and it is what makes the
half-hour control case produce thirty rooms deterministically.

**`reopen_window` is the rule's grouping window, with no override.** ADR-074's
plan of record allowed one per rule; working it through, every override breaks
one half of the pair. Longer leaves a room open that an arriving alert may not
join. Shorter closes a room whose next occurrence is still due. The default was
definitional, and so is the absence of an alternative: this is the one number
that makes auto-resolve and grouping agree by construction. It is **read live
from the rule** rather than copied onto the room at open time, because the fold
reads it live too, and a frozen copy would let the two disagree again.

**`occurrences` and `device_count` are restated from the room's own alerts on
every fold**, never incremented. Two concurrent increments read the same starting
value; an erased machine's rows leave and no foreign key subtracts them. Restating
is also why a resumed purge is safe to run twice, and it makes the fold and the
erasure repair arrive at the same number by the same route. The two counts are
different questions — 312 alerts across 40 machines is one event and two very
different numbers — and the Contoso fixture asserts both in one test, because a
version that conflates them passes every fixture where one machine raises one
alert.

**Concurrency is resolved by the database, not by a mutex in Go.** The fold takes
the open room `FOR UPDATE`, and the open-or-join is an upsert against ADR-074's
partial unique index whose conflict clause *blocks* rather than failing. A mutex
would hold only while one process is the only writer.

**A low-severity observation opens no room on its own.** A fleet event where no
host individually breaches is visible precisely because several hosts see it at
once, so an observation is stored holding no room until a second *distinct*
machine reports the same thing inside the window — at which point the readings
that were waiting join the room they opened. At device scope, cross-device
co-occurrence cannot happen, so an observation-only rule about one machine never
raises a room however often it fires. An observation arriving into a room that is
already open simply joins it: that is the context an investigation wants.

**Lifecycle `new → acknowledged → investigating → resolved`,** with every forward
skip allowed and backward moves stopping at the queue — a technician going off
shift hands a room back rather than leaving it looking worked. An unchanged
status is **not** a transition and is refused, because recording one puts a line
in a handover timeline that says nothing happened. Every accepted move appends
one `incident_events` row naming both ends and the actor.

**Resolving requires a cause code from the closed set;** anything else refuses
one. `false_positive` is the load-bearing member — it is the only channel that
says which curated rule needs its threshold moved, so a resolution that skips it
spends feedback the rule pack is tuned from. Each refusal is its own typed error
(unknown status, unknown cause, illegal transition, cause required, cause not
allowed, not found, key already open) because each is a different mistake with a
different fix and an API above has to answer differently for each.

**Reopening is a door of its own, not a transition.** `resolved → investigating`
is refused; `Reopen` withdraws the answer that was given — clearing the cause code
and the resolution time — because that is a different act from carrying on with
an open room. It fails with a named error when the same condition has since
recurred and opened a fresh room: there is one open room per key, and the live one
is where the alerts are landing.

**The auto-resolve sweep is the one statement in the store that names no
tenant.** It is asked about all of them, so there is nothing for a predicate to
confine it to, and a stale room in a tenant nobody is currently serving requests
for still sits in that tenant's triage queue. It runs admin-scoped like a purge,
and a test pins the exception so the list cannot quietly grow a second member.
It is clock-injected, never slept: a seven-day recurrence window is otherwise
untestable, and a sweep driven by sleeping is a test that passes on a fast
machine.

**A machine in maintenance keeps its room, checked inside the same statement that
would close it** — a device entering maintenance between a check and a close
would otherwise have its incident closed by the very silence maintenance causes.
The shield is only for a room about that one machine: a customer or office room is
still being reported into by the rest of the estate, and shielding those would let
one machine parked in maintenance pin an estate's rooms open indefinitely.

**A room whose rule this build no longer ships is left for a person.** There is
nothing to measure its hold against, and closing a customer's open work on a
guessed number is worse than leaving it. The storm room carries a hold of its own
— a rolling hour, because that is exactly what a storm is — since no catalogue
rule can supply one for a room that is not a rule.

**No notification on any transition.** Delivery stays investigation-aid only.

## Consequences

An incident is now the unit of work, and the alert table is its evidence rather
than its interface. Contoso's rollout is one row in a queue instead of 312, and
WS-4471's month of freezes is one row saying "thirty occurrences" — which is the
first time the recurrence is visible at all.

Restating the counts costs one indexed aggregate over the room's alerts per fold.
That is bounded by the hourly ceiling and buys numbers that cannot drift under
concurrency or erasure, which a counter cannot.

Grouping now depends on the rule catalogue at ingest time. A connection wired
without one falls back to the narrowest room and the shortest hold: both
directions of a guess are wrong, but too narrow only ever produces more rooms,
while too wide merges two customers' unrelated events.

The `reopen_window` override ADR-074 anticipated does not exist. If a rule ever
needs a hold that differs from its grouping window, that is a change to the
relationship between the two axes and belongs in a new decision, not a knob.

Assignment, comments and the read API are not here — this owns the state machine
and the event rows; the surface that exposes them is the next step, as are the
aggregate `/metrics` counters over open rooms.

## References

- [ADR-074](ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md) —
  the store, the partial unique index and the closed vocabularies this builds on.
- [ADR-071](ADR-071-rule-catalogue-bindings-and-durable-coverage.md) — where
  `group_by` and `group_window_secs` come from.
- [ADR-072](ADR-072-retroactive-rule-evaluation.md) — the retroactive scan whose
  findings the event-time fold exists for.
- [ADR-056](ADR-056-device-maintenance-mode.md) — the maintenance state the sweep
  reads.
- [`internal/alerts`](../../server/internal/alerts/),
  [`conn_alerts.go`](../../server/internal/agentapi/conn_alerts.go),
  [Monitoring](../Monitoring.md).
