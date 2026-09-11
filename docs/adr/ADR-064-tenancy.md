---
number: 64
title: Four levels of tenancy, and who may read or write
---

# ADR-064 — Four levels of tenancy, and who may read or write

## Context

A managed-services provider has customers, each customer has buildings, and each
building has machines. A single flat tenant could not express any of that, and
settings had nowhere sensible to live.

## Decision

**Four levels: tenant, organization, site, device** — the provider, its
customer, that customer's building, and a machine in it. They are named the way
the market names them.

**The isolation wall stays at the tenant and only there.**
[ADR-041](ADR-041-postgres-row-level-security.md) enforces it. Organization and
site narrow a query; they do not grant anything, and a caller cannot widen its
own view by naming one.

**A device's site must belong to that device's own customer**, enforced by a
composite foreign key rather than by application code. Site names are unique
within a customer, not across the tenant — two customers may each have a
"Head Office".

**Filing is a server-side decision.** A registering agent may suggest a site,
and the suggestion counts only when the server can match it inside that device's
own customer.

**Settings resolve device, then site, then organization, then tenant, then the
shipped default.** One class reads the ladder the other way — a ceiling set at
the tenant that a customer may lower but not raise — and that class is named
where it is defined rather than left for a reader to infer.

**Reading is bounded by tenant membership; changing configuration needs
`is_admin`.** Membership is the whole read gate: a member of the tenant may see
the tenant's machines.

**The dashboard summary is one query.** `GET /api/v1/devices/summary` returns the
counts the fleet header needs in a single aggregate rather than by counting a
list the browser has to fetch. Polling stops while the browser tab is hidden.

## Consequences

The rename and the new levels shipped as separate changes, because roughly 850
references moved and mixing that with new behaviour would have made both
unreviewable.
