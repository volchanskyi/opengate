# Phase 13a — PostgreSQL Migration (Fresh-Start Cutover)

## Context

**Why:** ADR-003 locked the Go server into `modernc.org/sqlite` with `MaxOpenConns(1)` because the pure-Go driver serializes all writes. The tech debt register flags this as Medium severity with a concrete ceiling (~20k concurrent agents) and names Phase 13 as the fix. Write-path latency is already visible under load, and the Store interface cannot grow any coherent transaction support until we have a real RDBMS underneath.

**What changed in scope:** The original Phase 13 bundle (PostgreSQL + cross-server routing + relay pool + Kubernetes) is split. This plan is **Phase 13a: PostgreSQL only**. Multiserver routing, relay pool, and Kubernetes are explicitly **deferred to a future Phase 13b** and are not touched here. Phase 14 already shipped independently, confirming nothing downstream is blocked by deferring the rest.

**Decisions locked in by the owner:**
1. **Scope:** Postgres only (Phase 13a). Multiserver/k8s deferred.
2. **Hosting:** Colocated `postgres:17-alpine` container on the existing OCI Always-Free ARM64 VPS, internal Docker network only.
3. **Data migration:** **Fresh start** — no data migration tool, existing SQLite data is discarded. Users re-enroll devices; audit logs, enrollment tokens, hardware reports, device logs, web push subscriptions, and security-group memberships are wiped. Production cutover happens in an announced maintenance window.
4. **SQLite fate:** Removed immediately after production cutover is verified stable (target: within 24–48h of production going green). No indefinite dual-backend support, no 30-day burn-in.

**Intended outcome:**
- `Store` interface is backed by Postgres 17 via `pgx/v5` (through `database/sql` stdlib adapter).
- Native Postgres types throughout (`TIMESTAMPTZ`, `UUID`, `BOOLEAN`, `JSONB`, `GENERATED ALWAYS AS IDENTITY`).
- Single-writer bottleneck eliminated; real concurrent writes + read pool.
- `pg_dump`-based backup automation running on the VPS (first backup infra in project history).
- `postgres_exporter` wired into the existing VictoriaMetrics + Grafana stack.
- SQLite code, migrations, tests, and Go dependencies fully removed.
- ADR-003 superseded by a new ADR-012 documenting the Postgres choice.

---

## Out of scope (explicitly)

- Cross-server routing / peer discovery (`server/internal/multiserver/` stub stays a stub).
- Relay pool / distributed WebSocket relay.
- Kubernetes migration.
- Store interface splitting into per-domain sub-interfaces (broad-refactoring plan deferred this to Phase 13; it is **still deferred** — this plan deliberately keeps the monolithic `Store` to minimize churn).
- Transaction-heavy refactors beyond what is needed to port existing call sites.
- Schema redesign for multi-server (no `server_id` columns, no partition keys).
- High-availability Postgres (replication, standby, failover).
- Managed-DB migration (Neon, Supabase, RDS) — rejected during brainstorm.

---

## Key design choices (non-negotiable unless noted)

| Area | Choice | Rationale |
|---|---|---|
| Driver | `github.com/jackc/pgx/v5/stdlib` via `database/sql` | Minimal diff vs. existing `*sql.DB` code shape; `metrics.InstrumentedStore` keeps working unchanged; still gets pgx internals. Native `pgxpool` reserved for future hot-path optimization. |
| Migration tool | `golang-migrate/migrate/v4` with `database/pgx/v5` driver | Already in use for SQLite; same `iofs` embedded FS pattern. |
| Schema types | Native PG types — `TIMESTAMPTZ`, `UUID`, `BOOLEAN`, `JSONB`, `BIGINT GENERATED ALWAYS AS IDENTITY` | One-time migration; cheap to do right. Eliminates `nowRFC3339()` helpers and TEXT-stored UUIDs/bools/JSON. |
| Postgres version | `postgres:17-alpine` | Current stable (17.x as of 2026). Alpine keeps footprint small on a 12GB VPS. |
| Placement | Internal docker-compose service, no host-port exposure | Zero attack surface; app reaches DB via Docker DNS `postgres:5432`. |
| Postgres auth | SCRAM-SHA-256 password from `POSTGRES_PASSWORD` secret | `sslmode=disable` for in-network traffic (same host, Docker bridge). |
| Connection pool | `*sql.DB` defaults, then tune: `SetMaxOpenConns(25)`, `SetMaxIdleConns(5)`, `SetConnMaxLifetime(30*time.Minute)` | Conservative starting point; revisit after prod metrics. |
| Backups | `pg_dump` cron inside a sidecar container, written to `/opt/opengate/backups/` on the host | Daily full, 7-day local retention. Optional rclone → OCI Object Storage (20GB free tier) in a follow-up ticket (not blocking Phase 13a). |
| Monitoring | `postgres_exporter` added to `docker-compose.monitoring.yml`, scraped by VictoriaMetrics | Reuses existing Grafana + alerting pipeline. |
| Test strategy | Temporarily dual-backend test suite via factory function; CI runs both; SQLite path deleted in PR-6 | Lets us validate PostgresStore against the existing 1300+ lines of sqlite_test.go before deleting anything. |

---

## Execution plan — 6 PRs on `dev`

Each PR is independently mergeable, keeps `dev` green, and respects `/precommit` gates.

### PR-1 — Skeleton: PostgresStore stub + CI plumbing + env var wiring

**Goal:** Add Postgres driver deps, empty `PostgresStore`, postgres migration tree, CI Postgres service container. SQLite remains the default; nothing in prod changes.

**Files created:**
- `server/internal/db/postgres.go` — `PostgresStore` struct wrapping `*sql.DB` opened via `pgx/v5/stdlib`, `NewPostgresStore(ctx, databaseURL)` constructor, `runPostgresMigrations(db *sql.DB)`. Method bodies return `errors.ErrUnsupported` or panic until PR-2.
- `server/internal/db/migrations/postgres/001_initial.up.sql` + `.down.sql` — full fresh schema (see **Schema translation notes** below).
- `server/internal/db/postgres_test.go` — table stub, real tests land in PR-3.

**Files modified:**
- `server/go.mod` / `server/go.sum` — add `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4/database/pgx/v5`.
- `server/cmd/meshserver/main.go:65` — add `-database-url` flag (also reads `DATABASE_URL` env). If set, construct `PostgresStore`; otherwise fall back to `NewSQLiteStore`. Interface is identical so the rest of wiring (`appmetrics.NewInstrumentedStore`) is unchanged.
- `server/internal/db/sqlite.go` — extract existing `runMigrations` into `runSQLiteMigrations` for symmetry with `runPostgresMigrations`.
- `.github/workflows/ci.yml` — add `postgres:17-alpine` service container to the Go test job with `POSTGRES_USER=opengate`, `POSTGRES_PASSWORD=opengate`, `POSTGRES_DB=opengate_test`, health check on 5432; export `POSTGRES_TEST_URL=postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable` to the `go test` step.

**Verification:**
- `go build ./...`, `go vet ./...` green.
- CI `go test ./...` still green (Postgres tests skipped by build tag until PR-2).
- Starting server without `DATABASE_URL` still boots SQLite (backward compatible).
- Starting server with `DATABASE_URL` set to a local Postgres runs migrations successfully but every method returns the stub error.

### PR-2 — Port every `Store` method + native-type models

**Goal:** Implement all ~50 methods of the `Store` interface on `PostgresStore`, and migrate the model structs (`server/internal/db/models.go`) to native Go types where the schema changes dictate.

**Method porting rules** (applied mechanically to each of the ~50 methods):

| SQLite construct | Postgres equivalent |
|---|---|
| `?` placeholder (61+ sites) | `$1, $2, ...` — pgx requires positional |
| `datetime('now')` (15+ sites) | `NOW()` (or `DEFAULT CURRENT_TIMESTAMP` in schema, set in-app elsewhere) |
| `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')` (5 sites in `005_security_groups.up.sql`) | `NOW()` |
| `INSERT OR IGNORE` (2 sites: `AddSecurityGroupMember`, `005_security_groups` seed) | `INSERT ... ON CONFLICT DO NOTHING` |
| `ON CONFLICT (x) DO UPDATE SET ...` (`UpsertDevice`, `UpsertWebPushSubscription`, `UpsertDeviceHardware`) | Same syntax — works in both |
| `INTEGER PRIMARY KEY AUTOINCREMENT` (`audit_events`, `device_updates`, `device_logs`) | `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY` |
| `TEXT` PK holding a UUID (most tables) | `UUID NOT NULL` |
| `TEXT` timestamps (RFC3339 strings) | `TIMESTAMPTZ` |
| `INTEGER` (0/1) booleans (`is_admin`, `is_system`) | `BOOLEAN` |
| `TEXT` holding JSON (`devices.capabilities`, `device_hardware.network_interfaces`) | `JSONB` |
| `PRAGMA foreign_keys = ON` | not needed — PG always enforces FKs |
| `BEGIN IMMEDIATE` / SQLite busy-timeout retry | standard `BeginTx` + `SERIALIZABLE` where needed (only `UpsertDeviceLogs` uses a tx today) |

**Model changes in `server/internal/db/models.go`:**
- `CreatedAt string` / `UpdatedAt string` / all other RFC3339 TEXT fields → `time.Time`.
- `IsAdmin int` / `IsSystem int` and similar 0/1 fields → `bool`.
- Keep `uuid.UUID` for AMT device IDs; add native UUID scanning for the rest (currently stored as TEXT then parsed).
- `DeviceCapabilities` field: currently `string` (raw JSON). Change to `[]string` and let pgx's JSONB scanner handle it.
- `DeviceHardware.NetworkInterfaces`: same — change from `string` to `[]NetworkInterface` struct.

**Update `SQLiteStore` to bridge:** while SQLite still exists (PRs 2–5), `SQLiteStore` methods must convert between the new native-type models and the TEXT-stored values. This keeps the dual-backend period honest:
- Read: parse `TEXT` timestamps with `time.Parse(time.RFC3339, ...)`.
- Write: format `time.Time` with `.UTC().Format(time.RFC3339)`.
- Booleans: convert 0/1 ↔ `bool`.
- JSON columns: `json.Unmarshal` / `json.Marshal`.
- This conversion layer is throwaway — deleted in PR-6.

**Consumers to re-check** (any place that passed timestamp strings directly):
- `server/internal/api/` — JSON response encoders for timestamps (should already emit RFC3339 regardless).
- `server/internal/notifications/` — audit event formatting.
- `server/internal/agentapi/` — device hardware report ingestion.
- `server/internal/mps/` — AMT device status writes.
- Any caller that does `t := time.Now().UTC().Format(time.RFC3339)` before calling the store loses that call.

**Verification:**
- `go test ./internal/db/...` green against both backends (via CI service container).
- `go test -race ./internal/db/...` green.
- Benchmark suite (`bench_test.go`) runs against both.

### PR-3 — Dual-backend test suite + full test migration

**Goal:** Refactor `sqlite_test.go` (1346 lines) into a backend-agnostic suite that runs identical assertions against both drivers.

**Files changed:**
- Rename `server/internal/db/sqlite_test.go` → `server/internal/db/store_test.go`.
- Introduce `server/internal/db/storetest/storetest.go` (new internal test helper package) with:
  - `type Factory func(t *testing.T) (db.Store, func())` — returns a fresh store + cleanup.
  - `SQLiteFactory(t)` using `t.TempDir()` as today.
  - `PostgresFactory(t)` reading `POSTGRES_TEST_URL` from env, creating a fresh schema per test (via `CREATE SCHEMA test_<random>` + `SET search_path`), dropped in cleanup.
  - Skip Postgres factory with `t.Skip("POSTGRES_TEST_URL not set")` when env is unset (local dev ergonomics).
- Every test becomes: `for _, f := range []storetest.Factory{storetest.SQLiteFactory, storetest.PostgresFactory} { t.Run(f.Name, ...) }`.
- `bench_test.go` same pattern but only runs when explicitly invoked.

**Also:** Update integration tests in other packages that instantiate a store directly:
- `server/internal/api/*_test.go` — check for `db.NewSQLiteStore` calls; swap to the factory helper.
- `server/internal/agentapi/*_test.go` — same.
- `server/internal/notifications/*_test.go` — same.
- Keep SQLite as the default local-dev path for speed; Postgres is CI-only unless env is set.

**Verification:**
- `make test` green.
- `make test-race` green.
- SonarCloud coverage does not regress (target: same or higher).
- `/precommit` passes.

### PR-4 — Deploy infrastructure: Postgres service, backups, monitoring

**Goal:** Production-grade Postgres colocated with the app, backups in place, monitoring visible.

**Files created:**
- `deploy/postgres/init.sql` — initial `opengate` role + `opengate` database setup (idempotent, runs via postgres image `docker-entrypoint-initdb.d` hook).
- `deploy/postgres-backup/Dockerfile` or use `prodrigestivill/postgres-backup-local` image directly in compose (preferred — one less custom image).
- `deploy/grafana/provisioning/dashboards/postgres.json` — import of Grafana dashboard ID 9628 (`postgres-exporter`).

**Files modified:**
- `deploy/docker-compose.yml` — add `postgres` service:
  ```yaml
  postgres:
    image: postgres:17-alpine
    container_name: opengate-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: opengate
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}
      POSTGRES_DB: opengate
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./postgres/init.sql:/docker-entrypoint-initdb.d/init.sql:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U opengate -d opengate"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    deploy:
      resources:
        limits: { memory: 512M, cpus: '1.0' }
        reservations: { memory: 128M }
  ```
  And add `postgres-backup` sidecar (`prodrigestivill/postgres-backup-local`) writing to a new `postgres-backups` volume, schedule `@daily`, retention `7`.
  Update `server` service: add `DATABASE_URL=postgres://opengate:${POSTGRES_PASSWORD}@postgres:5432/opengate?sslmode=disable` env, add `depends_on: { postgres: { condition: service_healthy } }`. Keep `server-data:/data` volume — still needed for TLS certs and VAPID keys.
- `deploy/docker-compose.staging.yml` — mirror with `opengate-postgres-staging` container name and separate `postgres-staging-data` volume; staging has its own password via `STAGING_POSTGRES_PASSWORD`.
- `deploy/docker-compose.monitoring.yml` — add `postgres_exporter` service (`prometheuscommunity/postgres-exporter:latest`) reading `DATA_SOURCE_NAME` from env, scraped internally only.
- `deploy/victoriametrics/scrape.yml` — add `postgres-exporter:9187` scrape target.
- `deploy/.env.example` — document `POSTGRES_PASSWORD`, `STAGING_POSTGRES_PASSWORD`, `DATABASE_URL` (optional override).
- `deploy/scripts/deploy.sh` — write `POSTGRES_PASSWORD` to `.env` and `.env.staging` during the deploy step (via the existing `set_env_var()` helper in `common.sh`).
- `deploy/tests/validate-configs.sh` — add `POSTGRES_PASSWORD` to the env-var coverage assertions.
- `.github/workflows/cd.yml` — inject `DEPLOY_POSTGRES_PASSWORD` / `DEPLOY_STAGING_POSTGRES_PASSWORD` from GitHub environments into the deploy step.

**New GitHub environment secrets required** (added in repo settings before this PR is merged):
- `production` environment: `DEPLOY_POSTGRES_PASSWORD`
- `staging` environment: `DEPLOY_STAGING_POSTGRES_PASSWORD`

**Resource accounting:** postgres 512M + postgres-backup 64M + postgres_exporter 64M = +640M on top of the existing ~2.3GB hard limits. Still well under the 12GB physical cap.

**Firewall / network:** no UFW or OCI security list changes — Postgres is strictly internal to the Docker bridge network.

**Verification:**
- `docker compose config` validates.
- `docker compose up -d postgres` locally brings up a healthy Postgres.
- `config-lint` CI job passes (includes `deploy/tests/validate-configs.sh`).
- Monitoring stack shows postgres_exporter target as UP in VictoriaMetrics.

### PR-5 — Staging cutover + production cutover

**Goal:** Flip both environments from SQLite to Postgres. SQLite code remains in the binary as a last-resort rollback path until PR-6.

**Steps:**
1. **Staging flip:** Merge to `dev`, CD pipeline builds image, `deploy-staging` job runs. Staging's existing "reset DB" step (removes `/data/opengate.db`) now becomes redundant for data — but the Postgres volume is fresh on first deploy anyway, so staging comes up empty by design (matches the fresh-start decision).
2. Playwright E2E runs against staging on Postgres. Investigate any regressions.
3. **Observation window: 24–48 hours** on staging with synthetic traffic + manual smoke tests covering:
   - User login + JWT refresh
   - Device enrollment via token
   - Agent QUIC connect + heartbeat
   - Session relay (web ↔ agent)
   - Audit log write + query
   - Device logs request + cache TTL
   - Hardware inventory request
   - Web push subscribe + notify
4. **Announce production maintenance window** (Telegram alerting channel + any relevant stakeholders). Schedule for low-traffic time.
5. **Production flip:** manual approval on `deploy-production` job triggers CD deploy. Because data is reset (fresh-start), the previous production `server-data` SQLite volume is left untouched (not deleted) — serves as last-resort rollback artifact until PR-6.
6. Post-deploy: run smoke-test.sh, monitor Grafana dashboards for error rate and DB query latency, manually re-enroll a test device, verify end-to-end.
7. **Rollback path (only if needed):** revert `IMAGE_TAG` to previous sha (same process as any CD rollback), revert `DATABASE_URL` env var to unset in `.env`. Server boots against the preserved SQLite volume. Documented in `deploy/scripts/rollback.sh` comments as part of this PR.

**Files modified in this PR:**
- `deploy/scripts/deploy.sh` — remove the staging SQLite-reset line (`rm /data/opengate.db`) since it's a no-op once Postgres is the backend.
- `.github/workflows/cd.yml` — remove any SQLite-specific deploy steps; Playwright E2E still runs unchanged.
- `.claude/phases.md` — mark Phase 13a as in-progress.

**Verification:**
- Staging Playwright E2E green on Postgres.
- 24–48h stability window on staging.
- Production post-deploy smoke tests green.
- Grafana DB-query latency histograms show healthy p50/p90/p99.
- Prometheus alerts silent (no DB errors, no connection exhaustion, no disk pressure).

### PR-6 — Remove SQLite (final cleanup)

**Goal:** Single source of truth. SQLite deleted everywhere.

**Files deleted:**
- `server/internal/db/sqlite.go`
- `server/internal/db/migrations/001_initial.up.sql` through `011_normalize_os.down.sql` (all 22 SQLite migration files)
- `server/internal/db/storetest/storetest.go` SQLite factory (keep only PostgresFactory)
- Any SQLite-specific test helpers in other packages

**Files moved:**
- `server/internal/db/migrations/postgres/*.sql` → `server/internal/db/migrations/*.sql` (flatten — Postgres is the only target).

**Files modified:**
- `server/go.mod` / `go.sum` — drop `modernc.org/sqlite`, `github.com/golang-migrate/migrate/v4/database/sqlite`.
- `server/internal/db/postgres.go` — rename `NewPostgresStore` → `NewStore`, rename `PostgresStore` → `Store` (type) or keep and expose via constructor alias. Note: conflict with `Store` interface name; use `PgStore` or leave as `PostgresStore`. Decision: **keep `PostgresStore` as the concrete type name** to keep the diff minimal.
- `server/internal/db/models.go` — remove any comments mentioning SQLite.
- `server/cmd/meshserver/main.go` — remove the SQLite fallback branch; `DATABASE_URL` is now required. Boot fails fast if missing.
- `.github/workflows/ci.yml` — remove SQLite matrix entry; Postgres service container is the only DB.
- `deploy/docker-compose.yml` — remove the `server-data:/data` volume for the DB (still needed for certs + VAPID — keep, but add comment noting DB is no longer stored there).
- `deploy/scripts/deploy.sh`, `deploy/scripts/rollback.sh` — remove SQLite-volume-preservation logic added in PR-5.
- `.claude/phases.md` — mark Phase 13a complete, add row with version bump (target: `v0.18.0`).
- `.claude/techdebt.md` — remove the `SQLite Single-Connection Constraint` row (resolved).
- `.claude/decisions.md` — mark ADR-003 as **Superseded** by new ADR-012; add ADR-012 row: `012 | PostgreSQL 17 via pgx/v5 stdlib, colocated Docker container on OCI VPS | 13a | Accepted`.
- Wiki updates (push to `/home/ivan/opengate.wiki/master`):
  - `Database.md` — full rewrite: Postgres schema, native types, pg_dump backup, monitoring dashboard references.
  - `Architecture-Decision-Records.md` — add ADR-012 full text, mark ADR-003 superseded.
  - `Architecture.md` — update the "Storage" section.
  - `CI-Pipeline.md` — update the test job description.
  - `Infrastructure.md` — add the Postgres service to the deployment topology diagram.
- README.md — update the "Quick Start" section to mention `DATABASE_URL` / `POSTGRES_PASSWORD` env vars.

**Verification before merge:**
- `go build ./...` green with SQLite removed.
- `go test ./...` green against Postgres only.
- `make lint`, `make golden`, `make e2e` all green.
- `/precommit` green.
- SonarCloud coverage ≥ pre-migration baseline.
- Production continues to run cleanly after this PR's deploy (should be a no-op behavior-wise).

---

## Schema translation notes (for PR-1 `001_initial.up.sql`)

Canonical table list after translation (based on current 11 SQLite migrations flattened):

| Table | Key PK/FK changes | Type notes |
|---|---|---|
| `users` | `id UUID PK` | `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`; `is_admin BOOLEAN` |
| `groups_` (keep trailing underscore to avoid Postgres reserved-word collision on `GROUP`) | `id UUID PK`, `owner_id UUID FK → users(id)` | timestamps TIMESTAMPTZ |
| `devices` | `id UUID PK`, `group_id UUID FK → groups_(id) ON DELETE SET NULL NULL` | `capabilities JSONB NOT NULL DEFAULT '[]'::jsonb`; `last_seen TIMESTAMPTZ` |
| `agent_sessions` | `token TEXT PK`, `device_id UUID FK → devices(id) ON DELETE CASCADE` | timestamps TIMESTAMPTZ |
| `web_push_subscriptions` | `endpoint TEXT PK`, `user_id UUID FK` | timestamps TIMESTAMPTZ |
| `audit_events` | `id BIGINT GENERATED ALWAYS AS IDENTITY PK` | `created_at TIMESTAMPTZ` + index |
| `amt_devices` | `uuid UUID PK` | — |
| `enrollment_tokens` | `id UUID PK`, `token TEXT UNIQUE` | `expires_at TIMESTAMPTZ` |
| `security_groups` | `id UUID PK` | `is_system BOOLEAN` |
| `security_group_members` | `(group_id, user_id)` composite PK | `added_at TIMESTAMPTZ` |
| `device_updates` | `id BIGINT GENERATED ALWAYS AS IDENTITY PK`, `device_id UUID FK` | `acked_at TIMESTAMPTZ NULL` |
| `device_hardware` | `device_id UUID PK / FK → devices(id) ON DELETE CASCADE` | `network_interfaces JSONB NOT NULL`; `updated_at TIMESTAMPTZ` |
| `device_logs` | `id BIGINT GENERATED ALWAYS AS IDENTITY PK`, `device_id UUID FK ON DELETE CASCADE` | `timestamp TIMESTAMPTZ`, message `TEXT` |

**Indexes (mirror existing):** All 12 indexes from the SQLite migrations carry over with identical column definitions. One new index to consider: `audit_events (created_at DESC)` explicitly for the `QueryAuditLog` pagination.

**Seeding:** The `005_security_groups` Administrators-group seed is carried into `001_initial.up.sql` as a single `INSERT ... ON CONFLICT DO NOTHING`.

---

## Critical files reference (consolidated)

**Source code:**
- `server/internal/db/store.go` — interface (unchanged)
- `server/internal/db/sqlite.go` — modified in PR-1 (extract migration runner), deleted in PR-6
- `server/internal/db/postgres.go` — new in PR-1, implemented in PR-2
- `server/internal/db/models.go` — native-type rewrite in PR-2
- `server/internal/db/migrations/` — SQLite until PR-6, then Postgres-only
- `server/internal/db/store_test.go` — factory-based dual-backend in PR-3, SQLite path removed in PR-6
- `server/internal/db/storetest/storetest.go` — new helper package in PR-3
- `server/cmd/meshserver/main.go:65` — driver selection + `DATABASE_URL` flag
- `server/go.mod` / `go.sum` — pgx deps added PR-1, modernc.org/sqlite removed PR-6
- `server/internal/metrics/store.go` — **no changes** (InstrumentedStore wraps the Store interface, driver-agnostic)

**Deploy / infra:**
- `deploy/docker-compose.yml` — `postgres` service + `postgres-backup` sidecar (PR-4)
- `deploy/docker-compose.staging.yml` — staging mirror
- `deploy/docker-compose.monitoring.yml` — `postgres_exporter` (PR-4)
- `deploy/victoriametrics/scrape.yml` — new scrape target
- `deploy/grafana/provisioning/dashboards/postgres.json` — new dashboard
- `deploy/postgres/init.sql` — new init script
- `deploy/.env.example` — document new vars
- `deploy/scripts/deploy.sh`, `common.sh` — wire new env vars
- `deploy/tests/validate-configs.sh` — env-var coverage

**CI / CD:**
- `.github/workflows/ci.yml` — Postgres service container (PR-1)
- `.github/workflows/cd.yml` — inject new secrets (PR-4)

**Docs:**
- `.claude/phases.md`, `.claude/techdebt.md`, `.claude/decisions.md`
- `/home/ivan/opengate.wiki/Database.md`, `Architecture.md`, `Architecture-Decision-Records.md`, `Infrastructure.md`, `CI-Pipeline.md`

**Reused existing utilities (do not reimplement):**
- `appmetrics.NewInstrumentedStore` (`server/internal/metrics/store.go`) — DB metrics wrapper, works on `Store` interface.
- `golang-migrate/migrate/v4` + `source/iofs` — already wired; swap DB driver only.
- `deploy/scripts/common.sh` — `set_env_var()`, `redeploy()`, `wait_healthy()`.
- `deploy/tests/validate-configs.sh` — env-var coverage assertions.
- `deploy/scripts/rollback.sh` — existing image-tag revert mechanism; reused for PR-5 rollback path.

---

## Verification (end-to-end)

**Unit / integration tests:**
```bash
# Start local Postgres for dev
docker run --rm -d --name pg-dev -e POSTGRES_PASSWORD=dev -e POSTGRES_USER=opengate -e POSTGRES_DB=opengate_test -p 5432:5432 postgres:17-alpine
export POSTGRES_TEST_URL='postgres://opengate:dev@localhost:5432/opengate_test?sslmode=disable'

cd server
go test -race ./internal/db/...          # dual-backend until PR-6, Postgres only after
go test -race ./...                        # full suite
```

**CI gates (must pass on every PR):**
```bash
make lint         # clippy + go vet + eslint + actionlint
make test         # all tests (Postgres service container in CI)
make golden       # cross-language compat
make e2e          # Playwright
/precommit        # mandatory pre-commit skill
```

**Local full-stack test (PR-4+):**
```bash
cd deploy
POSTGRES_PASSWORD=localdev JWT_SECRET=$(openssl rand -hex 32) docker compose up -d
# Verify all services healthy
docker compose ps
# Hit the API
curl -s http://localhost:8080/api/v1/health | jq
# Verify postgres volume has data
docker exec opengate-postgres psql -U opengate -d opengate -c '\dt'
```

**Staging gates (PR-5):**
- Playwright E2E green against staging Postgres.
- `deploy/scripts/smoke-test.sh --host localhost --port 18080` green.
- Grafana `Postgres Exporter` dashboard shows the service up, no errors.
- 24–48h observation without alert pages.

**Production gates (PR-5 manual approval):**
- Staging gates passed.
- Maintenance window announced.
- Backup of pre-migration SQLite volume captured (`docker run --rm -v opengate_server-data:/from -v $(pwd):/to alpine tar czf /to/pre-phase13a-sqlite-backup.tar.gz -C /from .`).
- Post-deploy smoke tests green.
- 24h production observation clean before triggering PR-6.

**Final gates (PR-6):**
- SonarCloud coverage ≥ pre-migration baseline.
- `go mod tidy` clean (no dangling SQLite deps).
- `grep -r "modernc.org/sqlite" .` returns nothing.
- `grep -ri "sqlite" server/` returns only historical comments / ADRs.
- Wiki updated and pushed.
- `phases.md`, `techdebt.md`, `decisions.md` updated.

---

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `pgx` stdlib adapter mishandles a corner case (timestamp precision, nullable UUIDs) | Medium | Medium | Dual-backend test suite in PR-3 catches divergence before PR-5; benchmark + race tests. |
| Postgres container OOMs on 512M limit under load | Low | Medium | Start with 512M; monitor via postgres_exporter; bump to 1GB if needed (plenty of headroom). |
| Backup sidecar silently fails, production data loss after VPS host incident | Medium | High | Backup monitoring: Grafana alert on `postgres_backup_last_success_timestamp` stale > 26h. Manual verification during PR-4 bring-up. Document restore procedure in `deploy/postgres/RESTORE.md`. |
| Fresh-start cutover surprises users who still had devices enrolled | Low | Low (single-owner product, user is aware) | Announced maintenance window; communicated via decision 3. |
| SonarCloud coverage drops temporarily during PR-2/PR-3 refactor | Medium | Low | Accepted — PR-3 brings it back. Quality gate threshold may need a temporary bump. |
| CI Postgres service container flakes (slow start) | Low | Low | Use `pg_isready` wait loop before tests; `services.postgres.options` with health check. |
| Rollback window between PR-5 and PR-6 leaves prod running on stale SQLite snapshot if Postgres fails | Low | Medium | Keep preserved SQLite volume untouched until PR-6; rollback script documented. |
| pgx requires explicit `time.Time` UTC handling and DB returns `+00` offsets | High | Low | `TIMESTAMPTZ` stores UTC; scan into `time.Time` and always call `.UTC()` before serialization (same pattern as today). |

---

## Follow-ups (explicitly out of scope but tracked)

- **Phase 13b** — multiserver peer discovery + relay pool (revisit after production load data justifies it).
- **Store interface splitting** — the broad-refactoring plan's deferred item. Now unblocked by Phase 13a completion; open a separate plan.
- **Transaction audit** — now that Postgres is underneath, review all multi-step operations and convert to transactions where atomicity matters. Currently only `UpsertDeviceLogs` uses a tx.
- **rclone → OCI Object Storage** — off-host backup replication. Follow-up ticket, not blocking.
- **pgvector / extensions** — if AI/embedding features land, consider `postgres:17-alpine` + `pgvector` image.
- **Connection-pool tuning** — revisit `SetMaxOpenConns` etc. after 2 weeks of prod metrics.
