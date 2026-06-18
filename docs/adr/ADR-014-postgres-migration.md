---
adr: 014
title: PostgreSQL 17 via pgx/v5 stdlib, deployed as the app Helm chart StatefulSet
status: Accepted
date: 2026-04-14
supersedes: ADR-003
---

# ADR-014: PostgreSQL 17 via pgx/v5 stdlib, Deployed as the App Helm Chart StatefulSet

## Status

Accepted — 2026-04-14. Supersedes [ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver).

Updated 2026-06-18 for the current OKE deployment substrate. The database
choice in this ADR is unchanged; the later move from Compose/VPS to OKE/Helm is
covered by [ADR-030](ADR-030-kubernetes-adoption-oke-helm.md),
[ADR-034](ADR-034-scale-out-keda-shared-keys.md), and
[ADR-035](ADR-035-oke-free-tier-block-volume-remediation.md).

## Context

[ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver)
locked the server onto `modernc.org/sqlite` with `MaxOpenConns(1)` because the
pure-Go driver serialised all writes. The tech debt register had flagged this as
Medium severity with a concrete ceiling (~20k concurrent agents) and named the
database migration as the fix. Write-path latency was already visible under
load, and the [`Store` interface](../../server/internal/db/store.go) could not
grow coherent transaction support until a real RDBMS sat underneath.

The original scaling bundle combined PostgreSQL + multi-server routing + relay
pool + Kubernetes. That scope was split: this ADR covers the PostgreSQL storage
decision. The Kubernetes rollout happened later and now determines the runtime
shape.

Constraints that shaped the options:

- **Cost ceiling.** No managed database dependency or monthly database bill for
  the current single-operator / small-team phase.
- **Fresh-start migration.** No historical SQLite data needed to be preserved;
  users re-enrolled devices during the announced maintenance window.
- **Cross-compilation target.** Agents build for
  `aarch64-unknown-linux-musl`; the server database driver must not require CGo.
- **Observable storage layer.** Database health must feed the same metrics and
  dashboard stack as the rest of the service.

## Decision

Replace SQLite with PostgreSQL 17 as the single storage backend for the server.
Four sub-decisions are adopted together.

### 1. Driver: `jackc/pgx/v5/stdlib` via `database/sql`

The [`PostgresStore`](../../server/internal/db/postgres.go) wraps `*sql.DB`
opened through the pgx stdlib adapter. This keeps the `Store` interface shape
compatible with existing call sites, and `appmetrics.NewInstrumentedStore` keeps
working. The native `pgxpool` interface is reserved as a future optimisation if
a hot path needs it.

### 2. Schema: native Postgres types throughout

The [initial migration](../../server/internal/db/migrations/001_initial.up.sql)
uses `TIMESTAMPTZ`, `UUID`, `BOOLEAN`, `JSONB`, and
`BIGINT GENERATED ALWAYS AS IDENTITY` rather than the TEXT/INTEGER shim the
SQLite schema used. Models in [`models.go`](../../server/internal/db/models.go)
hold `time.Time`, `bool`, and `[]string` directly — no more `nowRFC3339()`
helpers or TEXT-stored UUIDs.

### 3. Deployment: app Helm chart Postgres StatefulSet

Postgres runs inside the app Helm release as a Kubernetes
[`StatefulSet`](../../deploy/helm/opengate/templates/postgres-statefulset.yaml)
using `docker.io/library/postgres:17-alpine`. The server reaches it through the
chart's headless Service DNS name, generated in
[`server-deployment.yaml`](../../deploy/helm/opengate/templates/server-deployment.yaml)
as `postgres://opengate:$(POSTGRES_PASSWORD)@<release>-postgres:5432/opengate?sslmode=disable`.

Production uses a persistent `oci-bv` volume claim. Staging sets
`postgres.storage.persistent=false` in
[`values-staging.yaml`](../../deploy/helm/opengate/values-staging.yaml) and rides
`emptyDir` because staging data is disposable E2E/smoke state. This storage
split is part of the OCI free-tier block-volume remediation in ADR-035.

Backups run via the app chart's
[`postgres-backup` CronJob](../../deploy/helm/opengate/templates/postgres-backup-cronjob.yaml):
an init container runs `pg_dump`, a `curl` container streams the gzip to OCI
Object Storage through a write-only PAR URL, and retention is enforced by an OCI
Object Storage lifecycle policy. No in-cluster backup PVC is used.

### 4. Monitoring: `postgres_exporter` into VictoriaMetrics

The monitoring Helm chart deploys
[`postgres_exporter`](../../deploy/helm/monitoring/templates/postgres-exporter.yaml)
against the production Postgres Service configured by
[`deploy/helm/monitoring/values.yaml`](../../deploy/helm/monitoring/values.yaml).
VictoriaMetrics scrapes it, and the provisioned
[`postgres` Grafana dashboard](../../deploy/grafana/provisioning/dashboards/postgres.json)
surfaces connection count, transaction rate, and cache-hit ratio.

## Consequences

**Positive.**

- Single-writer bottleneck gone. Real concurrent writes and a read pool
  (`SetMaxOpenConns(25)`) replace `MaxOpenConns(1)`.
- Native types remove a class of conversion bugs (RFC3339 parsing, 0/1 boolean
  flags, JSON string fields).
- Database deployment is versioned with the app Helm chart and validated by the
  same Kubernetes/Helm lint path as the server.
- Off-cluster backups survive cluster loss without consuming another OCI block
  volume.
- Postgres unlocks serialisable transactions, clearing the path for a future
  `Store`-interface split into per-domain sub-interfaces.

**Negative.**

- New operational surface area. StatefulSet rollout, PVC health, backups, and
  exporter health must all be monitored.
- Production still has a single Postgres primary. High availability
  (replication, standby, managed Postgres) is explicitly out of scope until the
  multi-server phase needs it.
- Fresh-start cutover discarded existing enrolments, audit events, and cached
  device logs. Mitigated by the single-operator context and announced
  maintenance window.
- `sslmode=disable` is acceptable only while server ↔ Postgres traffic stays
  inside the Kubernetes cluster. If Postgres moves outside the cluster boundary,
  switch to `sslmode=verify-full`; see [`docs/Database.md`](../Database.md).

**Mitigated risks.**

- **pgx stdlib adapter corner cases** — the shared test suite in
  [`store_test.go`](../../server/internal/db/store_test.go) runs CRUD and
  concurrency cases against real Postgres in CI.
- **Backup regression** — the production chart owns a daily CronJob and the
  runbook documents the bucket/PAR/lifecycle setup in
  [`NOTES.txt`](../../deploy/helm/opengate/templates/NOTES.txt).
- **Postgres OOM under load** — conservative resource limits live in
  [`values.yaml`](../../deploy/helm/opengate/values.yaml), and exporter metrics
  surface memory and connection pressure.

## Supersession History

- [ADR-003](../Architecture-Decision-Records.md#adr-003-sqlite--wal-mode-via-pure-go-driver)
  (SQLite + WAL via `modernc.org/sqlite`, `MaxOpenConns(1)`) — superseded by
  this ADR.
