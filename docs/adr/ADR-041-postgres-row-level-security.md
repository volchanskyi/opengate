---
number: 41
title: Row-level security is the tenant wall
---

# ADR-041 — Row-level security is the tenant wall

## Context

One database holds every tenant's machines, alerts and audit records. A missing
`WHERE` clause in one query would show one customer another's fleet.

## Decision

**PostgreSQL row-level security, forced, on every tenant-owned table.** Forced
means the table owner is subject to it too, so a mistake in application code
cannot step around it.

**The tenant comes from the token.** The `tenant` claim is read by API
middleware into the request context, and every repository call runs inside
`dbtx.Scoped`, which sets `app.current_tenant` and `app.is_admin` with
`SET LOCAL` for the life of the transaction.

**The predicate is written anyway.** Queries keep an explicit
`WHERE tenant_id = current_setting('app.current_tenant')::uuid` and a
tenant-leading index. Row-level security is the wall; the predicate is what
makes the query use an index rather than filter after the fact.

**Administrators cross tenants through a policy, not a privileged role.** The
policy tests `app.is_admin`. No application role carries `BYPASSRLS`, so there
is no connection that can see everything by accident.

**Application traffic uses a non-superuser role** created by
[`zz-app-role.sh`](../../deploy/helm/opengate/files/zz-app-role.sh). The
original role stays for maintenance and backups.

## Consequences

A new tenant-owned table needs its `tenant_id` column, its policy and its index
in the same migration, or reads from it are unprotected.
