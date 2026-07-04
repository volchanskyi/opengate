# WS-20 — Data lifecycle: cascading erasure on device / tenant deletion

**Objective:** When a device is deleted from the web client, or a whole fleet/tenant is deleted,
**immediately and irreversibly purge all of that subject's centralized telemetry** across every
store — VictoriaMetrics series, Postgres descriptive tables, optional cold-tier objects — and
**deprovision the agent** so its local store is wiped on next contact. Right-to-be-forgotten, end to
end, race-safe.

**Dependencies:** WS-0 (org/device ownership + tenant scope), WS-4 (VM series), WS-11 (log-rate
series + audit), WS-17 (inventory), WS-7 (cold tier, if built), WS-14b/15 (agent local purge +
deprovision-on-reconnect). **Wave:** with WS-15b (lifecycle hardening).

## What must be erased per subject

| Store | Device delete | Tenant/fleet delete |
|---|---|---|
| **VM series** (numeric + log-rate + inline anomaly scores) | delete-series `match[]={org_id,device}` | `match[]={org_id}` |
| **Postgres** `device_processes`, `device_inventory` (+ device row) | FK `ON DELETE CASCADE` / explicit | all devices in org + org rows |
| **Cold-tier Parquet** (WS-7, optional) | delete the device prefix | delete the org prefix |
| **Agent local store** (WS-14b) | deprovision → purge on next reconnect | same, every agent in org |
| **Raw logs** | none stored centrally (WS-11) — nothing to purge | none |
| **Audit events** | **retained** (record the erasure; compliance) | retained |

## Decisions (locked)

- **Immediate hard purge** — no soft-delete/grace window, no undo. (Auto-expiry of stale devices and
  export-before-delete are **out of scope** this rollout.)
- **Hard delete** of telemetry; **retain the deletion + completion audit events** (who/what/when/
  scope/verified) — the erasure proof.
- **Tenant/fleet delete = async, resumable job**; device delete = a tracked purge. Both **idempotent**.
- **Authorization:** admin / tenant-owner only; **tenant-scoped** (cannot purge another org); purge
  runs server-side (VM delete-API key + object creds never at the edge).
- **Reinstall/re-enroll = a new device** (new id; old series purged, old id tombstoned).

## Edge cases & correctness (the backbone)

- **Resurrection prevention via a tombstone / deny-list.** Step 1 of any purge records the deleted
  device-id / org-id in a compact **deleted-ids table** (retained **indefinitely** — just ids) and
  captures the purge scope (matchers, prefixes). **Every write path — live ingest, backfill ingest,
  scheduler slot-grant — checks the tombstone and rejects**, so no live stream, in-flight backfill,
  or misbehaving/compromised agent can re-create purged data.
- **Strict ordering:** tombstone + capture scope **first** → purge stores → delete the Postgres
  device/org row **last** (labels/FKs still exist during purge; a crash mid-purge leaves the subject
  marked deleted, not half-alive).
- **Concurrency:** delete cancels any active **backfill slot / on-demand pull** for the subject and
  rejects in-flight batches; a concurrent **correlation query** returns empty for that id (never
  errors the whole query); concurrent device+tenant deletes are idempotent (org tombstone supersedes).
- **Explicit deletion state machine (operator-visible):** `requested → central-logical-complete →
  central-physical-compaction-pending → object-delete-pending → edge-erase-pending → complete`.
  "Logical" (ingest blocked + no longer queryable) is distinct from "physical" — **VM delete-series
  is async and does not free disk until later merges** ([proof](https://docs.victoriametrics.com/#delete-series)),
  so the UI shows logical-done immediately and physical/edge as pending. Offline-edge erasure is
  explicitly **pending-reconnect** with operator-visible status.
- **VM delete protections:** the delete API is guarded by **`-deleteAuthKey`** (server-side only),
  **low concurrency**, and a **max selector size**; selectors **always include `org_id`** (tested).
- **Partial failure / durability:** the orchestrator job is persisted with **per-store progress**,
  resumes after a server crash; **VM delete-series is async** → retry with backoff and **verify
  emptiness** before completing; a **post-purge verification** across all stores gates job completion.
- **Reconciliation GC sweep** (periodic): purge VM series / objects whose device-id no longer exists
  in Postgres — defense-in-depth against any earlier partial failure (the stores aren't one tx).
- **Agent/NAT:** the deprovision instruction is **re-sent on every connect until acked** (agent may
  drop first); a never-reconnecting host is harmless (central purge already complete); a misbehaving
  agent is denied at ingest by the tombstone regardless.
- **Backups caveat (documented SLA):** `pg_dump`→OCI PAR backups (ADR-035) can't be surgically
  purged; erasure is **fully complete once all backups containing the subject age out** via the
  bucket lifecycle policy. (VM has no backups, so only Postgres backups carry this.)

## File inventory

- **Create:** `server/internal/lifecycle/` — the **purge orchestrator** (persisted job, per-store
  fan-out, verification, idempotent + resumable) and the **tombstone/deleted-ids** store + a periodic
  **reconciliation GC** sweep.
- **Modify:** ingest paths (WS-4 VM client, WS-11 log-rate, WS-15 backfill + scheduler slot-grant) —
  **tombstone check rejects tombstoned ids**.
- **Modify:** [`api/openapi.yaml`](../../api/openapi.yaml) + [`api.go`](../../server/internal/api/api.go)
  — `DELETE /devices/{id}` and a tenant-delete path trigger the orchestrator (admin/tenant-scoped);
  return job status for the async tenant case. Regen Go + TS.
- **Modify:** the WS-4 VM client — **scoped delete-series** (server-side admin key), verify completion.
- **Modify:** migrations — FK `ON DELETE CASCADE` (org→devices→`device_processes`/`device_inventory`);
  the deleted-ids/tombstone table.
- **Modify:** WS-7 object client (if cold tier exists) — delete the tenant/device prefix.
- **Modify:** [`conn.go`](../../server/internal/agentapi/conn.go) — on connect, a tombstoned/unknown
  device gets the deprovision/purge-local instruction (capability-gated), re-sent until acked.
- **Modify:** web delete flows ([`DeviceList.tsx`](../../web/src/features/devices/DeviceList.tsx) +
  tenant admin) — confirm dialog (irreversible) + progress for the async tenant purge.

## Steps (TDD-first)

1. **Test first:** tombstone — after delete, live ingest / backfill / slot-grant for that id are
   **rejected**; ordering puts tombstone before purge and device-row deletion last → implement.
2. **Test first:** orchestrator — device purge fans out (VM delete + Postgres cascade + object
   prefix); idempotent; **resumes after a simulated mid-purge crash**; verification gates completion.
3. **Test first:** tenant purge — all devices' series/rows/objects gone; **other orgs untouched**;
   async job reports completion → implement.
4. **Test first:** concurrency — delete during active backfill cancels the slot + rejects in-flight;
   correlation returns empty not error → implement.
5. **Test first:** authz — non-admin / cross-tenant denied; **deletion + completion audit events
   retained** → implement.
6. **Test first:** deleted-device reconnect → deprovision (re-sent until acked) → agent purges local +
   stops (WS-14b/15) → implement.
7. **Test first:** reconciliation GC purges an orphaned VM series with no Postgres device → implement.

## Reviewer checklist

- [ ] Tombstone created first, retained indefinitely; **all** write paths reject tombstoned ids.
- [ ] Ordering: tombstone+scope → purge stores → delete device/org row last.
- [ ] Purge covers all stores (VM series, Postgres, cold objects, agent local); idempotent + resumable.
- [ ] VM delete scoped + verified; Postgres FK cascade tested; post-purge verification gates completion.
- [ ] Concurrency (backfill/pull/correlation) handled; deprovision re-sent until acked.
- [ ] Reconciliation GC sweep for orphans; authz admin/tenant-scoped + cross-tenant deny.
- [ ] Deletion + completion audit retained; backups-expiry caveat documented; OpenAPI regen; `/precommit` green.

## Verification

`cd server && go test ./internal/lifecycle/... ./internal/api/... ./internal/agentapi/...` (testpg +
throwaway VM); `make e2e` (web delete device + tenant → series/rows gone, ingest rejected after,
audit retained). Manual: delete a device mid-backfill → slot cancelled, series purged, a reconnecting
purged agent wipes local + stops; orphaned series GC'd. `/precommit` green. `/docs`: Database + API +
a data-lifecycle page (incl. the backups-expiry erasure SLA).
