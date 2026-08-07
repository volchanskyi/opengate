---
adr: 062
title: Tenant-Scoped Reads, Admin-Gated Configuration, and an O(1) Fleet Summary
status: Accepted
date: 2026-07-30
---

# ADR-062: Tenant-Scoped Reads, Admin-Gated Configuration, and an O(1) Fleet Summary

## Status

Accepted.

## Context

Two problems shared a root cause: the read path was shaped by a per-group owner
that nothing in the product exposed.

**Group ownership gated nothing coherent.** `groups_.owner_id` recorded whoever
created a group. A helper resolved `device → group → group.owner_id` and
compared it to the caller, so a colleague in the same tenant could not
open a device page for a group they had not created — while an ungrouped device
was readable by everyone, because the nil group had no owner to check. The web
client never displayed an owner and offered no way to change one. The result was
an access rule with no user-facing model behind it: two people running the same
fleet saw different fleets, for reasons neither could see or edit.

**The dashboard downloaded the fleet to count it.** It fetched the entire device
table four times a minute and read exactly three things from the result:
`length`, `status`, and `anomaly_rate`. Every other column — hostname, OS, agent
version, capabilities, group, timestamps, the four maintenance fields — was
queried, serialized, transferred, parsed and stored without ever being rendered
on that page. A second round trip fetched the maintenance count. Per open tab
that is 480 requests an hour, half of them O(fleet) in payload, for eight
integers.

Both pollers also ran forever in a background tab, along with two more on the
device pages.

## Decision

### Tenant is the visibility boundary; `is_admin` is the mutation boundary

Every member of a tenant sees the same fleet and may act on any device in
it. Only configuration and secret-bearing reads are gated on admin.

| Class | Gate | Endpoints |
|---|---|---|
| Fleet reads | Tenant | Device list/detail/summary, metrics, correlate, history, inventory, hardware, group list/detail, session list |
| Device commands | Tenant | Session create/delete, restart, maintenance, Intel AMT power |
| Configuration | Admin | Device delete, device group move, group create/delete, enrollment tokens, updates, users, security groups, tenant purge |
| Secret-bearing reads | Admin | Device logs, enrollment tokens, update signing key, audit log, user list |

Visibility is enforced in the repository rather than in a handler branch. Every
read runs inside a transaction that sets `app.current_tenant`, so a resource in
another tenant resolves to "not found" — the boundary is structural and a
handler cannot forget it. The mutation boundary is the `denyIfNotAdmin` helper
at the top of each configuration handler.

`groups_.owner_id` is dropped (migration `009_drop_group_owner`). A group is a
filing label, not an access boundary. Because dropping a column is lossy, the
down migration restores `owner_id` as nullable; the original `NOT NULL` cannot
be recreated without the values it held.

Three named guards make the tenant check explicit where a handler acts on
a resource addressed by a caller-supplied id: `requireDeviceInScope`,
`requireAMTDeviceInScope` and `requireSessionInScope`. Naming the step matters
for more than readability — the adversarial pen-test gate
([ADR-027](ADR-027-adversarial-pentest-precommit-gate.md)) recognises these
identifiers as valid authorization forms, so a new mutating handler with neither
an admin gate nor a scope guard is refused at commit time.

Intel AMT power is the one case where the resource has no tenant of its own: the
CIRA connection map is keyed by AMT UUID alone. The handler resolves the managed
device behind that UUID through the tenant-scoped repository first, so a UUID
outside the caller's tenant is refused before any command is dispatched.
This replaces the previous admin gate, which was standing in for a tenancy check
it could not actually perform — an administrator of one tenant could have
power-cycled another's hardware.

### `GET /api/v1/devices/summary`

One fixed-size response replaces the list download and the separate maintenance
round trip:

```json
{"total":42,"online":37,"offline":5,"maintenance":2,
 "health":{"anomalous":1,"watch":3,"healthy":33,"unknown":5}}
```

Two constant-size reads back it: one aggregate row in Postgres for the status
counts, and one instant query in VictoriaMetrics that counts devices per health
band **inside the time-series store**. No per-device row crosses either
boundary, so the work and the payload are identical for a fleet of one and a
fleet of ten thousand.

`count()` over an empty set returns no sample at all rather than zero, so a band
with no devices is absent from the result and the reader maps missing to 0. That
is the sharp edge of the design and it is tested against a real VictoriaMetrics,
not a mock. `unknown` is the remainder, `total − Σbands`, floored at zero
because a sample can outlive the device row it described.

The summary is **tenant-scoped for every caller, administrators
included** — the one deliberate exception to admin cross-tenant reads. The
dashboard describes the caller's own tenant, so the tiles and the health
bands always cover one device set and `unknown` is exact.

*Accepted consequence:* for an administrator in a multi-tenant deployment,
a tile links to `/devices?status=online`, which lists across tenants, so
tile counts and list length can differ. Production runs a single tenant,
so this is latent rather than live. If it ever bites, the fix is a scope
selector on the list, not a change here.

The band thresholds now exist twice — in Go, feeding the PromQL, and in
TypeScript, classifying the per-device badge the grid and detail panel still
need. A paired sync test reads the TypeScript literals and fails if either copy
moves alone, the discipline
[ADR-049](ADR-049-edge-sentinel-raw-log-privacy.md) uses for the redaction
corpora.

### Polling is gated on document visibility

`useVisibleInterval(callback, delayMs)` runs its callback on an interval only
while the tab is visible, fires once immediately on the hidden→visible edge, and
clears on hide and unmount. It replaces the raw `setInterval` at four sites: the
device list, the dashboard, the device detail page and the device metrics panel.
Mount-time fetches stay with the caller — the hook governs the repeat, not the
first load.

The purge-job poller on the data-lifecycle admin page stays ungated: it is a
bounded, user-initiated job whose terminal state drives a completion toast.

## Consequences

A colleague added to a tenant now sees the fleet immediately, with no
group hand-off. Two people looking at the same deployment see the same thing,
which is what an operations tool has to guarantee.

Admin-only controls are hidden rather than disabled — delete device, the
move-to-group panel, group create/delete, and the drag-to-regroup affordance are
absent from a non-admin's DOM. A control that is visible but refuses on click
teaches the user nothing.

The dashboard's per-poll cost stops tracking fleet size. A background tab issues
nothing on any of the four pollers and catches up once on re-show.

Dropping `owner_id` is a breaking change to the `Group` schema, where it was
required. No client read it.

What this does **not** do: it does not introduce roles between member and
administrator. If per-group or per-device delegation is ever needed, it will be
a deliberate authorization model rather than a side effect of who happened to
create a group.
