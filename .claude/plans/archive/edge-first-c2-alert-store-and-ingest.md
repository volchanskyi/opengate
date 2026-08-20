# EF-C2 — `014_investigations`: the RLS alert store, accounted ingest, and the erasure cascade

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.6, §7.4, §7.6 (I1, I5, I6, I9),
D28, E7, E9, E13, E14, E19, steps 16 and 18 (erasure half).
**Acceptance criteria owned:** **C1**, **C7**, **C8**.
**Dependencies:** EF-C1 (the wire).
**Blocks:** EF-C3, EF-C4, EF-C5.

## Schema (from §7.4 — verified: **011 is free**, 001–009 exist and EF-B9 takes 010)

Every table carries **both** `tenant_id` (isolation) and `organization_id` (scoping) — see the
master plan's tenancy levels. They answer different questions and neither substitutes for the other.

`alerts` — `id, tenant_id, organization_id, device_id → devices(id) ON DELETE CASCADE, rule_id, rule_version, severity,
metric, value, window_start, window_end, observed_at, received_at, backfilled, incident_id,
evidence bytea, evidence_codec`, with **`UNIQUE (device_id, rule_id, rule_version, window_start)`**
as the idempotency key.

`incidents` — `id, tenant_id, organization_id, rule_id, scope, scope_key, severity, status, assignee_id, opened_at,
first_seen, last_seen, resolved_at, cause_code, occurrences, device_count`, plus the **partial unique
index** `(organization_id, rule_id, scope, scope_key) WHERE status <> 'resolved'` that makes EF-C3's fold
race-safe.

`incident_events` — `id, tenant_id, organization_id, incident_id → incidents(id) ON DELETE CASCADE, at, kind, actor_id,
body jsonb`, `kind ∈ {alert_folded, status_change, assignment, comment, device_offline, resolution}`.

All three: **forced RLS on `tenant_id`**, with `tenant_id`- and `organization_id`-leading indexes, mirroring
[005_inventory](../../../server/internal/db/migrations/005_inventory.up.sql). `severity` and
`cause_code` are constrained to §6.6's closed sets by **check constraints**, not application
convention (D30).

## File inventory

- **Create:** `server/internal/db/migrations/014_investigations.{up,down}.sql`.
- **Create:** `server/internal/alerts/` — store + types. Arch-lint component `mayDependOn: [dbtx]`,
  mirroring `inventory` in [.go-arch-lint.yml](../../../server/.go-arch-lint.yml#L185-L189).
- **Create:** `server/internal/agentapi/conn_alerts.go` — `AgentAlert` ingest with I1 accounting,
  validation, and the **per-organization 500 alerts/h** ceiling — per customer, never per tenant.
- **Modify:** [metrics.go](../../../server/internal/metrics/metrics.go) — `opengate_alerts_suppressed_total{reason}`
  (this plan is its producer; EF-C4 adds the rest of §6.7's counters).
- **Modify:** [lifecycle](../../../server/internal/lifecycle/) — the erasure cascade and the emptied
  incident rule.
- **Modify:** [.go-arch-lint.yml](../../../server/.go-arch-lint.yml),
  [scoped_sql_test.go](../../../server/internal/dbtx/scoped_sql_test.go) (tenant-table gate),
  [store_part4_test.go](../../../server/internal/db/store_part4_test.go) (migration rehearsal).
- **Docs:** [Database.md](../../../docs/architecture/Database.md), [Data-Lifecycle.md](../../../docs/product/Data-Erasure.md).

## Steps (TDD-first)

1. **Test first — migration:** `011` up/down round-trips; forced RLS on all three tables; the check
   constraints reject an out-of-set `severity` and `cause_code` **at the database**, not in Go; the
   partial unique index exists and permits two resolved incidents with the same key. Extend the
   existing rehearsal rather than adding a parallel one.
2. **Test first (C1):** an alert and its evidence are stored **in one transaction** — assert that a
   failure writing evidence leaves **no** alert row (drive it with a forced error, not by reading the
   code). Then: a duplicate on `(device_id, rule_id, rule_version, window_start)` is a **no-op**, not
   an error and not a second row (E7 — a reconnect replays).
3. **Test first (C7):** a cross-tenant read is denied, **including via a crafted grouping key** — an
   org-B caller supplying org-A's `scope_key` and `rule_id` resolves to *not found*, not to a row.
   RLS enforces it in the repository, so the test drives the real scoped path.
4. **Test first (I1):** every ingested `AgentAlert` either produces a row or increments a typed
   drop — reuse EF-A1's identity, do not invent a second accounting scheme. Cover: unknown rule id,
   malformed timestamps, evidence that fails to decode, missing idempotency components, severity out
   of set, ceiling suppression.
5. **Test first (E9) — the organization ceiling:** at **500 alerts/h** per organization the excess is
   suppressed, counted under `opengate_alerts_suppressed_total{reason="organization_ceiling"}`, and
   folded into **one**
   storm incident carrying a count — never silently dropped. Assert the count is preserved and that
   the window is a rolling hour, not a calendar one.
6. **Test first (E19):** when Postgres is unavailable the alert is **not acknowledged** — assert the
   agent-visible outcome, since "never acknowledged as stored when it is not" is the contract that
   makes the edge's retry safe.
7. **Test first (C8, E13):** purging DAL-WS-012 erases its alerts and evidence, leaves the incident
   with `device_count = 39`, and **closes an incident that ends up empty**. Then (E14) an org purge
   cascades fully with no orphan incidents. Extend the lifecycle rehearsal; the orchestrator's
   ordering (tombstone → VM → cold → Postgres **last**) is settled by ADR-054 and is not re-designed
   here.
8. Implement store, ingest, ceiling, cascade; register both new tables in the tenant-table gate; docs.

## Traps

- **Confirm 011 is still free** at implementation time (`ls server/internal/db/migrations/`) — EF-B9
  lands 010 and a parallel plan could take 011.
- `ON DELETE CASCADE` erases alert rows when a device row goes, but **`device_count` and
  `occurrences` on the incident are application state** — the cascade will not fix them. That is the
  whole substance of C8; a passing FK test proves nothing about it.
- Evidence is `bytea` and immutable. Never `UPDATE` it; a rewritten blob breaks I3's frozen-snapshot
  guarantee.
- The tenant-table gate enumerates scoped tables — a table missing from it passes every test while
  being readable cross-tenant.
- The ceiling is per **organization** (customer) here, **not per tenant** — at the tenant one
  customer's storm would consume every other customer's budget. The per-device 20/h ceiling is the
  agent's (EF-B6). Implementing
  half of the other one on this side gives two answers to "was this suppressed".
- Alerts arrive on the read-loop goroutine like all control messages; a synchronous Postgres write
  there blocks the control stream — reuse the existing bounded persist-slot pattern rather than
  inventing a new concurrency shape.

## Out of scope

Grouping and lifecycle transitions (EF-C3). The remaining §6.7 counters (EF-C4). The API (EF-C5).
Age-based retention deletion — **explicitly deferred** by §14.3; do not add a sweep.

## Reviewer checklist

- [ ] Migration up/down, forced RLS, check constraints at the DB, partial unique index.
- [ ] Alert + evidence in one transaction, proven by a forced mid-transaction failure.
- [ ] Duplicate ingest is a no-op; crafted-key cross-tenant read denied.
- [ ] Every drop path typed and counted; ceiling suppression counted and folded with a count.
- [ ] Purge leaves `device_count = 39` and closes an emptied incident; org purge leaves no orphans.
- [ ] Both tables registered in the tenant-table gate; `go-arch-lint` clean.

## Verification

`cd server && go test ./internal/alerts/... ./internal/db/... ./internal/agentapi/... ./internal/lifecycle/... ./internal/dbtx/...`
(testpg-backed), `make lint`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
