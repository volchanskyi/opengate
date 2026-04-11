# Database

## Engines

OpenGate supports two backends behind the same `db.Store` interface. The server selects at startup based on the `DATABASE_URL` env var (or `-database-url` flag):

- **PostgreSQL 17** — preferred for staging and production deployments.
- **SQLite (modernc.org/sqlite)** — fallback for local development and single-node installs; used automatically when `DATABASE_URL` is unset.

Both backends ship their own migration set under `server/internal/db/migrations/{sqlite,postgres}` and are tested against the full `db.Store` contract.

### PostgreSQL

| Setting | Value | Rationale |
|---------|-------|-----------|
| Driver | `github.com/jackc/pgx/v5/stdlib` | `database/sql` adapter over pgx |
| Migrations | `golang-migrate` with `database/pgx/v5` | iofs-embedded SQL under `migrations/postgres` |
| ID columns | `BIGINT GENERATED ALWAYS AS IDENTITY` / `UUID` | Native Postgres identity + UUID types |
| Timestamps | `TIMESTAMPTZ` | Time-zone aware; `CURRENT_TIMESTAMP` defaults |
| JSON columns | `JSONB` | Indexable binary JSON for hardware/capability payloads |
| Upserts | `ON CONFLICT ... DO UPDATE` / `DO NOTHING` | Matches SQLite semantics where needed |
| Size metric | `pg_database_size(current_database())` | Exposed via `opengate_db_size_bytes` gauge |

### SQLite

| Setting | Value | Rationale |
|---------|-------|-----------|
| Journal mode | WAL | Concurrent reads during writes |
| `MaxOpenConns` | 1 | SQLite single-writer constraint |
| `foreign_keys` | ON | Enforced via pragma on every connection |
| Driver | `modernc.org/sqlite` | Pure Go, no CGO required |
| Size metric | `PRAGMA page_count * PRAGMA page_size` | Exposed via `opengate_db_size_bytes` gauge |

## Schema

Fifteen tables managed by `golang-migrate`:

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
│ user_id       FK    │───┤   │ capabilities (JSON) │
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
│ p256dh              │       │ id        PK (auto)  │
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

### Enrollment Tokens Table

The `enrollment_tokens` table tracks tokens used for agent CSR enrollment:

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | UUID |
| `token` | TEXT UQ | The enrollment token string |
| `label` | TEXT | Human-readable label |
| `created_by` | TEXT FK | References `users(id)` |
| `max_uses` | INTEGER | Maximum allowed enrollments (0 = unlimited) |
| `use_count` | INTEGER | Current enrollment count |
| `expires_at` | TEXT | ISO 8601 expiration timestamp |
| `created_at` | TEXT | ISO 8601 creation timestamp |

### Device Updates Table

The `device_updates` table tracks OTA update push/ack status per device:

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `device_id` | TEXT FK | References `devices(id)`, CASCADE delete |
| `version` | TEXT | Target version string |
| `status` | TEXT | `pending`, `success`, or `failed` |
| `error` | TEXT | Error message (empty on success) |
| `pushed_at` | TEXT | ISO 8601 timestamp when update was pushed |
| `acked_at` | TEXT | ISO 8601 timestamp when agent acknowledged (nullable) |

Indexed on `device_id` and `version` for fast lookups.

The `devices.capabilities` column stores a JSON array of capability strings (e.g., `'["Terminal","FileManager","RemoteDesktop"]'`). Capabilities are reported by the agent during registration and persisted via `UpsertDevice`. The web client uses this field to determine which session tabs to show.

The `devices.group_id` foreign key is nullable with `ON DELETE SET NULL` — deleting a group ungroups its devices (sets `group_id` to NULL). Newly enrolled devices start with `group_id = NULL` until assigned to a group. The `agent_sessions.device_id` foreign key cascades on delete.

### Store Methods (Device)

| Method | Signature | Description |
|--------|-----------|-------------|
| `UpdateDeviceGroup` | `(ctx, DeviceID, GroupID) error` | Moves a device to a different group. Pass `uuid.Nil` as `GroupID` to ungroup the device (sets `group_id` to NULL). Updates `updated_at` timestamp. |

### Security Groups

The `security_groups` and `security_group_members` tables implement role-based access control. A well-known "Administrators" group (UUID `00000000-0000-0000-0000-000000000001`) is seeded on migration and cannot be deleted (`is_system = 1`). Group membership is a many-to-many join via `security_group_members`.

Key behaviors:
- Adding/removing a member of the Administrators group automatically syncs the `users.is_admin` boolean via `syncIsAdmin()` for backward compatibility
- The last member of the Administrators group cannot be removed (server returns 409 Conflict)
- The first registered user is auto-added to the Administrators group (bootstrap mechanism)
- JWT `admin` claims are derived from Administrators group membership at login/register time

### AMT Devices Table

The `amt_devices` table tracks Intel AMT devices connected via CIRA, independent from the agent `devices` table:

| Column | Type | Description |
|--------|------|-------------|
| `uuid` | TEXT PK | AMT device UUID (from ProtocolVersion message) |
| `hostname` | TEXT | Device hostname |
| `model` | TEXT | Hardware model string |
| `firmware` | TEXT | AMT firmware version |
| `status` | TEXT | `online` / `offline` |
| `last_seen` | TEXT | ISO 8601 timestamp of last activity |

The upsert logic preserves existing non-empty fields (hostname, model, firmware) when the new value is empty, allowing incremental enrichment of device metadata.

### Device Hardware Table

The `device_hardware` table stores on-demand hardware inventory collected from agents:

| Column | Type | Description |
|--------|------|-------------|
| `device_id` | TEXT PK | References `devices(id)`, CASCADE delete |
| `cpu_model` | TEXT | CPU model string |
| `cpu_cores` | INTEGER | Number of CPU cores |
| `ram_total_mb` | INTEGER | Total RAM in MB |
| `disk_total_mb` | INTEGER | Total disk in MB |
| `disk_free_mb` | INTEGER | Free disk in MB |
| `network_interfaces` | TEXT | JSON array of network interfaces (name, mac, ipv4, ipv6) |
| `updated_at` | TEXT | ISO 8601 timestamp of last update |

Hardware data is collected on demand via `RequestHardwareReport` control message and upserted via `UpsertDeviceHardware`. Retrieved via `GetDeviceHardware`.

### Device Logs Table

The `device_logs` table caches log entries retrieved on demand from agents via the control path:

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `device_id` | TEXT FK | References `devices(id)`, CASCADE delete |
| `timestamp` | TEXT | ISO 8601 timestamp of the log line |
| `level` | TEXT | Log level (`TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `target` | TEXT | Tracing target / module path (default `''`) |
| `message` | TEXT | Log message body (default `''`) |
| `fetched_at` | TEXT | ISO 8601 timestamp when the row was cached (default `datetime('now')`) |

Indexes: `device_id`, `(device_id, level)`, `(device_id, timestamp)`.

### Store Methods (Device Logs)

| Method | Signature | Description |
|--------|-----------|-------------|
| `UpsertDeviceLogs` | `(ctx, DeviceID, []LogEntry) error` | Inserts or replaces cached log entries for a device |
| `QueryDeviceLogs` | `(ctx, DeviceID, filters) ([]LogEntry, int, error)` | Queries cached logs with level/time/search filters and pagination |
| `HasRecentLogs` | `(ctx, DeviceID) (bool, error)` | Returns true if cached logs exist within the 5-minute TTL window |

The 5-minute TTL avoids repeated round-trips to the agent. When `HasRecentLogs` returns true, the server serves from cache; otherwise it sends a `RequestDeviceLogs` control message and returns HTTP 202.

## Migrations

Migrations live in `server/internal/db/migrations/` and use `golang-migrate`:

```
server/internal/db/migrations/
├── 001_initial.up.sql       # Core tables (users, groups, devices, sessions, push, audit)
├── 001_initial.down.sql
├── 002_amt_devices.up.sql   # Intel AMT device tracking
├── 002_amt_devices.down.sql
├── 003_agent_version.up.sql     # Add agent_version column to devices
├── 003_agent_version.down.sql
├── 004_enrollment_tokens.up.sql # CSR enrollment token management
├── 004_enrollment_tokens.down.sql
├── 005_security_groups.up.sql   # Security groups + membership (RBAC)
├── 005_security_groups.down.sql
├── 006_nullable_device_group.up.sql   # Nullable device group_id (SET NULL on group delete)
├── 006_nullable_device_group.down.sql
├── 007_device_updates.up.sql   # OTA update tracking (push/ack status per device)
├── 007_device_updates.down.sql
├── 008_device_capabilities.up.sql  # Add capabilities JSON column to devices
├── 008_device_capabilities.down.sql
├── 009_hardware_info.up.sql         # Device hardware inventory table
├── 009_hardware_info.down.sql
├── 010_device_logs.up.sql       # On-demand device log cache
├── 010_device_logs.down.sql
├── 011_normalize_os.up.sql      # Normalize devices.os to {linux,windows,darwin}; add os_display for raw value
└── 011_normalize_os.down.sql
```

On first startup, the server creates the SQLite database under the configured `data-dir` and runs all pending migrations.

## Data Directory

The `-data-dir` flag (default: `./data`) holds:

```
data/
├── opengate.db      # SQLite database
├── ca.crt           # Self-signed ECDSA P-256 CA certificate
└── ca.key           # CA private key
```

Both the database and CA are created automatically on first startup.
