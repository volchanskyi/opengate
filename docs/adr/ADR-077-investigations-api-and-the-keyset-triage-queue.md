---
adr: 077
title: "The Investigations API: an Operational Boundary and a Keyset Triage Queue"
status: Accepted
date: 2026-08-16
---

# ADR-077: The Investigations API: an Operational Boundary and a Keyset Triage Queue

## Status

Accepted.

## Context

[ADR-074](ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md) gave
alerts a home, [ADR-075](ADR-075-incident-grouping-lifecycle-and-auto-resolve.md)
folded them into incidents, and
[ADR-076](ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md) made
the machinery visible on a dashboard. Nobody could open any of it. Contoso's
02:41 driver rollout is one incident holding 312 alerts across 40 machines, and
until there is a read path it is a row in a table with no reader.

Three questions had to be settled before the surface could be written, and each
has a wrong answer that works in every test anybody writes by hand.

**Who may see and move an incident.** ADR-062 settled the general shape — the
tenant is the visibility boundary, `is_admin` the mutation boundary for
*configuration* — but said nothing about incidents, which are neither ordinary
fleet reads nor configuration.

**How the queue is paged.** A triage queue is read while it is being written to.
An alert lands, an incident's last activity moves to the head, and every row
behind it shifts by one.

**How much one read carries.** Evidence is up to 64 KB compressed per alert
(ADR-074's cap) and an incident folds hundreds of alerts.

## Decision

### Incidents are operational work, so tenant membership is the whole gate

Reading the queue, opening an incident, moving it through its lifecycle,
assigning it and commenting on it are operational work on the tenant's own
resources — the same class as restarting an agent or opening a maintenance
window. They are gated on **tenant membership**, not on `is_admin`. Rule
bindings and rollout state remain configuration and stay admin-gated; they are
not part of this surface.

`organization_id` **narrows, it does not permit**. Every member of a tenant may
look at any customer in it, and the picker chooses which one is on screen. But a
narrowed read must never return another customer's row: both customers sit inside
one tenant, so nothing is refused and a wrong query simply shows Contoso's estate
to somebody looking at Fabrikam's. That is a quieter failure than a breach of the
wall and it is tested separately, route by route.

Both boundaries answer **"no such incident"**. A caller must not be able to tell
an incident they may not see from one that does not exist, so a crafted id from
another tenant and an incident belonging to a customer they are not looking at
fail identically.

`requireIncidentInScope` joins `requireDeviceInScope`,
`requireAMTDeviceInScope` and `requireSessionInScope` as a named guard the
adversarial pen-test gate ([ADR-027](ADR-027-adversarial-pentest-precommit-gate.md))
recognises, so a future handler addressed by an incident id that forgets it is
refused at commit time rather than reviewed for.

### The queue is keyset-paged, and the index is the budget

A page is "everything before `(last_seen, id)`", and `next_cursor` is that
position, base64-wrapped so it stays opaque — a client that could construct
positions would eventually construct one no index answers.

Offset paging over a live queue **loses rows silently**: the row that shifts past
the page boundary is an incident nobody ever sees, and nothing reports it. Both
columns are in the key because two incidents can be last seen in the same
microsecond, and a cursor on the timestamp alone would either repeat them or drop
one.

Migration `015_incident_queue` adds the two orders the queue is read in —
`(organization_id, last_seen DESC, id DESC)` for one customer and
`(tenant_id, last_seen DESC, id DESC)` for a technician covering an estate of
them — and `(alerts.incident_id, device_id)` for the device page's strip, which
subsumes and replaces the incident-only index.

The customer-scoped read and the whole-tenant read are **two statements**, not
one with an optional predicate: they are answered from two different indexes, and
an optional predicate leaves which one to the planner's guess. Every other filter
is a sentinel comparison rather than a NULL test for the same reason — one shape,
whatever the caller narrowed on.

**Q10 is asserted as a plan, not as a stopwatch.** At 10 000 open incidents the
test `EXPLAIN`s every filter combination the UI can produce and refuses a
sequential scan of the incidents table, and for the unnarrowed reads refuses a
sort as well. A wall-clock assertion inside a unit suite measures the machine it
runs on; a sequential scan passes at ten thousand rooms on a fast laptop and
misses the budget on the estate this is sized for, which is exactly what a timing
test cannot catch.

### Evidence is a call of its own, decoded server-side

Neither the queue nor the incident detail carries an evidence blob. The detail
says which alerts have evidence, under which codec, and how many bytes fetching
one costs; `GET /api/v1/investigations/{id}/alerts/{alertId}/evidence` returns
the decoded structure. Both lists in the detail are bounded and both carry their
totals, so "312 alerts across 40 machines" never silently becomes "200 alerts".

The decode happens on the server because the blob is DEFLATE around MessagePack
and a browser has neither. `protocol.DecodeAlertEvidence` mirrors the agent's own
encoder and answers exactly two ways: this is what the machine sent, or this
cannot be read and here is why — an unknown codec, a blob that does not
decompress or expands past the bound, or bytes that are not evidence. The unknown
codec is `422` and its own error, because it is the additive case working as
designed (a newer agent, an older server) rather than a broken row. Handing back
bytes under a codec nobody claimed would put invented detail in front of somebody
deciding what happened to a customer's machine.

Nothing is redacted on this path and nothing needs to be: log lines are redacted
on the machine before they are sent
([ADR-049](ADR-049-edge-sentinel-raw-log-privacy.md)), and the read returns the
stored structure unchanged rather than re-deriving anything. A test pins the
response's field set to the evidence contract, so a later change cannot fold in
something the machine never redacted.

### The device strip is the queue, not a second list

`GET /api/v1/devices/{id}/incidents` is the same read with the machine filter
set. An incident is not keyed on a machine — a customer-wide event is one
incident across forty of them — so the filter asks which incidents hold an alert
that machine raised, answered through the alerts index. A second implementation
of one question drifts, and the one that drifts is the one nobody is looking at.

### `GET /api/v1/rules` is a coverage view, not an editor

It answers with the compiled pack beside each rule's rollout state and its
coverage split, whose four states always sum to the fleet size they were counted
against — a rule watching half an estate says so instead of reading as healthy.
The fleet size is counted once and passed into the coverage read, because a share
taken against a fleet counted a moment apart describes an estate nobody was ever
in.

The response deliberately omits the predicate, its extra terms and its clear
threshold. Those are the grammar the rule is written in, and putting them on a
read-only surface invites the question of how to change them — which is the one
thing this product does not do, because an agent that runs server-supplied code
is a supply-chain weapon aimed at every customer estate at once. What comes back
is what the rule watches and the parameters it declares tunable, each beside the
value the catalogue ships.

## Consequences

A technician can work a triage queue that is being written to underneath them
without losing rows, narrow it to the customer they are looking at, and open one
incident to see what folded in and what everybody before them did about it.

Paging is a contract the client cannot construct positions for, so the read path
cannot be driven into a query no index answers.

The cost of an incident list stops tracking the size of the incident table, and
the plan assertion is what keeps it there — an index dropped later fails the
suite rather than degrading a p99 nobody is watching.

An unknown evidence codec becomes an additive, self-announcing failure: a future
codec ships to agents, older servers say they cannot read it, and no reader is
ever handed a decoded structure that is not what the machine wrote.

What this does **not** do: it adds no rule-authoring surface, no notification or
ticketing path off an incident, and no cross-tenant view for administrators. The
queue describes one tenant, because an incident belongs to one customer inside
one tenant and there is no correct assignee for a room that spans two.
