# WS-0 — Multi-tenancy foundation + Postgres RLS

**Objective:** Make the whole product multi-tenant. Introduce organizations, add `org_id`
to every tenant table, enforce isolation with Postgres Row-Level Security, and thread tenant
context from JWT → middleware → a per-transaction GUC through the repository layer.

**Dependencies:** none (foundation). **Blocks:** WS-4, WS-5, WS-6, WS-7 (all tenant-scoped).
**Parallel with:** WS-1, WS-2.

## Tenancy isolation scope

RLS logical isolation is the **terminal** tenancy design for OpenGate's audience (internal
orgs / fleet segmentation) and infrastructure (Always-Free single node, shared control-plane
Postgres). Per-tenant physical data planes (per-tenant DB/schema/bucket/KMS/namespace) and an
MSP role/delegation model were considered and **rejected** — no mutually-distrusting tenants
need them and the single free node cannot host them. Three adopted hardening items are folded
in: `org_id`-leading indexes + explicit filters and a policy-based admin bypass (steps below);
the cold-tier/DuckDB prefix-scoping correction lives in `edge-sentinel-ws-7-cold-tier-duckdb.md`.

## Context

Today there is **no** tenancy: authz is `users.is_admin` + `security_groups` RBAC, and the
schema ([`001_initial.up.sql`](../../../server/internal/db/migrations/001_initial.up.sql))
has no `org_id`/RLS. Auth is JWT `Claims{UserID,…}`
([`auth.go:21`](../../../server/internal/auth/auth.go#L21)) over a chi authenticated group
([`api.go:277`](../../../server/internal/api/api.go#L277)); data access is a repository
pattern over `PostgresStore`.

## File inventory

- **Create:** `server/internal/db/migrations/002_multitenancy.{up,down}.sql`
- **Modify:** [`server/internal/auth/auth.go`](../../../server/internal/auth/auth.go) (add `OrgID` to `Claims`, `GenerateToken`, `ValidateToken`)
- **Create:** tenant-context middleware in [`server/internal/api/`](../../../server/internal/api/) (sets `app.current_org`)
- **Modify:** repository layer under [`server/internal/device/`](../../../server/internal/device/) and the `PostgresStore` tx helper — run each query inside a tenant-scoped tx
- **Modify:** web auth/store to carry org context; org switcher for multi-org users

## Steps (TDD-first)

1. **Test first:** a `db`-package test (using `testpg`) that applies `002` and asserts
   **cross-tenant deny** — org A cannot read org B's `devices`/`groups_`; `is_admin` bypass
   works. Then write `002_multitenancy.up.sql`: `organizations` table; `org_id UUID NOT NULL`
   on users, groups_, devices, agent_sessions, audit_events, amt_devices, enrollment_tokens,
   security_groups (+ device_* tables); **backfill** all rows to a seeded default org;
   `ALTER TABLE … ENABLE ROW LEVEL SECURITY` + `FORCE` + policy
   `USING (org_id = current_setting('app.current_org')::uuid)`; **admin cross-org access via a
   policy** that also permits when `current_setting('app.is_admin', true)::bool` — **never a
   `BYPASSRLS` application role** (a missed `SET LOCAL` must fail closed, not leak every org);
   composite **`org_id`-leading indexes** on every tenant lookup/list path. Write `.down.sql`.
2. **Test first:** auth test for `OrgID` round-trip → add `OrgID` to `Claims` + token funcs.
3. **Test first:** middleware test (GUC set within the request tx, cleared after) → implement
   the tenant-context middleware after auth in the authenticated group.
4. **Test first:** per-repo cross-tenant-deny tests → wrap repo methods in a tenant-scoped tx
   that issues `SET LOCAL app.current_org = $1` (a shared `PostgresStore` helper). **Do not**
   use bare `SET` on a pooled connection. Carry an **explicit redundant `WHERE org_id =
   current_setting('app.current_org')::uuid`** in tenant queries so the planner uses the
   `org_id`-leading indexes instead of applying RLS as a post-filter.
5. **Migration rehearsal (Wave 0 gate item):** on a seeded copy, prove backfill correctness,
   RLS cross-tenant deny, `pg_dump`/restore, and a clean `.down.sql` rollback **before** any
   telemetry WS depends on this. Record the rehearsal log.
6. Web: thread org into login/session state; add org switcher.

## Gotchas / constraints

- **The correctness landmine:** `pgxpool` reuses connections. `SET LOCAL` **must** be inside
  a transaction (auto-resets at tx end). A bare `SET` leaks the org to the next request on
  that pooled conn. Every tenant-scoped query path must go through the tx helper.
- **Classify every table before enabling RLS:** fail-closed RLS (no `app.current_org`) will break
  any path that legitimately runs without a tenant. Inventory all ~13 tables (and every repo path)
  as **tenant-scoped** (RLS + policy), **global/system** (no RLS — e.g. cross-org login lookup,
  enrollment-token validation, migrations), **admin-only**, or **audit-retained** (survives tenant
  deletion). Background jobs, enrollment, and auth that must run pre-tenant-context use the global
  classification explicitly — never a silent `BYPASSRLS`.
- **RLS perf is not optional here:** telemetry shares the control-plane Postgres, so RLS
  post-filter cost competes with live relay/WebRTC for the same node CPU. `org_id`-leading
  composite indexes + explicit `WHERE org_id` keep lookups index-driven. Admin bypass is a
  policy checking a second GUC (`app.is_admin`), **never** `BYPASSRLS` on the application role
  (a missed `SET LOCAL` then fails closed, not open).
- RLS interacts with later WS-4 hypertable/continuous aggregates — flag for WS-4.
- This is a large, cross-cutting change; keep it self-contained and fully tested before WS-4.
- **CI grep gate:** forbid tenant-table queries outside the tenant-tx helper (a missed scope is
  a security bug, not a style nit). Note WS-4's numeric path lives in VictoriaMetrics (no RLS);
  its app-layer label scoping gets an analogous grep gate so neither store can be queried
  unscoped.

## Reviewer checklist

- [ ] Every tenant table has `org_id NOT NULL` + FK; RLS `ENABLE`+`FORCE`+policy on all.
- [ ] Default-org backfill is correct and idempotent; `.down.sql` reverses cleanly.
- [ ] GUC set **within tx**; verified no cross-request leak on pooled conns.
- [ ] Cross-tenant-deny test on **every** repo; `is_admin` bypass tested.
- [ ] Admin bypass is **policy-based** (second GUC); **no `BYPASSRLS` application role**.
- [ ] `org_id`-leading composite indexes on tenant lookup/list paths; queries carry explicit `WHERE org_id`.
- [ ] No query path bypasses RLS; web carries org context.
- [ ] Migration rehearsal done (backfill, RLS deny, dump/restore, rollback) before WS-4 depends on it.
- [ ] CI grep gate forbids unscoped tenant-table queries (and unscoped VM reads, per WS-4).

## Verification

`cd server && go test ./internal/db/... ./internal/auth/... ./internal/device/...` (incl.
RLS deny). Migration applies in `testpg`. `/precommit` green. `/docs`: Database + auth pages.
