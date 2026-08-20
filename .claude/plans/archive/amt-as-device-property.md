# Intel AMT as a Device Property

**Status:** Implemented
**Rollout:** One plan / one branch / one PR — the tenancy repair ships in the same commit as the feature
**Order:** Lands **before** [the authorization + dashboard plan](../dashboard-summary-and-visibility-gated-polling.md)
**Author:** Ivan Volchanskyi

Intel AMT stops being a separate collection. A device that supports AMT carries a
blue **Intel AMT** badge beside its status on the device detail page, sourced
from the device's own payload — no second list, no client-side join, no
`GET /amt/devices` request on page load.

Getting there means repairing a chain that is broken in three places today.

---

## The defect being repaired

Verified by reading the code, not inferred:

**1. No AMT row is ever persisted.** [`registerConn`](../../../server/internal/amt/transport/mps.go#L157-L169)
upserts on CIRA connect using the connection context, which descends from
`mpsSrv.ListenAndServe(ctx, …)` at [main.go:333](../../../server/cmd/meshserver/main.go#L333)
— the root app context, no JWT, no tenant. But
[`PostgresAMTDevices.Upsert`](../../../server/internal/amt/postgres.go#L26-L30)
opens with `dbtx.TenantFromContext(ctx)` and returns `ErrTenantRequired` before
touching the database. `registerConn` logs and continues. Nothing under
`internal/amt/transport/` injects a tenant — the whole subtree has zero `dbtx.`
references.

**2. The tests cannot see it.** [`mps_test.go:29`](../../../server/internal/amt/transport/mps_test.go#L29)
substitutes a writer that bypasses `dbtx.Scoped` and hardcodes
`dbtx.DefaultOrgID`. Its own comment notes that production threads
`amt.PostgresAMTDevices` instead — which is exactly the path that fails.

**3. The join key is never populated.** `registerConn` writes `UUID`, `Status`,
`LastSeen` only; `hostname` stays `''`. The WSMAN call that would supply it,
[`GetGeneralInfo`](../../../server/internal/amt/transport/wsman/operations.go#L85),
is never invoked anywhere. So the client-side join
`amtDevices.find(a => a.hostname === device.hostname)` compares against `''`.

Net effect: `GET /api/v1/amt/devices` returns `[]` in every production
deployment, by construction. The in-memory control path still works —
[`PowerAction`](../../../server/internal/amt/service.go#L37-L43) resolves
`s.mps.GetConn(uuid)` from memory, not Postgres — but nothing can tell the UI
which uuid to act on. **Discovery is dead; control is fine.**

---

## Locked decisions

| Topic | Decision |
|---|---|
| Identity join | **SMBIOS system UUID.** The agent reports it in its hardware inventory; on vPro the AMT CIRA UUID is that same value. Exact match, and it resolves the AMT connection's org — closing the tenancy hole with the same key. |
| System UUID exposure | **Stored, never returned.** Join key only: not in `DeviceHardware`, not in any API response, not in the UI. |
| Unmatched AMT connection | **Hold in memory, persist nothing.** AMT is a property of a managed device, so an AMT box with no agent has no home. The connection stays live and is re-resolved on each keepalive tick, so it is adopted once the agent registers. |
| AMT hardware attributes | **Live under hardware data**, not on `amt_devices`. `device_hardware` gains the AMT columns; `amt_devices` keeps only connection state. |
| Data source | **Both.** The agent reports *presence* (`system_uuid`, `amt_available`, `amt_version`) from the MEI interface; the CIRA/WSMAN connection fills *detail* (`amt_model`, `amt_firmware`) via `GetGeneralInfo`. Two writers, disjoint columns. |
| Badge rule | Shows whenever the device **supports** AMT (`amt.available`), independent of CIRA connection state, so it never flickers. |
| Setup instructions | **Moved to the `/setup` page.** They are static BIOS/MEBx documentation, not device state, and today they render on every non-AMT device. |
| Sequencing | Tenancy repair and badge feature ship in **one commit**. |
| Validation | **Test harness only**, no manual hardware step. The harness drives registration through the real `PostgresAMTDevices` — which is what would have caught this. |

### Resolved conflict

An earlier decision put the machine model in the badge tooltip. "AMT hardware
attributes live under hardware data" supersedes it: **model and firmware render
in the Hardware panel** alongside CPU/RAM, and the badge tooltip carries
connection state only (`Intel AMT · online` / `· offline` / `· not connected`).
Keeps the badge cheap — it needs nothing beyond the device payload the page
already fetches.

## Out of scope

- Opening `POST /amt/devices/{uuid}/power` to non-admins — that belongs to the
  authorization plan, which lands after this one. Until then the endpoint keeps
  its existing `denyIfNotAdmin` gate.
- Reading AMT *provisioning* state over the MEI protocol. `amt_available` means
  the MEI interface is present; a linked `amt_devices` row is the proof of
  actual activation, and the badge tooltip distinguishes the two.
- A CIRA protocol simulator.

---

## Data model

**`device_hardware`** — additive (migration `008_amt_device_link`):

| Column | Source | Notes |
|---|---|---|
| `system_uuid UUID` | agent | SMBIOS UUID; indexed; **never** returned by the API |
| `amt_available BOOLEAN NOT NULL DEFAULT false` | agent | MEI interface present |
| `amt_version TEXT NOT NULL DEFAULT ''` | agent | ME/AMT version from MEI |
| `amt_model TEXT NOT NULL DEFAULT ''` | AMT (WSMAN) | machine model |
| `amt_firmware TEXT NOT NULL DEFAULT ''` | AMT (WSMAN) | AMT firmware version |

Two writers share the row, so **both upserts must be column-targeted**: the
agent's hardware report must not blank the AMT-sourced columns, and vice versa.
The existing `CASE WHEN EXCLUDED.x = '' THEN <old> ELSE EXCLUDED.x END` idiom in
[`amt/postgres.go`](../../../server/internal/amt/postgres.go#L33-L41) is the pattern
to copy. This is the single most likely regression in the change and gets a
dedicated test.

**`amt_devices`** — reduced to connection state:

```
uuid      UUID PRIMARY KEY        -- CIRA identity (= SMBIOS system UUID)
device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE   -- new
org_id    UUID NOT NULL
status    TEXT NOT NULL
last_seen TIMESTAMPTZ NOT NULL
```

`hostname`, `model`, `firmware` are dropped — the device row owns hostname, and
the hardware row now owns the rest. The down migration re-adds them with their
original defaults.

**`Device` API schema** — gains an optional `amt` object, omitted entirely when
the device neither supports AMT nor has a link:

```json
"amt": { "available": true, "status": "online", "uuid": "…" }
```

`available` comes from `device_hardware`; `status`/`uuid` from `amt_devices`
when linked (both null when not). Built with two LEFT JOINs by primary key in
the existing device read — cheap, and it replaces a whole separate round trip.

## Registration flow

1. CIRA handshake completes; AMT UUID known.
2. Resolve `device_hardware.system_uuid = <amt uuid>` → `device_id`, then that
   device's `org_id`. This lookup runs **cross-org and self-scoped**, the same
   shape as `DeleteRelaySession` in ADR-059, because a CIRA connection has no
   request tenant to inherit.
3. **No match** → keep the connection in memory, persist nothing, log at info.
   Re-resolve on each keepalive tick so a later agent registration adopts it.
4. **Match** → build a tenant context from the resolved org and upsert
   `amt_devices`; then issue `GetGeneralInfo` and write `amt_model` /
   `amt_firmware` into that device's hardware row.
5. Disconnect → `SetStatus(offline)` as today, now with a real tenant.

---

## File inventory

### Agent (Rust)

| File | Change |
|---|---|
| `agent/crates/mesh-agent-core/src/hardware.rs` (or the module that builds the inventory) | `+ system_uuid`, `+ amt_available`, `+ amt_version` |
| `agent/crates/mesh-agent-core/src/amt_detect.rs` | **new** — MEI presence + version; Linux `/dev/mei0`, Windows MEI device, no-op elsewhere |
| [`agent/crates/mesh-protocol/src/control.rs`](../../../agent/crates/mesh-protocol/src/control.rs) | three additive fields on the hardware-inventory message |
| `agent/crates/mesh-protocol/tests/golden_test.rs`, `reverse_golden_test.rs` | regenerated goldens |

Additive agent→server fields are tolerated by the forward-compatibility rule in
ADR-042, so no capability gate is needed.

### Server (Go)

| File | Change |
|---|---|
| [`server/internal/db/migrations/008_amt_device_link.{up,down}.sql`](../../../server/internal/db/migrations/) | **new** — hardware columns + index, `amt_devices.device_id`, drop three columns |
| [`server/internal/protocol/`](../../../server/internal/protocol/) | decode the three new fields; golden round-trip |
| [`server/internal/device/postgres_hardware.go`](../../../server/internal/device/postgres_hardware.go) | column-targeted upsert; `+ ResolveBySystemUUID`; `+ SetAMTDetail` |
| [`server/internal/device/device.go`](../../../server/internal/device/device.go) | hardware struct + repository port additions |
| [`server/internal/amt/transport/mps.go`](../../../server/internal/amt/transport/mps.go#L157-L169) | resolve → tenant → upsert; unmatched path; keepalive re-resolution; `GetGeneralInfo` call |
| [`server/internal/amt/postgres.go`](../../../server/internal/amt/postgres.go) | `device_id` column; drop the three removed ones |
| [`server/internal/api/handlers_amt.go`](../../../server/internal/api/handlers_amt.go) | `− ListAMTDevices`, `− GetAMTDevice`; `AmtPowerAction` stays |
| [`server/internal/api/handlers_devices.go`](../../../server/internal/api/handlers_devices.go), [`converters.go`](../../../server/internal/api/converters.go) | populate `Device.amt` |
| [`api/openapi.yaml`](../../../api/openapi.yaml) | `+ Device.amt` / `+ DeviceAMT`; `+ DeviceHardware.amt_*`; `− /api/v1/amt/devices`, `− /api/v1/amt/devices/{uuid}`, `− AMTDevice` schema |
| `server/internal/api/openapi_gen.go` | regenerated |

### Server tests (written first)

| File | Change |
|---|---|
| [`server/internal/amt/transport/mps_test.go`](../../../server/internal/amt/transport/mps_test.go#L29) | **delete `pgAMTState`** and drive registration through the real `amt.PostgresAMTDevices` — the substitution is what hid the bug |
| `server/internal/amt/transport/mps_link_test.go` | **new** — resolve-by-system-uuid, org inheritance, unmatched-connection persistence is a no-op, adoption on a later keepalive |
| `server/internal/device/hardware_amt_test.go` | **new** — agent upsert does not blank AMT columns; AMT write does not blank agent columns |
| [`server/internal/api/device_handlers_test.go`](../../../server/internal/api/device_handlers_test.go) | `Device.amt` shape: absent / available-unlinked / linked-online |
| [`server/internal/api/amt_handlers_test.go`](../../../server/internal/api/amt_handlers_test.go) | drop list/get coverage; keep power |

### Web

| File | Change |
|---|---|
| `web/src/features/devices/AmtBadge.tsx` + test | **new** — blue badge, tooltip by link state |
| [`web/src/features/devices/DeviceDetail.tsx`](../../../web/src/features/devices/DeviceDetail.tsx) | drop `fetchAmtDevices` + the hostname join; badge beside `StatusBadge`; power buttons keyed off `device.amt`; instructions block removed |
| [`web/src/features/devices/state/amt-store.ts`](../../../web/src/features/devices/state/amt-store.ts) + test | **deleted** — power action moves to the device store |
| [`web/src/features/agent-setup/AgentSetupPage.tsx`](../../../web/src/features/agent-setup/AgentSetupPage.tsx) | receives the Intel AMT setup instructions |
| `web/src/types/api.d.ts` | regenerated |

### Docs

`docs/adr/ADR-061-amt-as-device-property.md` (new) + [`decisions.md`](../../decisions.md)
row; [`docs/API-Reference.md`](../../../docs/architecture/API-Reference.md) AMT section;
[`phases.md`](../../phases.md) Completed row.

---

## Implementation steps

1. Failing tests first: protocol goldens, `mps_link_test.go`, hardware
   dual-writer test, `Device.amt` handler tests, `AmtBadge` test.
2. Migration `008`, both directions; run the down-migration rehearsal.
3. Rust: MEI detection + three inventory fields; regenerate goldens.
4. Go protocol decode; hardware repo columns, `ResolveBySystemUUID`,
   `SetAMTDetail`, column-targeted upserts.
5. MPS path: resolve → tenant → upsert; unmatched handling; keepalive
   re-resolution; `GetGeneralInfo` detail write.
6. `Device.amt` in the converter; OpenAPI edits; regenerate Go + TS.
7. Delete the two AMT list endpoints and `amt-store.ts`; move power action into
   the device store.
8. Badge component; wire into `DeviceDetail`; move instructions to `/setup`.
9. `make golden`, `cd server && go test ./...`, `cd web && npm test`.
10. `make dead-code`; docs + ADR-061 + decisions row.
11. `phases.md` Completed row **and** `git mv` this plan to
    `.claude/plans/archive/`, bumping internal links one `../` deeper, in the
    same commit. Validate with `GO111MODULE=off go run ./scripts/check-doc-links`.
12. `/precommit` → commit → `/refactor` → push.

---

## Reviewer checklist

- [ ] Device detail page issues **no** AMT request — the badge comes from the
      device payload.
- [ ] A CIRA connect with a matching `system_uuid` persists a row **with the
      device's org**, and the test proves it through the real repository, not a
      substitute writer.
- [ ] A CIRA connect with no match persists nothing, keeps the connection, and
      is adopted on a later keepalive once the agent registers.
- [ ] `system_uuid` appears in **no** API response — grep the generated TS types
      and the OpenAPI spec to confirm.
- [ ] Agent hardware report does not blank `amt_model` / `amt_firmware`; the AMT
      write does not blank the agent's columns. Both directions tested.
- [ ] Badge shows for an AMT-capable device that has never dialled in, with the
      "not connected" tooltip; power buttons stay hidden until linked **and**
      online.
- [ ] Golden files regenerated and Rust↔Go round-trip passes with the three new
      fields.
- [ ] Migration `008` down-path applies cleanly in the rehearsal.
- [ ] Setup instructions render on `/setup` and nowhere else.
- [ ] No `t.Skip` / `.skip` / `#[ignore]` in the new tests.
- [ ] Docs describe the live model only — no narration of the removed AMT list.
