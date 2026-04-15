# Database

OpenGate uses PostgreSQL 17 as its single storage backend behind the
[`db.Store` interface](../server/internal/db/store.go). The server requires
the `DATABASE_URL` env var (or `-database-url` flag) at startup and exits
fast if it is unset. See [ADR-014](adr/ADR-014-postgres-migration.md) for
the rationale behind the PostgreSQL choice and the supersession of ADR-003.

## Driver & connection pool

| Setting | Value | Source |
|---------|-------|--------|
| Driver | `github.com/jackc/pgx/v5/stdlib` | [`postgres.go`](../server/internal/db/postgres.go) |
| Pool impl | `database/sql` adapter over pgx | [`postgres.go`](../server/internal/db/postgres.go) |
| Migrations | `golang-migrate` with `database/pgx/v5` source | [`postgres.go`](../server/internal/db/postgres.go), [`migrations/`](../server/internal/db/migrations/) |
| Max open conns / idle / lifetime | Set inside `NewPostgresStore` | [`postgres.go`](../server/internal/db/postgres.go) |
| Size metric | `pg_database_size(current_database())` | [`postgres.go`](../server/internal/db/postgres.go) `Size()` |

The size value feeds the `opengate_db_size_bytes` Prometheus gauge (see
[Monitoring-and-Observability](Monitoring-and-Observability.md)).

## Schema types

Native Postgres types throughout — no TEXT/INTEGER shims.

| Column kind | Type |
|-------------|------|
| Primary keys (generated) | `BIGINT GENERATED ALWAYS AS IDENTITY` |
| Entity IDs (assigned by app) | `UUID` |
| Timestamps | `TIMESTAMPTZ`, `NOT NULL DEFAULT NOW()` where applicable |
| Booleans | `BOOLEAN` |
| JSON columns | `JSONB` |
| Upsert semantics | `ON CONFLICT ... DO UPDATE` / `DO NOTHING` |

## Schema

Thirteen tables managed by `golang-migrate`. The current schema is a single
flat migration ([`001_initial.up.sql`](../server/internal/db/migrations/001_initial.up.sql))
produced by Phase 13a's fresh-start cutover from SQLite.

```
┌─────────────────────┐       ┌─────────────────────┐
│       users         │       │      groups_         │
│─────────────────────│       │─────────────────────│
│ id            PK    │◄──┐   │ id            PK    │
│ email         UQ    │   │   │ name                │
│ password_hash       │   ├───│ owner_id      FK    │
│ display_name        │   │   │ created_at          │
│ is_admin            │   │   │ updated_at          │
│ created_at          │   │   └──────────┬──────────┘
│ updated_at          │   │              │ 1:N (SET NULL)
└─────────────────────┘   │   ┌──────────▼──────────┐
                          │   │      devices         │
┌─────────────────────┐   │   │─────────────────────│
│  agent_sessions     │   │   │ id            PK    │
│─────────────────────│   │   │ group_id FK (nullable)│
│ token         PK    │   │   │ hostname            │
│ device_id     FK    │───┤   │ os                  │
│ user_id       FK    │───┤   │ capabilities (JSONB)│
│ created_at          │   │   │ status              │
└─────────────────────┘   │   │ last_seen           │
                          │   │ created_at          │
                          │   │ agent_version       │
                          │   │ updated_at          │
┌─────────────────────┐   │   └─────────────────────┘
│ web_push_subscriptions│  │
│─────────────────────│   │   ┌─────────────────────┐
│ endpoint      PK    │   │   │    audit_events      │
│ user_id       FK    │───┘   │─────────────────────│
│ p256dh              │       │ id        PK (identity)│
│ auth                │       │ user_id              │
└─────────────────────┘       │ action               │
                              │ target               │
                              │ details              │
                              │ created_at           │
                              └─────────────────────┘
```

```
┌─────────────────────────┐       ┌─────────────────────────────┐
│   security_groups       │       │ security_group_members      │
│─────────────────────────│       │─────────────────────────────│
│ id            PK        │◄──────│ group_id      FK (CASCADE)  │
│ name          UQ        │       │ user_id       FK (CASCADE)  │
│ description             │       │ added_at                    │
│ is_system               │       │ PK(group_id, user_id)       │
│ created_at              │       └─────────────────────────────┘
│ updated_at              │
└─────────────────────────┘
```

Note: the groups table is named `groups_` (trailing underscore) to avoid
collision with the Postgres `GROUP` reserved word. All column lists,
indexes, and the Administrators seed row live in
[`001_initial.up.sql`](../server/internal/db/migrations/001_initial.up.sql).

### Enrollment Tokens Table

The `enrollment_tokens` table tracks tokens used for agent CSR enrollment:

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID PK | Identifier |
| `token` | TEXT UQ | The enrollment token string |
| `label` | TEXT | Human-readable label |
| `created_by` | UUID FK | References `users(id)` |
| `max_uses` | INTEGER | Maximum allowed enrollments (0 = unlimited) |
| `use_count` | INTEGER | Current enrollment count |
| `expires_at` | TIMESTAMPTZ | Expiration timestamp |
| `created_at` | TIMESTAMPTZ | Creation timestamp |

### Device Updates Table

The `device_updates` table tracks OTA update push/ack status per device:

| Column | Type | Description |
|--------|------|-------------|
| `id` | BIGINT PK | Identity column |
| `device_id` | UUID FK | References `devices(id)`, CASCADE delete |
| `version` | TEXT | Target version string |
| `status` | TEXT | `pending`, `success`, or `failed` |
| `error` | TEXT | Error message (empty on success) |
| `pushed_at` | TIMESTAMPTZ | When the update was pushed |
| `acked_at` | TIMESTAMPTZ | When the agent acknowledged (nullable) |

Indexed on `device_id` and `version` for fast lookups.

The `devices.capabilities` column stores a JSONB array of capability strings
(e.g., `'["Terminal","FileManager","RemoteDesktop"]'`). Capabilities are
reported by the agent during registration and persisted via `UpsertDevice`.
The web client uses this field to determine which session tabs to show.

The `devices.group_id` foreign key is nullable with `ON DELETE SET NULL` —
deleting a group ungroups its devices (sets `group_id` to NULL). Newly
enrolled devices start with `group_id = NULL` until assigned to a group.
The `agent_sessions.device_id` foreign key cascades on delete.

### Store Methods (Device)

| Method | Signature | Description |
|--------|-----------|-------------|
| `UpdateDeviceGroup` | `(ctx, DeviceID, GroupID) error` | Moves a device to a different group. Pass `uuid.Nil` as `GroupID` to ungroup the device (sets `group_id` to NULL). Updates `updated_at` timestamp. |

### Security Groups

The `security_groups` and `security_group_members` tables implement
role-based access control. A well-known "Administrators" group (UUID
`00000000-0000-0000-0000-000000000001`) is seeded on migration and cannot
be deleted (`is_system = TRUE`). Group membership is a many-to-many join
via `security_group_members`.

Key behaviors:
- Adding/removing a member of the Administrators group automatically syncs the `users.is_admin` boolean via `syncIsAdmin()` for backward compatibility
- The last member of the Administrators group cannot be removed (server returns 409 Conflict)
- The first registered user is auto-added to the Administrators group (bootstrap mechanism)
- JWT `admin` claims are derived from Administrators group membership at login/register time

### AMT Devices Table

The `amt_devices` table tracks Intel AMT devices connected via CIRA, independent from the agent `devices` table:

| Column | Type | Description |
|--------|------|-------------|
| `uuid` | UUID PK | AMT device UUID (from ProtocolVersion message) |
| `hostname` | TEXT | Device hostname |
| `model` | TEXT | Hardware model string |
| `firmware` | TEXT | AMT firmware version |
| `status` | TEXT | `online` / `offline` |
| `last_seen` | TIMESTAMPTZ | Last activity timestamp |

The upsert logic preserves existing non-empty fields (hostname, model, firmware) when the new value is empty, allowing incremental enrichment of device metadata.

### Device Hardware Table

The `device_hardware` table stores on-demand hardware inventory collected from agents:

| Column | Type | Description |
|--------|------|-------------|
| `device_id` | UUID PK | References `devices(id)`, CASCADE delete |
| `cpu_model` | TEXT | CPU model string |
| `cpu_cores` | INTEGER | Number of CPU cores |
| `ram_total_mb` | BIGINT | Total RAM in MB |
| `disk_total_mb` | BIGINT | Total disk in MB |
| `disk_free_mb` | BIGINT | Free disk in MB |
| `network_interfaces` | JSONB | Array of network interfaces (name, mac, ipv4, ipv6) |
| `updated_at` | TIMESTAMPTZ | Last update timestamp |

Hardware data is collected on demand via `RequestHardwareReport` control message and upserted via `UpsertDeviceHardware`. Retrieved via `GetDeviceHardware`.

### Device Logs Table

The `device_logs` table caches log entries retrieved on demand from agents via the control path:

| Column | Type | Description |
|--------|------|-------------|
| `id` | BIGINT PK | Identity column |
| `device_id` | UUID FK | References `devices(id)`, CASCADE delete |
| `timestamp` | TEXT | Raw timestamp string as emitted by the agent |
| `level` | TEXT | Log level (`TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `target` | TEXT | Tracing target / module path (default `''`) |
| `message` | TEXT | Log message body (default `''`) |
| `fetched_at` | TIMESTAMPTZ | When the row was cached (default `NOW()`) |

Indexes: `device_id`, `(device_id, level)`, `(device_id, timestamp)`.

### Store Methods (Device Logs)

| Method | Signature | Description |
|--------|-----------|-------------|
| `UpsertDeviceLogs` | `(ctx, DeviceID, []LogEntry) error` | Inserts or replaces cached log entries for a device |
| `QueryDeviceLogs` | `(ctx, DeviceID, filters) ([]LogEntry, int, error)` | Queries cached logs with level/time/search filters and pagination |
| `HasRecentLogs` | `(ctx, DeviceID) (bool, error)` | Returns true if cached logs exist within the 5-minute TTL window |

The 5-minute TTL avoids repeated round-trips to the agent. When `HasRecentLogs` returns true, the server serves from cache; otherwise it sends a `RequestDeviceLogs` control message and returns HTTP 202.

## Migrations

Migrations live in [`server/internal/db/migrations/`](../server/internal/db/migrations/)
and use `golang-migrate`. The Phase 13a cutover consolidated the prior
eleven SQLite migrations into a single flat Postgres-native migration:

- [`001_initial.up.sql`](../server/internal/db/migrations/001_initial.up.sql)
  creates every table, index, and the Administrators seed row in one pass.
- [`001_initial.down.sql`](../server/internal/db/migrations/001_initial.down.sql)
  drops them in FK-safe order.

On first startup, `NewPostgresStore` opens a connection, runs migrations,
and the server is ready. Schema changes made after Phase 13a land as new
sequentially numbered `.up.sql` / `.down.sql` pairs in the same directory.

## Backups

Daily `pg_dump` backups run inside the `postgres-backup` sidecar defined in
[`deploy/docker-compose.yml`](../deploy/docker-compose.yml). Output is
written to the `postgres-backups` Docker volume, with 7-day local
retention. Off-host replication (e.g. to OCI Object Storage via rclone) is
tracked as a follow-up and is not configured by default. See
[Infrastructure](Infrastructure.md) for VPS placement details.

## Data directory

The `-data-dir` flag (default: `./data`) no longer stores the database. It
holds just the TLS/VAPID material that still lives on disk:

```
data/
├── ca.crt      # Self-signed ECDSA P-256 CA certificate
├── ca.key      # CA private key
└── vapid.json  # VAPID keypair for Web Push
```

The database itself lives in the `postgres-data` Docker volume
(`/var/lib/postgresql/data` inside the postgres container).

## Transport security inside Docker

The connection string in deploy configs uses `sslmode=disable` because all
server ↔ postgres traffic stays on the internal Docker bridge network.
Postgres is not exposed to the host or the public internet. If Postgres is
ever moved off-host, switch to `sslmode=verify-full` and provision TLS
material on both sides.
