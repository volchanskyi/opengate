---
adr: 061
title: Intel AMT as a Device Property — SMBIOS-UUID Link and CIRA Tenancy
status: Accepted
date: 2026-07-29
---

# ADR-061: Intel AMT as a Device Property — SMBIOS-UUID Link and CIRA Tenancy

## Status

Accepted.

## Context

Intel AMT was modelled as a parallel collection. `amt_devices` held its own
hostname, model and firmware; the browser fetched the whole list on every device
page and joined it client-side on hostname. Three defects made that chain
unusable end to end.

**No AMT row was ever persisted.** MPS upserts on CIRA connect using the
connection's context, which descends from the root application context — no JWT,
no tenant. The Postgres adapter opens with a tenant lookup and returns
`ErrTenantRequired` before touching the database, so every upsert was logged and
dropped. Nothing under the MPS transport injected a tenant, because a CIRA
connection has no request to inherit one from.

**The tests could not see it.** The MPS test substituted a writer that bypassed
the tenant-scoped transaction helper and hardcoded the default organization, so
the one path that failed in production was the one path never exercised.

**The join key was never populated.** Registration wrote uuid, status and
last-seen only; the hostname column stayed empty, and the WSMAN call that would
have filled it was never invoked. The client-side join compared every device's
hostname against the empty string.

The net effect: the AMT list endpoint returned `[]` in every deployment, by
construction. The in-memory control path still worked — power actions resolve
the connection from a map, not from Postgres — but nothing could tell the UI
which uuid to act on. Discovery was dead; control was fine.

## Decision

Intel AMT becomes a property of the managed device it belongs to.

**The join key is the SMBIOS system UUID.** The agent reads it out of DMI and
reports it with its hardware inventory; on vPro hardware the AMT firmware
presents that same value as its CIRA identity. The match is exact, and — this is
the point — it resolves the device, and through it the organization, which is
precisely what the connection was missing. The same key that makes discovery
work closes the tenancy hole.

The lookup runs cross-org and self-scoped, the same shape as relay-session
teardown in [ADR-059](ADR-059-agent-session-row-lifecycle.md): it supplies its
own admin scope because there is no request tenant to inherit. A UUID that
matches more than one device resolves to nothing — cloned disk images share the
firmware's UUID, and an ambiguous key is not an identity.

**The key is stored, never returned.** It appears in no API response, no
generated TypeScript type, and no UI. It exists to resolve the link.

**An unmatched connection persists nothing.** AMT is a property of a managed
device, so an AMT box with no agent has no organization to store state in. The
connection stays live in memory and the lookup is retried on a timer, so the
machine is adopted the moment its agent registers — no reconnect required.

**Two writers share the hardware row, on disjoint columns.** The agent owns
`system_uuid`, `amt_available` and `amt_version` from the Management Engine
interface; the server's WSMAN query over the CIRA connection owns `amt_model`
and `amt_firmware`. Every write is column-targeted, so neither blanks the other.
`amt_available` rides the wire as a pointer on the Go side so a *stated* false
survives `omitempty` — the server must distinguish "this host has no Management
Engine" from "this agent predates AMT reporting", and only the second preserves
what is already stored.

**`amt_devices` is reduced to connection state** — uuid, device link, org,
status, last seen. The device row owns the hostname; the hardware row owns the
model and firmware.

**The device read carries the AMT property.** Two LEFT JOINs by primary key add
`available`, `status` and `uuid` to the existing device query, so the badge comes
from the payload the page already fetches. The two AMT list endpoints are gone
along with the client-side join.

**The badge keys off capability, not connection.** It shows whenever the hardware
supports AMT, so it never flickers with CIRA state; the tooltip carries
`online` / `offline` / `not connected`. Power buttons need a live tunnel and
appear only when the connection is linked and online.

**Setup instructions moved to `/setup`.** They are static BIOS/MEBx
documentation, identical everywhere, and previously rendered on every non-AMT
device page.

## Consequences

The device detail page issues no AMT request at all — one fewer round trip per
page load, and the badge is correct before any AMT-specific fetch could have
returned.

MPS registration now writes under the resolved device's organization, so AMT
connection state is tenant-isolated by the same RLS policy as everything else,
and the MPS tests drive the real Postgres adapter instead of a substitute that
hid the failure.

`amt_available` reflects the presence of the MEI interface, which means the
hardware *supports* AMT — not that AMT is provisioned. A linked connection row
is the proof of actual activation, and the badge tooltip distinguishes the two.

Migration `008_amt_device_link` links existing AMT rows by hostname within their
organization and discards what cannot be linked; those rows carried only status
and last-seen, and the next CIRA connect recreates them against the device that
claims the system UUID. The down path restores the original column shape.

A host that reports no system UUID — no DMI access, or a firmware placeholder —
can never link an AMT connection. The badge still shows from the agent's
capability reading; only the connection stays unadopted.

Opening power actions to non-admins is out of scope here; the endpoint keeps its
existing admin gate.

## Alternatives considered

**Join on hostname.** What the code attempted. Hostnames are mutable, not unique
across organizations, and the AMT firmware does not report one over CIRA without
an extra WSMAN round trip — so it could not have resolved a tenant even if it
had been populated.

**Keep the AMT list endpoint and fix only the tenancy.** Leaves the browser doing
a client-side join for data the device read can carry for free, and leaves a
second collection to keep consistent.

**Persist unmatched AMT connections under a holding organization.** Creates rows
with no owner, no access-control story, and no way to decide which tenant may see
them. Holding the connection in memory costs nothing and resolves itself.

**Store AMT attributes on `amt_devices`.** Splits hardware facts across two
tables and forces a second read to render the Hardware panel.
