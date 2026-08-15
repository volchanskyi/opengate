# Data Lifecycle — Right-to-be-Forgotten Erasure

Deleting a device or purging a whole tenant **irreversibly erases that subject's
centralized telemetry across every store** and deprovisions its agents. There is
no soft-delete, grace window, or undo. The orchestrator runs server-side only:
the VictoriaMetrics delete key and any object-store credentials never reach the
edge.

The engine lives in [`server/internal/lifecycle`](../server/internal/lifecycle/);
the delete endpoints are in
[`server/internal/api/handlers_purge.go`](../server/internal/api/handlers_purge.go)
and [`handlers_device_actions.go`](../server/internal/api/handlers_device_actions.go).

## What is erased

| Store | Device delete | Tenant/tenant purge |
|-------|---------------|------------------|
| VictoriaMetrics series (numeric host metrics, inline anomaly scores) | scoped delete-series `{tenant_id,device_id}` | `{tenant_id}` |
| Postgres `device_processes`, `device_inventory` (+ the `devices` row) | FK `ON DELETE CASCADE` | every device in the tenant |
| Postgres `alerts` and their evidence | FK `ON DELETE CASCADE`, plus the incident repair below | `alerts`, `incidents` and `incident_events` erased by tenant |
| Cold-tier objects (optional) | device prefix | tenant prefix |
| Agent local store | deprovision → wiped on next reconnect | every agent in the tenant |
| Audit events | **retained** — the erasure proof | retained |

The tenant row itself is retained for a tenant purge: it anchors the
retained audit trail and the deny-list, and enforcing referential integrity
against retained audit events would otherwise block the delete. Nothing cascades
from a retained row, so a tenant's investigations are erased by name rather than
left to the foreign key.

## Repairing what the cascade cannot

An incident's `occurrences` and `device_count` are application state. The foreign
key erases a machine's alerts; it leaves those two numbers describing a machine
that no longer exists, so a technician would read "40 machines" on a room whose
fortieth was decommissioned last week.

[`EraseDeviceAlerts`](../server/internal/alerts/postgres.go) runs inside the
Postgres stage, **before** the device row goes — once the cascade has taken the
alerts there is nothing left to say which incidents the machine was in. It
restates both counts from the rows that survive rather than subtracting, which
is what makes a resumed purge safe to run twice, and closes any incident the
erasure emptied with a `resolution` event saying why. No cause code is set: those
are a person's answer to why an incident ended, and `false_positive` in
particular is the channel that decides whether a rule gets retuned.

## Tombstone deny-list

Every purge first records the subject in the `deleted_ids` table (see
[Database](Database.md)). Every write path checks it, so no live stream,
in-flight backfill, or misbehaving agent can re-create purged data:

- The agent server warms an in-memory deny-list from the table at startup
  ([`AgentServer.WarmTombstones`](../server/internal/agentapi/server.go)) and
  rejects a tombstoned device on connect and on every write-path control message
  ([`conn.go`](../server/internal/agentapi/conn.go)).
- A connected agent is deregistered immediately; an offline one is denied by its
  own id on the next reconnect (a tenant purge records a per-device tombstone for
  each device it finds, so the check needs only the device id).

A tenant tombstone supersedes its device tombstones, and `deleted_ids` carries ids
and purge scope only — never telemetry — so it is retained indefinitely.

## Deletion state machine

A purge is a persisted, resumable job (`purge_jobs`). Logical completion (ingest
blocked and the subject no longer queryable) is distinct from physical
completion — VictoriaMetrics delete-series is
[asynchronous](https://docs.victoriametrics.com/#how-to-delete-time-series) and
frees disk only on a later merge:

```
requested
  → central-logical-complete            (VM delete issued; Postgres rows removed)
  → central-physical-compaction-pending (verifying VM emptiness)
  → object-delete-pending               (cold-tier prefixes, when a cold tier exists)
  → edge-erase-pending                  (agent local wipe, pending reconnect)
  → complete                            (VM verified empty)
```

Ordering is strict — tombstone and scope first, then VM delete, cold-tier
objects, and the Postgres row **last** — so labels and FKs survive while the
stores drain, and a crash mid-purge leaves the subject marked deleted, not
half-alive. Each stage is guarded by a persisted per-store flag, so
[`Orchestrator.Resume`](../server/internal/lifecycle/orchestrator.go) re-runs an
interrupted job idempotently at startup. Completion is gated on a
post-delete emptiness check.

## Reconciliation sweep

A periodic [`Reconciler`](../server/internal/lifecycle/reconcile.go) sweep
garbage-collects any VictoriaMetrics series whose device no longer exists in
Postgres — defense in depth against a purge that partially failed, since the
stores are not one transaction. Device rows are created at handshake before any
ingest, so a series with no row is genuinely orphaned.

## API

- `DELETE /devices/{id}` runs a synchronous device purge (bounded emptiness
  verify; a slow VM compaction falls through to the sweep).
- `POST /tenants/{tenantId}/purge` starts an admin-only, tenant-scoped asynchronous
  purge and returns the job.
- `GET /purge-jobs/{jobId}` reports progress.

See [API Reference](API-Reference.md). Admins drive a tenant purge and watch its
progress from the **Data Lifecycle** settings page
([`web/src/features/admin/DataLifecycle.tsx`](../web/src/features/admin/DataLifecycle.tsx)).

## Backups caveat

VictoriaMetrics keeps no backups, so its erasure is immediate. Postgres
`pg_dump` copies in OCI Object Storage (see [Backups](Database.md)) cannot be
surgically edited: a purged subject is **fully erased only once every backup that
still contains it ages out** under the bucket's Object Storage lifecycle policy.
