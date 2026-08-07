# Authorization Model, Dashboard Summary, and Visibility-Gated Polling

**Status:** Planning (awaiting go-ahead to implement)
**Rollout:** One plan / one branch / one PR
**Order:** Lands **after** [the AMT plan](amt-as-device-property.md)
**Author:** Ivan Volchanskyi

Three changes that depend on each other in that order.

**A — Authorization model.** Organization becomes the visibility boundary and
`is_admin` becomes the mutation boundary. Group ownership stops gating anything
and its column is removed.

**B — `GET /api/v1/devices/summary`.** The Dashboard downloads the entire device
table four times a minute and reads exactly three things from it: `length`,
`status`, and `anomaly_rate`. Every other field on every device — `hostname`,
`os`, `os_display`, `agent_version`, `capabilities`, `group_id`, `last_seen`,
`created_at`, `updated_at`, the four `maintenance_*` columns — is queried,
serialized, transferred, parsed and stored without ever being read on that page.
Replace it with one fixed-size response of eight integers, which also folds in
the separate `maintenance-summary` round trip. A depends-on-B ordering matters:
the summary's SQL is written once, against the new scope.

**C — Visibility-gated polling.** Four `setInterval` pollers run forever in a
hidden tab. Gate every one on document visibility, with a catch-up fire on
re-show.

Measured today, per open tab: Dashboard = 2 requests / 15 s (480/h), one of them
O(fleet) in payload; `/devices` = 1 request / 15 s (240/h), likewise.

---

## Part A — authorization model

### The rule

**Organization is the visibility boundary. `is_admin` is the mutation boundary.**
Every org member sees the same fleet; only configuration is gated.

| Class | Gate | Endpoints |
|---|---|---|
| Fleet reads | org only | devices list/detail/summary, metrics, correlate, history, inventory, hardware, groups list, sessions list |
| Device commands | org only | `POST /sessions`, `DELETE /sessions/{token}`, restart, **maintenance**, AMT power |
| Configuration | **admin** | delete device, move device between groups, group create/update/delete, enrollment tokens, updates publish/push/status/signing-key, users, security groups, org purge |
| Secret-bearing reads | **admin** | `GET /devices/{id}/logs` (ADR-049 — the gate is a structural control, not a convenience), enrollment tokens, update signing key, audit log, user list |

Admins keep **cross-org** reads, unchanged. The dashboard summary is the one
deliberate exception — see Part B.

### What that deletes

`isGroupOwner` ([middleware.go:133-145](../../../server/internal/api/middleware.go#L133-L145))
has no remaining purpose and is removed. Its nine read call sites lose their
check outright; its configuration-write call sites become `denyIfNotAdmin`.

`ListForOwner` ([postgres_device.go:121-135](../../../server/internal/device/postgres_device.go#L121-L135))
goes with it. `ListAll`'s predicate — `org_id = current_setting('app.current_org')::uuid
OR current_setting('app.is_admin', true)::boolean` — already expresses the new
rule for both roles, so `ListDevices` collapses to a single unbranched call.

**`groups_.owner_id` is dropped entirely** (migration `009_drop_group_owner`).
Note the group repository's `List` is *currently owner-filtered*
([postgres_group.go:60](../../../server/internal/device/postgres_group.go#L60)) —
it becomes org-scoped, which is the point. The down migration re-adds the column
as **nullable** (a drop is inherently lossy; a `NOT NULL` restore is impossible
without data), and the ADR-041 down-migration rehearsal must still pass.

Dropping the column is a breaking change to the `Group` schema, where `owner_id`
is currently **required** ([openapi.yaml:540](../../../api/openapi.yaml#L540)). The
web client never reads it — confirmed, no non-generated reference — so the
client-side cost is a regenerate.

### Web

Admin-only controls are **hidden**, not disabled — the same treatment the
Settings link already gets via `user?.is_admin`. That covers Delete Device, the
Move-to-Group panel, and the admin sections' entry points.

---

## Part B — `GET /api/v1/devices/summary`

```json
{"total":42,"online":37,"offline":5,"maintenance":2,
 "health":{"anomalous":1,"watch":3,"healthy":33,"unknown":5}}
```

**Org-scoped for every caller, admins included.** The dashboard describes the
caller's own organization, so the tiles and the health bands always cover one
device set and `unknown` is exact. Admin cross-org reads stay available
everywhere else; this is a dashboard-scope choice, not a permission change.

*Accepted consequence:* for an admin in a multi-org deployment, a dashboard tile
links to `/devices?status=online`, which lists cross-org. Tile counts and list
length can therefore differ for admins. Production runs a single org today, so
this is latent rather than live — but it is a real seam, and if it ever bites,
the fix is a scope selector on the list, not a change here.

Two constant-size round trips inside the handler:

1. **`devices.Counts(ctx)`** — one aggregate row:

   ```sql
   SELECT count(*),
          count(*) FILTER (WHERE status = 'online'),
          count(*) FILTER (WHERE maintenance_on)
     FROM devices
    WHERE org_id = current_setting('app.current_org')::uuid
   ```

2. **`telemetryReader.CountAnomalyBands(ctx, orgID, watch, anomalous, now, lookback)`**
   — one instant query returning up to three labeled scalars:

   ```promql
   label_replace(count(last_over_time(<scoped>[600s]) >= 0.3), "band", "anomalous", "", "")
     or label_replace(count(last_over_time(<scoped>[600s]) >= 0.1 < 0.3), "band", "watch", "", "")
     or label_replace(count(last_over_time(<scoped>[600s]) < 0.1), "band", "healthy", "", "")
   ```

   `<scoped>` comes from the existing `ScopeSelector(selector, orgID)`. The
   PromQL is built inside the `telemetry` package — the port stays typed, no raw
   expressions cross the interface.

   **`count()` over an empty set returns no sample, not zero.** A band with no
   devices is absent from the result; the reader maps missing → 0. This is the
   most likely bug in the change and gets a test against the real VM client, not
   a mock.

3. `offline = total − online` (matches the existing tile, which treats
   `connecting` as offline). `unknown = total − Σbands`, floored at 0.
4. `telemetryReader == nil` → zeroed bands, status counts still returned. Never
   503: the tiles must render.

`maintenance-summary` is **removed** — `summary` is a strict superset, our SPA is
its only consumer, and the count folds into the one aggregate.

Route precedent: `/devices/maintenance-summary` already coexists with
`/devices/{id}`, so the static segment wins the chi match. Covered by a test.

Band thresholds now exist in Go (feeding the PromQL) and TS (per-device badge,
still needed by the grid and detail panel), guarded by a **paired sync test**
that reads the TS literals — the discipline ADR-049 uses for the redaction
corpora.

---

## Part C — visibility-gated polling

`useVisibleInterval(callback, delayMs)`:

- runs `callback` on an interval only while `document.visibilityState === 'visible'`;
- on the hidden→visible edge fires once immediately, then restarts the interval;
- clears on hide and on unmount;
- holds `callback` in a ref so an inline arrow does not restart the interval
  every render.

Replaces the `setInterval` body at four sites: `DeviceList` (15 s), `Dashboard`
(15 s), `DeviceDetail` (30 s), `DeviceMetrics` (30 s). Mount-time initial
fetches stay put — the hook governs the repeat, not the first load.

`DataLifecycle`'s purge-job poll ([DataLifecycle.tsx:47](../../../web/src/features/admin/DataLifecycle.tsx#L47))
stays ungated: a bounded, user-initiated job whose terminal state drives a
completion toast.

## Out of scope

- SSE / event channel, and the conditional-GET generation-counter work. The
  timers stay, just quieter.
- Poll cadence, and consolidating the detail page's two unsynchronized 30 s
  timers.
- Server-side device pagination; operational scripts.

---

## File inventory

### Part A — server

| File | Change |
|---|---|
| `server/internal/db/migrations/009_drop_group_owner.{up,down}.sql` | **new** — drop `groups_.owner_id` + `idx_groups_org_id_owner_id`; down re-adds nullable |
| [`middleware.go`](../../../server/internal/api/middleware.go#L131-L145) | `− isGroupOwner` |
| [`handlers_devices.go`](../../../server/internal/api/handlers_devices.go) | unbranched list; drop read gates |
| `handlers_device_metrics.go`, `handlers_device_correlate.go`, `handlers_device_history.go`, `handlers_device_inventory.go`, `handlers_sessions.go` | drop read gates |
| [`handlers_device_actions.go`](../../../server/internal/api/handlers_device_actions.go) | restart → open; delete + group move → `denyIfNotAdmin` |
| [`handlers_maintenance.go`](../../../server/internal/api/handlers_maintenance.go) | toggle → open |
| [`handlers_groups.go`](../../../server/internal/api/handlers_groups.go) | create/update/delete → `denyIfNotAdmin`; `− OwnerID` |
| [`handlers_amt.go`](../../../server/internal/api/handlers_amt.go) | `AmtPowerAction` → open (list/get already deleted by the AMT plan) |
| [`postgres_device.go`](../../../server/internal/device/postgres_device.go) | `− ListForOwner` |
| [`postgres_group.go`](../../../server/internal/device/postgres_group.go) | `− owner_id` from create/get/list; `List` becomes org-scoped |
| [`device.go`](../../../server/internal/device/device.go) | `− Group.OwnerID`, `− ListForOwner` from the port |
| [`converters.go`](../../../server/internal/api/converters.go#L63) | `− OwnerId` |
| [`testutil.go`](../../../server/internal/testutil/testutil.go#L269) | `− OwnerID` |
| [`api/openapi.yaml`](../../../api/openapi.yaml#L538-L555) | `− Group.owner_id` (and its `required` entry) |

### Part B — server

| File | Change |
|---|---|
| [`api/openapi.yaml`](../../../api/openapi.yaml) | `+ /api/v1/devices/summary`, `+ DeviceSummary`, `+ FleetHealthCounts`; `− /api/v1/devices/maintenance-summary`, `− MaintenanceSummary` |
| [`handlers_devices.go`](../../../server/internal/api/handlers_devices.go) | `+ GetDeviceSummary` |
| [`handlers_device_metrics.go`](../../../server/internal/api/handlers_device_metrics.go) | `+ watchThreshold` / `+ anomalousThreshold` |
| [`api.go`](../../../server/internal/api/api.go#L89-L93) | `MetricsReader`: `+ CountAnomalyBands` |
| [`vm_query.go`](../../../server/internal/telemetry/vm_query.go) | `+ BandCounts`, `+ CountAnomalyBands` |
| [`device.go`](../../../server/internal/device/device.go) | `+ Counts` type + port method; `− CountInMaintenance` |
| [`postgres_device.go`](../../../server/internal/device/postgres_device.go), [`postgres_device_write.go`](../../../server/internal/device/postgres_device_write.go#L110-L119), [`instrumented.go`](../../../server/internal/device/instrumented.go) | `+ Counts`; `− CountInMaintenance` |
| [`handlers_maintenance.go`](../../../server/internal/api/handlers_maintenance.go#L70-L78) | `− GetDeviceMaintenanceSummary` |
| `server/internal/api/openapi_gen.go` | regenerated |

### Tests (written first)

`handlers_device_summary_test.go` (**new**) · `health_bands_sync_test.go`
(**new**) · `authorization_test.go` + `authorization_part2/3_test.go` (rewritten
around the new matrix) · [`vm_test.go`](../../../server/internal/telemetry/vm_test.go)
(`CountAnomalyBands`, incl. the empty-band case) ·
[`handlers_device_metrics_test.go:51`](../../../server/internal/api/handlers_device_metrics_test.go#L51)
(`fakeMetricsReader` gains the method — **compile break if missed**) ·
[`device_part6_test.go:26`](../../../server/internal/device/device_part6_test.go#L26)
(`memDevices`: `− CountInMaintenance`, `+ Counts` — **compile break if missed**) ·
`device_maintenance_test.go` · `handlers_maintenance_test.go:170` ·
`group_handlers_test.go`.

### Web

| File | Change |
|---|---|
| `web/src/lib/use-visible-interval.ts` + test | **new** |
| [`device-store.ts`](../../../web/src/features/devices/state/device-store.ts) | `+ summary` / `+ fetchSummary`; `− maintenanceCount` / `− fetchMaintenanceSummary` |
| [`Dashboard.tsx`](../../../web/src/features/dashboard/Dashboard.tsx) | reads `summary`; drops `fetchDevices`; polls via the hook |
| [`FleetHealth.tsx`](../../../web/src/features/devices/FleetHealth.tsx) | prop `devices` → `counts`; the rollup `useMemo` and `healthBand` import go |
| [`DeviceList.tsx`](../../../web/src/features/devices/DeviceList.tsx#L76-L83), [`DeviceDetail.tsx`](../../../web/src/features/devices/DeviceDetail.tsx#L169-L176), [`DeviceMetrics.tsx`](../../../web/src/features/devices/DeviceMetrics.tsx#L162-L166) | timers → hook |
| `DeviceDetail.tsx`, `DeviceList.tsx`, `GroupSidebar.tsx` | hide admin-only controls behind `user?.is_admin` |
| `web/src/types/api.d.ts` | regenerated |

`FleetHealth` has a single call site (`Dashboard.tsx:69`), so the prop change is
contained.

### Docs

`docs/adr/ADR-062-tenant-scoped-reads-and-fleet-summary.md` (**new**) +
[`decisions.md`](../../decisions.md) row · [`API-Reference.md`](../../../docs/API-Reference.md#L213)
· [`ADR-056`](../../../docs/adr/ADR-056-device-maintenance-mode.md#L90) (mutable —
retarget the D9 fleet-count sentence) · [`phases.md`](../../phases.md#L17).
Archived plans naming `maintenance-summary` are left alone: outside the
doc-links scan, deletion-bound by design.

---

## Implementation steps

Test-first; the TDD gate needs a test edit on the branch before the first source
edit.

1. Rewrite the authorization tests around the new matrix — red.
2. Migration `009`, both directions; run the down-migration rehearsal.
3. Delete `isGroupOwner` and `ListForOwner`; unbranch `ListDevices`; move
   configuration writes to `denyIfNotAdmin`; open commands and maintenance.
4. Drop `owner_id` through repo, converter, handler, testutil and the `Group`
   schema.
5. Write `handlers_device_summary_test.go`, `health_bands_sync_test.go` and the
   `CountAnomalyBands` telemetry tests — red.
6. OpenAPI: add `summary` + schemas, remove `maintenance-summary` + schema, in
   one pass. Regenerate Go and TS.
7. `telemetry`: `BandCounts` + `CountAnomalyBands`; extend `MetricsReader` and
   `fakeMetricsReader`.
8. `device`: `Counts`; implement, wrap, update `memDevices`; delete
   `CountInMaintenance` and its tests.
9. `GetDeviceSummary` + thresholds; delete `GetDeviceMaintenanceSummary`.
10. `cd server && go build ./... && go test ./...`.
11. Web: `useVisibleInterval` (test-first), then the store/Dashboard/FleetHealth
    rewire, then the four timer swaps, then hiding admin-only controls.
12. `cd web && npm run lint && npm test`; `make e2e` — `security-permissions.spec.ts`
    and `admin.spec.ts` both encode the old matrix and will need updating.
13. `make dead-code`; docs + ADR-062 + decisions row.
14. `phases.md` Completed row **and** `git mv` this plan to
    `.claude/plans/archive/`, bumping internal links one `../` deeper, in the
    same commit. Validate with `GO111MODULE=off go run ./scripts/check-doc-links`.
15. `/precommit` → commit → `/refactor` → push.

---

## Reviewer checklist

**Authorization**

- [ ] Two non-admins in one org see an identical device list, detail page,
      metrics, inventory and session list.
- [ ] A non-admin can start a session, restart a device, toggle maintenance and
      send an AMT power action.
- [ ] A non-admin is refused: delete device, move device between groups, group
      create/update/delete, enrollment tokens, updates, users, security groups,
      purge.
- [ ] A non-admin is still refused every secret-bearing read — device logs,
      enrollment tokens, signing key, audit, user list. ADR-049's gate is intact.
- [ ] Admin cross-org reads unchanged outside the dashboard.
- [ ] No `isGroupOwner` and no `ListForOwner` remain anywhere.
- [ ] Migration `009` down-path applies cleanly in the rehearsal.
- [ ] Admin-only controls are absent from a non-admin's DOM, not merely disabled.

**Summary endpoint**

- [ ] Dashboard issues **one** request per poll and never fetches the device list.
- [ ] Response bytes identical with 1 device and with 500.
- [ ] Server work constant: one aggregate row, one instant query. No per-device
      rows cross either boundary.
- [ ] Tiles equal the device list's own count for a non-admin in the same org.
- [ ] A band with zero devices returns `0`, not a missing key — proven against
      the real VM client.
- [ ] `unknown` never negative; `online + offline == total`.
- [ ] `telemetryReader == nil` → zeroed bands, correct status counts, not 503.
- [ ] `GET /devices/summary` does not route into `GetDevice` with `id="summary"`.
- [ ] Go and `health.ts` thresholds agree, and the sync test fails when either
      moves alone.
- [ ] No `maintenance-summary` references outside `plans/archive/`.

**Polling**

- [ ] Hidden tab issues zero requests on all four pollers; re-show fires
      immediately, exactly once, then resumes.
- [ ] `DeviceMetrics` still freezes during a drag selection — the hook must not
      defeat the `selectedWindow` guard.

**General**

- [ ] No `t.Skip` / `.skip` / `#[ignore]` in new tests.
- [ ] Docs describe live state only — no narration of removed gates or endpoints.
