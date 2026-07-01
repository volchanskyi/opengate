---
adr: 041
title: Postgres RLS Multi-Tenancy
status: Accepted
date: 2026-07-01
---

# ADR-041: Postgres RLS Multi-Tenancy

## Status

Accepted.

## Context

OpenGate previously scoped access through application ownership checks and
administrator flags only. Edge Sentinel telemetry needs a hard tenant boundary
before adding high-volume time-series and historical data paths.

## Decision

Use logical multi-tenancy in the shared PostgreSQL database:

- Add an `organizations` table and `org_id UUID NOT NULL` to tenant-owned tables.
- Seed existing rows into the default organization
  `00000000-0000-0000-0000-000000000002`.
- Enable and force Postgres Row-Level Security on tenant tables.
- Thread tenant scope from JWT claim `org` through API middleware into request
  context.
- Execute repository methods inside transactions that set
  `app.current_org` and `app.is_admin` with `SET LOCAL`.
- Keep explicit `WHERE org_id = current_setting('app.current_org')::uuid`
  predicates and `org_id`-leading indexes on tenant lookup/list paths.
- Permit administrator cross-org reads through RLS policy checks on
  `app.is_admin`, not through a `BYPASSRLS` application role.
- Test the boundary with per-repository cross-tenant deny coverage, a static
  tenant-table SQL scoped-helper gate, and an automated migration rehearsal that
  proves backfill, RLS deny/admin bypass, `pg_dump`/restore, and `.down.sql`
  rollback in a dedicated Postgres container.

Pre-tenant paths, such as login lookup and enrollment-token validation, opt into
the default tenant explicitly. They are not hidden unscoped database reads.

## Consequences

- A missed tenant context fails closed under forced RLS.
- Pooled connections do not leak tenant state because `SET LOCAL` lives inside a
  transaction.
- Repositories have more boilerplate, but the security boundary is close to the
  data and testable with real Postgres.
- A future multi-org UI needs an org-membership/switching API; the current web
  client carries the active org from the JWT.
