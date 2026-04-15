---
adr: 014
title: PostgreSQL 17 via pgx/v5 stdlib, colocated Docker container on OCI VPS
status: Accepted
date: 2026-04-14
supersedes: ADR-003
---

# ADR-014: PostgreSQL 17 via pgx/v5 stdlib, Colocated Docker Container on OCI VPS

## Status

Accepted — 2026-04-14. Supersedes [ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver).

## Context

[ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver) locked the
server onto `modernc.org/sqlite` with `MaxOpenConns(1)` because the pure-Go
driver serialises all writes. The tech debt register had flagged this as
Medium severity with a concrete ceiling (~20k concurrent agents) and named
Phase 13 as the fix. Write-path latency was already visible under load, and
the [`Store` interface](../../server/internal/db/store.go) could not grow any
coherent transaction support until a real RDBMS sat underneath.

The original Phase 13 bundle combined PostgreSQL + multi-server routing +
relay pool + Kubernetes. That scope was split: this ADR covers the
**PostgreSQL-only** subset (Phase 13a). Multi-server routing, relay pool, and
Kubernetes are deferred to a future phase and are not touched by this
decision.

Constraints that shaped the options:

- **Single VPS deployment.** Production runs on a single OCI Always-Free ARM64
  instance with 24 GB RAM and tight memory limits on each container. A
  managed database (Neon, Supabase, RDS) would introduce an external
  dependency and monthly cost for a single-operator product.
- **Fresh-start migration.** No historical SQLite data needs to be preserved
  — users re-enrol devices during the announced maintenance window. This
  eliminated the need for a complex SQLite→Postgres data migration tool.
- **Cross-compilation target.** Agents build for `aarch64-unknown-linux-musl`
  (see [ADR-008](../Architecture-Decision-Records.md#adr-008-aarch64-unknown-linux-musl-cross-compilation-via-cross)).
  The driver must not force CGo on the server binary either.
- **Existing monitoring stack.** The VPS already runs VictoriaMetrics +
  Grafana + Loki + Promtail (see [Monitoring-and-Observability](../Monitoring-and-Observability.md)).
  Any new storage layer must slot into the existing scrape pipeline.

## Decision

Replace SQLite with PostgreSQL 17 as the single storage backend for the
server. Four sub-decisions, adopted together:

### 1. Driver: `jackc/pgx/v5/stdlib` via `database/sql`

The [`PostgresStore`](../../server/internal/db/postgres.go) wraps `*sql.DB`
opened through the pgx stdlib adapter. This keeps the `Store` interface shape
identical to what the SQLite implementation had — all existing call sites
compile unchanged, and `appmetrics.NewInstrumentedStore` keeps working. The
native `pgxpool` interface is reserved as a future optimisation if a hot
path needs it.

### 2. Schema: native Postgres types throughout

The [initial migration](../../server/internal/db/migrations/001_initial.up.sql)
uses `TIMESTAMPTZ`, `UUID`, `BOOLEAN`, `JSONB`, and
`BIGINT GENERATED ALWAYS AS IDENTITY` rather than the TEXT/INTEGER shim the
SQLite schema used. Models in [`models.go`](../../server/internal/db/models.go)
hold `time.Time`, `bool`, and `[]string` directly — no more
`nowRFC3339()` helpers or TEXT-stored UUIDs.

### 3. Deployment: colocated `postgres:17-alpine` container

Postgres runs as a Docker Compose service on the same VPS as the server.
See [`deploy/docker-compose.yml`](../../deploy/docker-compose.yml) for the
service definition (memory limit, healthcheck, initdb hook). The server
reaches it via the internal Docker bridge (`postgres:5432`); no host-port
exposure, no UFW or OCI security-list rule. Password auth is SCRAM-SHA-256
from the `POSTGRES_PASSWORD` secret; `sslmode=disable` is acceptable because
traffic never leaves the local Docker bridge.

Backups run via a `prodrigestivill/postgres-backup-local` sidecar. Daily
full dumps with 7-day local retention; off-host replication to OCI Object
Storage is a follow-up, not a blocker.

### 4. Monitoring: `postgres_exporter` into VictoriaMetrics

A `postgres_exporter` container is scraped by the existing VictoriaMetrics
configuration (see [`deploy/docker-compose.monitoring.yml`](../../deploy/docker-compose.monitoring.yml)
and [`deploy/victoriametrics/scrape.yml`](../../deploy/victoriametrics/scrape.yml)).
The provisioned Grafana dashboard surfaces connection count, transaction
rate, and cache-hit ratio. Alerts reuse the existing Telegram pipeline.

## Consequences

**Positive.**

- Single-writer bottleneck gone. Real concurrent writes and a read pool
  (`SetMaxOpenConns(25)`) replace `MaxOpenConns(1)`. The ~20k concurrent
  agent ceiling from [ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver)
  is lifted.
- Native types remove a whole class of conversion bugs (RFC3339 parsing,
  0/1 boolean flags, JSON string fields). Queries read cleanly.
- First real backup infrastructure in the project. Point-in-time restore is
  now feasible; SQLite relied on volume snapshots alone.
- Monitoring dashboards for DB health slot into the existing Grafana stack
  without any new tooling.
- Postgres unlocks serialisable transactions, which clears the path for a
  future `Store`-interface split into per-domain sub-interfaces (deferred;
  see the broad-refactoring plan backlog).

**Negative.**

- New operational surface area. Backups must be monitored (the dedicated
  Grafana alert on backup-age catches silent failures). The Postgres
  container is a new single point of failure; high availability
  (replication, standby) is explicitly out of scope and deferred.
- Memory pressure on the VPS increased by ~640 MB (512 MB postgres +
  64 MB backup sidecar + 64 MB exporter). Still well under the 24 GB
  physical cap, but the margin for future services is tighter.
- Fresh-start cutover discarded existing enrolments, audit events, and
  cached device logs. Mitigated by the single-operator context and the
  announced maintenance window.
- `sslmode=disable` is safe today because traffic is bridge-local, but
  becomes unsafe the moment Postgres is exposed outside the Docker
  network. This is documented in [`docs/Database.md`](../Database.md).

**Mitigated risks.**

- **pgx stdlib adapter corner cases** (e.g. timestamp precision, nullable
  UUIDs) — the shared test suite in
  [`store_test.go`](../../server/internal/db/store_test.go) runs every CRUD
  and concurrency case against a real Postgres service container in CI.
- **Fresh-start surprise** — the cutover was announced and production data
  was preserved as a one-shot rollback artefact (the old `server-data`
  volume). Documented in [`deploy/scripts/rollback.sh`](../../deploy/scripts/rollback.sh).
- **Postgres OOM under load** — 512 MB limit starts conservatively; the
  `postgres_exporter` dashboard exposes memory usage. Bumping the limit is a
  compose-file edit.

## Supersession history

- [ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver)
  (SQLite + WAL via `modernc.org/sqlite`, `MaxOpenConns(1)`) — superseded by
  this ADR.
