# WS-17 — Inventory server store + API (RLS)

**Objective:** Persist `DiscoveryReport` (WS-16) into a tenant-scoped inventory store and expose it
via the API, so the web view (WS-18) can render a device's discovered footprint.

**Dependencies:** WS-0 (tenancy/RLS), WS-16 (DiscoveryReport). **Blocks:** WS-18.

## Context

Discovery data is descriptive/relational (ports, services, DBs, containers, packages) — it belongs in
a **Postgres RLS** table (like the WS-4/WS-11 process/log descriptive tables), not in VM. It is an
**attack-surface map**, so tenancy isolation is mandatory.

## File inventory

- **Create:** `server/internal/db/migrations/00X_inventory.{up,down}.sql` — `device_inventory` RLS
  table(s) (org_id, device, kind, name, version, port, ts), `org_id`-leading indexes.
- **Create:** `server/internal/inventory/` repo — upsert latest inventory per device (tenant-scoped
  via the WS-0 tx helper).
- **Modify:** [`conn.go`](../../server/internal/agentapi/conn.go) — handle `DiscoveryReport` → upsert,
  scoped by the connection's device→org (never trust agent-supplied org).
- **Modify:** [`api/openapi.yaml`](../../api/openapi.yaml) + [`api.go`](../../server/internal/api/api.go) —
  `GET /devices/{id}/inventory` (+ optional fleet search); regen Go + TS.

## Steps (TDD-first)

1. **Test first:** repo tests in `testpg` — upsert + read latest; **RLS cross-tenant-deny**;
   `.down.sql` reverses → write the migration + repo.
2. **Test first:** `conn.go` handler persists a `DiscoveryReport` scoped to the connection's org →
   add the case.
3. **Test first:** API returns a device's inventory; tenant-scoped → add endpoint; regen OpenAPI.

## Gotchas / constraints

- RLS + `org_id`-leading indexes (mirror WS-0 patterns); writes via the tenant-tx helper.
- Upsert "latest" per (device, component) + a **bounded change history** (not every scan); per-device
  row caps so growth is bounded regardless of host size. Descriptive data only — never VM labels.
- No secrets persisted (enforced upstream in WS-16; assert on ingest as defense-in-depth).

## Reviewer checklist

- [ ] `device_inventory` RLS + indexes; cross-tenant-deny; `.down.sql` reverses.
- [ ] Handler scopes to connection device→org; upsert bounds growth.
- [ ] API tenant-scoped; OpenAPI regen committed (Go + TS); `/precommit` green.

## Verification

`cd server && go test ./internal/inventory/... ./internal/agentapi/... ./internal/api/...` (testpg,
RLS deny). `/precommit` green. `/docs`: Database + API pages.
