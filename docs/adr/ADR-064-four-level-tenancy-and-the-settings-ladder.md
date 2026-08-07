---
adr: 064
title: Four-Level Tenancy — Tenant, Organization, Site, Device — and the Settings Ladder
status: Accepted
date: 2026-08-06
---

# ADR-064: Four-Level Tenancy — Tenant, Organization, Site, Device — and the Settings Ladder

## Status

Accepted.

## Context

OpenGate used the word *organization* for the wall the database enforces: the
MSP. Everywhere else in this market the word means the opposite — the customer
an MSP serves — with a location or department beneath it. A technician hired
from another product would read the word backwards on their first day, and the
programme that was being specified on top of it inherited the mistake in two
load-bearing places: an hourly alert ceiling "per organization" would have let
one customer's storm silence every other customer, and incident grouping "per
organization" would have folded two unrelated customers' outages into one.

Underneath the naming, one level was missing and one was misparented. Machine
groups existed as a flat filing label on the tenant, with no customer above them,
so nothing in the system could express "the Dallas office of Contoso" — which is
the unit an operator actually tunes, silences, and reports on.

The cheapest moment to fix all of this was before any of it had data: one seeded
row existed and no screen displayed the word.

## Decision

**Four levels, named the way the market names them.**

| Level | What it is |
|---|---|
| Tenant | The MSP. The wall the database enforces. |
| Organization | One customer inside a tenant. |
| Site | A location or department inside one customer. |
| Device | One machine, in exactly one customer and at most one of its sites. |

**The wall stays at the tenant, and only there.** Organization and site are
structural — they decide what a technician is looking at and what a rule or a
ceiling applies to. They carry the tenant policy like every other tenant-scoped
table and add no second wall of their own. Filtering by customer is a query
concern: a second database-enforced boundary would double the policy surface on
every table for a restriction nobody has asked for.

**The rename and the new levels ship separately.** Roughly 850 references moved
in the rename. Mixing a mechanical rename with new concepts produces a diff
nobody can review, and a failure afterwards cannot be attributed to either half,
so the rename shipped first and behaviour-preservingly.

**A device's site must belong to the device's own customer, and the database
enforces it.** That constraint is over a *pair*, not a value, so it is a
composite foreign key — `devices (organization_id, site_id)` references
`sites (organization_id, id)` — rather than a check the application has to
remember to run. `site_id` stays nullable because an unfiled machine is normal,
and a null referencing column leaves the pair unchecked, which is the wanted
behaviour. Two properties fall out of the same key rather than being coded
separately: deleting a site clears only `site_id`
(`ON DELETE SET NULL (site_id)`), so closing an office unfiles its machines
instead of decommissioning them; and a customer move clears the site in the same
statement, so the office a machine left never travels with it into another
customer's estate.

**Filing is a server-side decision.** A registering agent's site counts only when
it belongs to the customer the device lands in, and a reconnect never refiles.
Without this, a machine moved to another customer comes back naming an office
that customer does not have, the pair refuses the write, and the agent cannot
reconnect at all.

**Settings resolve device → site → organization → tenant → what shipped**, and
that ordering is one shared primitive ([`internal/settings`](../../server/internal/settings/settings.go))
holding the walk and the tie-break and nothing else. Where a configurable value
is *stored* belongs to whatever feature it configures. Resolution is keyed on
identity as well as level, so handing the resolver a whole customer's overrides
cannot let one office's number reach a machine in another.

**One class of setting reads the ladder the other way up, and it is named.** A
customer-wide stop exists to stop something; it must not be undone by a value
someone set on a single machine. `BroadestWins` is that class. Naming it makes
the exception a decision rather than an accident of ordering.

**Site names are unique within their customer**, not across the tenant, because
"Head Office" names a different building for each customer.

## Alternatives considered

**A general settings table with four levels, owned by tenancy.** Rejected after
running four candidate designs against 26 concrete operator situations. It does
nothing the shared-ordering approach does, and cannot express six situations that
one handles: a threshold for 200 file servers spread across six sites (a role is
not a tenancy level), validating a tuned value against the range a rule declares,
staged rollout state, cross-cutting labels, and their tie-break. It also breaks
on the stop switch, where a row on one machine would outrank a customer-wide
stop. Both reference products in this market store per-setting values *with the
thing being configured* rather than in a table beside it.

**Policy objects assigned down the tree, in the shape NinjaOne uses** — a named
bundle of settings, inheriting from a parent bundle and recording only what it
changes. Genuinely stronger wherever a human reads configuration back: "every
threshold in force across Contoso" is answered by opening one object, and a
changed shipped default propagates to every bundle that did not override it.
Weaker where a label cuts across sites, since there is no single place to assign
the bundle to. Not foreclosed: bundles need the same device → site → customer
walk, so this decision is a step toward that shape rather than away from it.

**A second database-enforced wall at the organization.** Rejected: every read
already narrows by customer, and per-technician customer restrictions are not a
requirement yet. Adding a policy to every table for a restriction nobody has
requested is cost without a need.

## Consequences

Existing login tokens name their tenant under a key the current build does not
read, so `ValidateToken` refuses them and the caller is asked to log in again
rather than reaching a handler with no scope.

Two contracts keep the model honest across the whole surface rather than one
endpoint at a time. `TestFleetReadsOfferTheOrganizationFilter` derives its rule
from the specification: any operation whose 200 response is a set of devices, or
a rollup over one, must declare `organization_id`, so a fleet read added later
without the filter fails rather than quietly showing a technician every customer
at once. `TestEveryTenantTableIsProbed` reads back every table carrying
`tenant_id` and insists it is covered by the isolation contract, which is what
forced `sites` into that contract when it landed.

`security_groups` and `security_group_members` are user permission groups — an
unrelated concept sharing the word — and are untouched. The migration rehearsal
asserts they survived the rename.

The edge-first programme's hourly alert ceiling and incident grouping now key on
the customer, so each customer gets its own budget and one customer's rollout
never folds together with another's unrelated outage. Its rule bindings resolve
through this ladder rather than defining their own ordering.

## References

- [`docs/Database.md`](../Database.md) — the schema-level description.
- [`012_sites`](../../server/internal/db/migrations/012_sites.up.sql) and
  [`011_organizations`](../../server/internal/db/migrations/011_organizations.up.sql).
- [ADR-062](ADR-062-tenant-scoped-reads-and-fleet-summary.md) — the visibility and
  mutation boundaries this builds on.
