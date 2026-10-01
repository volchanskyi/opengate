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

**Measured by `postgres_exporter` in a pod of its own beside each database**
([`postgres-exporter.yaml`](../../deploy/helm/opengate/templates/postgres-exporter.yaml)),
scraped into VictoriaMetrics alongside the server's own series. It reads the
database in its own namespace with the credentials that database is given, and
its readings carry that namespace, so production and staging are each measured
under their own name. A single exporter in the monitoring namespace would watch
one database, label its readings as the monitoring stack's, and need a copy of
the production database password kept outside the deploy. It is not a second
program in the database's pod, because a pod is ready only while every program
in it is: an exporter that crashed, hung or was killed for memory would take the
database out of its Service, and the server could open no new connection until
it came back. The postmaster collector is on, which publishes the database's
start time.

## Consequences

Migrations run with `golang-migrate` and are verified after every deploy.
Connection-pool occupancy is published, because a slow query and a query waiting
for a connection look identical until the pool says which it was.
