---
number: 14
title: PostgreSQL is the database
---

# ADR-014 — PostgreSQL is the database

## Context

The server stores tenants, devices, sessions, alerts and audit records, and
several of those tables are written concurrently by the API and by the agent
control path.

## Decision

**PostgreSQL 17, reached through `jackc/pgx/v5/stdlib` over `database/sql`.**
The driver is registered once; the rest of the server uses the standard
interface, so no repository depends on a PostgreSQL-specific type.

**Native column types throughout** — `TIMESTAMPTZ`, `UUID`, `JSONB`, `BOOLEAN`,
`BIGINT`. Application code no longer parses strings into times or identifiers.

**In-cluster, as a StatefulSet in the application chart**
([`postgres-statefulset.yaml`](../../deploy/helm/opengate/templates/postgres-statefulset.yaml)),
rather than a managed service. Storage is sized by
[ADR-035](ADR-035-block-volume-budget.md).

**Measured by `postgres_exporter`** into VictoriaMetrics, alongside the server's
own series.

## Consequences

Migrations run with `golang-migrate` and are verified after every deploy.
Connection-pool occupancy is published, because a slow query and a query waiting
for a connection look identical until the pool says which it was.
