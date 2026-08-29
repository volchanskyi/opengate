# Database

OpenGate uses PostgreSQL 17 as its single storage backend behind per-domain
repositories. The server requires the `DATABASE_URL` env var (or
`-database-url` flag) at startup and exits fast if it is unset. See
[ADR-014](../adr/ADR-014-postgres-migration.md) for the rationale behind the
PostgreSQL choice and the supersession of ADR-003.

## Driver & connection pool

| Setting | Value | Source |
|---------|-------|--------|
| Driver | `github.com/jackc/pgx/v5/stdlib` | [`postgres.go`](../../server/internal/db/postgres.go) |
| Pool impl | `database/sql` adapter over pgx | [`postgres.go`](../../server/internal/db/postgres.go) |
| Migrations | `golang-migrate` with `database/pgx/v5` source | [`postgres.go`](../../server/internal/db/postgres.go), [`migrations/`](../../server/internal/db/migrations) |
| Max open conns / idle / lifetime | Set inside `NewPostgresStore` | [`postgres.go`](../../server/internal/db/postgres.go) |
| Size metric | `pg_database_size(current_database())` | [`postgres.go`](../../server/internal/db/postgres.go) `Size()` |

The size value feeds the `opengate_db_size_bytes` Prometheus gauge (see
[Monitoring](../infrastructure/Monitoring.md)).

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

## Tenancy

Four levels — tenant, customer, site, device — and the rules for what may belong
to what are in
[Tenancy and Access](../product/Tenancy-and-Access.md#the-four-levels). The isolation
boundary is the **tenant** and only the tenant; a customer is structural and
carries the tenant policy like every other tenant-scoped table. Filtering by
customer is a query concern: every fleet read accepts an `organization_id` and
narrows to it, returning the whole tenant when none is given.

The migrations that enforce the shape are
[`011_organizations`](../../server/internal/db/migrations/011_organizations.up.sql)
— every tenant has at least one customer and every device names one, and deleting
a tenant's last customer is refused — and
[`012_sites`](../../server/internal/db/migrations/012_sites.up.sql), which makes a
device's site a composite key against its customer:
`devices (organization_id, site_id)` references `sites (organization_id, id)`, so
the database refuses a site belonging to another customer outright. `site_id`
stays nullable, and a null referencing column leaves the pair unchecked, so an
unfiled machine is normal. Deleting a site clears only `site_id`; moving a device
to another customer clears it in the same statement. A site name is unique within
its customer rather than across the tenant.

Deleting a customer cascades its sites, its devices and, through them, their
telemetry, inventory, hardware and update rows.

The order these levels resolve in is one shared primitive,
[`internal/settings`](../../server/internal/settings/settings.go), so the ordering
exists in one place and cannot drift between the things that depend on it.
## Multi-Tenancy

Every tenant-owned table carries `tenant_id UUID NOT NULL` and is protected by
Postgres Row-Level Security. The server derives the active tenant from
the JWT `tenant` claim, stores it in request context, and each repository method
opens a tenant-scoped transaction through `dbtx.Scoped`.

Inside that transaction the server issues `SET LOCAL app.current_tenant = ...`
and `SET LOCAL app.is_admin = ...`; the settings reset automatically on commit
or rollback, so pooled connections do not leak tenant state between requests.
Tenant queries also carry explicit `WHERE tenant_id =
current_setting('app.current_tenant')::uuid` predicates so the `tenant_id`-leading
indexes stay usable instead of relying on RLS as a post-filter.

Admin cross-tenant access is policy-based: RLS policies also allow rows when
`app.is_admin` is true. Helm deployments build the server `DATABASE_URL` for the
dedicated runtime role in
[`server-deployment.yaml`](../../deploy/helm/opengate/templates/server-deployment.yaml);
[`zz-app-role.sh`](../../deploy/helm/opengate/files/zz-app-role.sh) and
[`cd.yml`](../../.github/workflows/cd.yml) keep that role non-superuser and without
`BYPASSRLS`, so a missing tenant GUC fails closed. Pre-tenant paths such as login
lookup and enrollment token validation opt into the default tenant
explicitly.

The RLS boundary is covered by per-repository cross-tenant-deny tests plus
[`TestMultitenancyMigrationRehearsal`](../../server/internal/db/store_test.go),
which applies `002_multitenancy` to seeded pre-tenant data, verifies backfill and
RLS behavior, runs in-container `pg_dump`/restore, re-verifies the restored copy,
and rolls the migration down cleanly.

## Schema

Tables are managed by `golang-migrate`. The Phase 13a fresh-start schema lives
in [`001_initial.up.sql`](../../server/internal/db/migrations/001_initial.up.sql);
the multi-tenant RLS layer lives in
[`002_multitenancy.up.sql`](../../server/internal/db/migrations/002_multitenancy.up.sql).
Edge Sentinel process snapshots are added by
[`003_telemetry.up.sql`](../../server/internal/db/migrations/003_telemetry.up.sql).

```
┌─────────────────────┐       ┌─────────────────────┐
│       users         │       │      groups_         │
│─────────────────────│       │─────────────────────│
│ id            PK    │◄──┐   │ id            PK    │
│ email         UQ    │   │   │ name                │
│ password_hash       │   │   │ created_at          │
│ display_name        │   │   │ updated_at          │
│ is_admin            │   │   └──────────┬──────────┘
│ created_at          │   │              │ 1:N (SET NULL)
│ updated_at          │   │   ┌──────────▼──────────┐
└─────────────────────┘   │   │      devices         │
                          │   │─────────────────────│
┌─────────────────────┐   │   │ id            PK    │
│  agent_sessions     │   │   │ group_id FK (nullable)│
│─────────────────────│   │   │ hostname            │
│ token         PK    │   │   │ os                  │
│ device_id     FK    │───┤   │ capabilities (JSONB)│
│ user_id       FK    │───┤   │ status              │
│ created_at          │   │   │ last_seen           │
└─────────────────────┘   │   │ created_at          │
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
[`001_initial.up.sql`](../../server/internal/db/migrations/001_initial.up.sql).

All tenant tables below include `tenant_id` in addition to the domain columns shown.

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

Hardware data is collected via the `RequestHardwareReport` control message and upserted via `UpsertDeviceHardware`. The server sends that request as an agent registers, so a device that reboots or reconnects with different RAM, disks or interfaces refreshes its row by coming back online. Retrieved via `GetDeviceHardware`.

### Device Logs

Raw device logs have no database table. They are brokered on demand: the server
sends a `RequestDeviceLogs` control message, blocks on the agent's bounded
response, redacts known secrets, and streams the lines straight back in the same
HTTP response. Nothing raw is persisted centrally, so tenant isolation for raw
logs is the agent connection's scope rather than an RLS row. See
[ADR-046](../adr/ADR-046-edge-sentinel-raw-log-broker.md) and the
[API reference](API-Reference.md).

### Device Processes Table

The `device_processes` table stores sanitized Edge Sentinel process snapshots:

| Column | Type | Description |
|--------|------|-------------|
| `id` | BIGINT PK | Identity column |
| `tenant_id` | UUID FK | Tenant scope, protected by forced RLS |
| `device_id` | UUID FK | References `devices(id)`, CASCADE delete |
| `ts` | TIMESTAMPTZ | Agent sample timestamp |
| `rank` | INTEGER | Top-N rank assigned by the agent sampler |
| `basename` | TEXT | Executable basename only |
| `cmdline_hash` | TEXT nullable | Optional hash, no raw command line |
| `pid` | BIGINT | Process id at sample time |
| `cpu` / `mem` | DOUBLE PRECISION | Reported process utilization values |
| `created_at` | TIMESTAMPTZ | Ingest timestamp |

The Postgres adapter lives in
[`server/internal/telemetry`](../../server/internal/telemetry) and always runs
through `dbtx.Scoped`. Numeric process metrics use rank-only labels in
VictoriaMetrics; basenames, PIDs, and command-line hashes stay in the RLS table.
The numeric side's retention and long-term (cold) tier are covered in
[Monitoring](../infrastructure/Monitoring.md#long-term-cold-tier).

### Device Inventory Table

The `device_inventory` table stores each device's current auto-discovered
footprint — one row per discovered component from a
[`DiscoveryReport`](Wire-Protocol.md): a listening port, host service, database
engine, container, or installed package.

| Column | Type | Description |
|--------|------|-------------|
| `id` | BIGINT PK | Identity column |
| `tenant_id` | UUID FK | Tenant scope, protected by forced RLS |
| `device_id` | UUID FK | References `devices(id)`, CASCADE delete |
| `kind` | TEXT | One of `port`, `service`, `db_engine`, `container`, `package` |
| `name` | TEXT | Primary label — owning process, unit, engine, or component name |
| `version` | TEXT | Engine or package version when known |
| `port` | INTEGER | Listening or engine port; 0 when not applicable |
| `proto` | TEXT | Transport for a port (`tcp`/`udp`) |
| `state` | TEXT | Run state for a service or container |
| `runtime` / `image` | TEXT | Container runtime and image reference |
| `first_seen` / `last_seen` | TIMESTAMPTZ | When the component first and most recently appeared |
| `created_at` | TIMESTAMPTZ | Ingest timestamp |

The Postgres adapter lives in
[`server/internal/inventory`](../../server/internal/inventory) and always runs
through `dbtx.Scoped`. Each discovery report replaces the device's footprint:
present components are upserted (advancing `last_seen`) while vanished ones are
pruned, so the stored rows always reflect the latest scan and stay bounded by the
agent's per-category caps. The report is scoped to the connection's authoritative
tenant, never an agent-supplied tenant, and carries descriptive attack-surface
data only — never a connection string or credential. It is exposed to any device
viewer in the tenant through
[`GET /devices/{id}/inventory`](API-Reference.md).

### Rule Configuration Tables

Three tables hold what a customer has changed about a monitoring rule, and which
machines a rule cannot be evaluated on. What a rule *is* — its predicate, window,
grouping key and shipped numbers — is not here: definitions are versioned YAML
compiled into the server from
[`server/internal/rules/catalogue/`](../../server/internal/rules/catalogue), which
is what lets a predicate be cost-bounded in CI before it reaches an endpoint. See
[Alerts and Rules](../product/Alerts-and-Rules.md) and
[ADR-071](../adr/ADR-071-rule-catalogue-bindings-and-durable-coverage.md).

- `rule_bindings` — a customer's parameter overrides, keyed
  `(organization_id, rule_id, level, level_key, selector)` where `level` is one
  of `device`, `site`, `organization`, `tenant`. `params` carries only values the
  rule declares tunable, validated against that rule's own bounds on write;
  `selector` is a bounded tag predicate and `precedence` breaks a tie between two
  selectors matching one machine at one rung. A partial unique index refuses two
  selectors at one rung sharing a precedence, so resolution never depends on row
  order.
- `rule_rollout` — per `(organization_id, rule_id)`: `enabled`, `canary_group`,
  `rollout_percent`, and `kill`. A customer with no row has not configured the
  rule, which is not the same as having switched it off — the shipped default
  applies.
- `rule_coverage_unsupported` — one row per `(device_id, rule_id)` a machine
  cannot evaluate, with `since`. Presence of the row *is* the state, so there is
  no column that can go stale; a machine that can evaluate the rule again has its
  row deleted rather than flipped. Whether a rule is currently being evaluated,
  and whether a machine has been heard from, stay in memory — those are liveness
  and are supposed to reset when the server loses sight of the fleet.

All three carry forced RLS on `tenant_id` through the shared `app_tenant_visible`
predicate, plus a composite `(tenant_id, organization_id)` foreign key so a row
cannot name a customer belonging to another tenant. The adapter is
[`server/internal/rules`](../../server/internal/rules).

### Investigation Tables

Three tables hold what a machine reported was wrong, the room those reports fold
into, and what people did about it. The adapter is
[`server/internal/alerts`](../../server/internal/alerts); the ingest path is
[`conn_alerts.go`](../../server/internal/agentapi/conn_alerts.go). See
[ADR-074](../adr/ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md).

- `alerts` — one row per thing a machine reported, keyed for idempotency on
  `(device_id, rule_id, rule_version, window_start)` so a reconnect replaying a
  queued alert lands on the row it already wrote rather than a second one.
  `evidence` is a compressed blob on the row itself, immutable once written and
  never fetched again: central keeps one 60 s average per dimension and there is
  no path for asking the endpoint afterwards, so what is not on the alert is not
  recorded anywhere. A size cap and a "codec named whenever a blob is present"
  rule are check constraints, not application convention.
- `incidents` — the room, keyed on `(organization_id, rule_id, scope,
  scope_key)`. A partial unique index over that key `WHERE status <> 'resolved'`
  allows exactly one open room per key, which is what makes folding an alert
  race-safe; resolved rooms sit outside the index, so the same condition
  recurring next month opens a new one. `occurrences` and `device_count` are
  application state — no foreign key keeps them true when a machine is erased —
  so the engine restates both from the room's own alerts rather than
  incrementing them. Which alerts land in which room, how a room moves through
  its statuses, and when a quiet one closes itself are all
  [Investigations](../product/Investigations.md#how-alerts-group-into-incidents).
- `incident_events` — the append-only history behind the room's current state,
  which is what a handover between two technicians reads.

`severity`, `status`, `scope`, `cause_code` and the event `kind` are closed sets
enforced by check constraint: a value nothing downstream can render would
otherwise be stored happily and found by whoever opens the incident. All three
tables carry forced RLS on `tenant_id` through the shared `app_tenant_visible`
predicate, plus a composite `(tenant_id, organization_id)` foreign key so a row
cannot name a customer belonging to another tenant.

A customer may store **500 alerts per rolling hour**
([`OrganizationHourlyCeiling`](../../server/internal/alerts/types.go)). Per customer,
never per tenant: at the tenant one customer's storm would consume the budget of
every other customer the MSP looks after. What the ceiling refuses is counted
under `opengate_alerts_suppressed_total` (see
[Metrics Reference](./Metrics-Reference.md#detection-alerts-incidents-and-coverage))
and folded into one storm incident
carrying the count, so suppression is never silent.

### Data Lifecycle Tables

Two system-level tables back right-to-be-forgotten erasure (see
[Data Lifecycle](../product/Data-Erasure.md)). Neither is tenant-scoped (RLS) and neither
carries a foreign key to `tenants`: both must outlive a tenant's own
data so the deny-list keeps rejecting a purged subject and the completion record
survives as the erasure proof.

- `deleted_ids` — the persisted tombstone / deny-list. One row per purged device
  or tenant (`scope`), recorded before any store is touched and retained
  indefinitely; it carries ids and purge scope only, never telemetry.
- `purge_jobs` — per-subject purge progress (`state` plus `vm_deleted`,
  `object_deleted`, `pg_deleted`, `verified` flags), so a purge is idempotent and
  [resumes](../../server/internal/lifecycle/orchestrator.go) after a crash.

## Migrations

Migrations live in [`server/internal/db/migrations/`](../../server/internal/db/migrations)
and use `golang-migrate`. The Phase 13a cutover consolidated the prior
eleven SQLite migrations into a single flat Postgres-native migration:

- [`001_initial.up.sql`](../../server/internal/db/migrations/001_initial.up.sql)
  creates every table, index, and the Administrators seed row in one pass.
- [`001_initial.down.sql`](../../server/internal/db/migrations/001_initial.down.sql)
  drops the base schema in FK-safe order.
- [`002_multitenancy.up.sql`](../../server/internal/db/migrations/002_multitenancy.up.sql)
  creates `tenants`, seeds the default tenant
  (`00000000-0000-0000-0000-000000000002`), backfills tenant tables, adds
  `tenant_id`-leading indexes, and enables forced RLS policies.
- [`002_multitenancy.down.sql`](../../server/internal/db/migrations/002_multitenancy.down.sql)
  removes those policies, indexes, and columns for rollback rehearsal.
- [`003_telemetry.up.sql`](../../server/internal/db/migrations/003_telemetry.up.sql)
  creates the forced-RLS `device_processes` table for Edge Sentinel process
  snapshots.
- [`003_telemetry.down.sql`](../../server/internal/db/migrations/003_telemetry.down.sql)
  removes the process table for rollback.
- [`004_retire_device_logs.up.sql`](../../server/internal/db/migrations/004_retire_device_logs.up.sql)
  drops the central `device_logs` cache; raw logs are brokered on demand.
- [`004_retire_device_logs.down.sql`](../../server/internal/db/migrations/004_retire_device_logs.down.sql)
  recreates the table for rollback rehearsal.
- [`005_inventory.up.sql`](../../server/internal/db/migrations/005_inventory.up.sql)
  creates the forced-RLS `device_inventory` table for the auto-discovered device
  footprint.
- [`005_inventory.down.sql`](../../server/internal/db/migrations/005_inventory.down.sql)
  removes the inventory table for rollback.
- [`006_data_lifecycle.up.sql`](../../server/internal/db/migrations/006_data_lifecycle.up.sql)
  creates the non-RLS `deleted_ids` deny-list and `purge_jobs` progress tables for
  right-to-be-forgotten erasure (see [Data Lifecycle](../product/Data-Erasure.md)).
- [`006_data_lifecycle.down.sql`](../../server/internal/db/migrations/006_data_lifecycle.down.sql)
  drops both tables for rollback.
- [`007_maintenance_mode.up.sql`](../../server/internal/db/migrations/007_maintenance_mode.up.sql)
  adds `maintenance_on`/`maintenance_since`/`maintenance_by`/`maintenance_reason`
  to `devices` (the server-authoritative maintenance desired state, default
  Active) plus a partial index on `(tenant_id) WHERE maintenance_on` for the fleet
  count (see [ADR-056](../adr/ADR-056-device-maintenance-mode.md)).
- [`007_maintenance_mode.down.sql`](../../server/internal/db/migrations/007_maintenance_mode.down.sql)
  drops the columns and index for rollback.
- [`008_amt_device_link.up.sql`](../../server/internal/db/migrations/008_amt_device_link.up.sql)
  adds the SMBIOS `system_uuid` join key and the AMT columns to
  `device_hardware`, links `amt_devices` to its owning device, and reduces
  `amt_devices` to connection state (see
  [ADR-061](../adr/ADR-061-amt-as-device-property.md)).
- [`008_amt_device_link.down.sql`](../../server/internal/db/migrations/008_amt_device_link.down.sql)
  restores the standalone AMT columns for rollback.
- [`009_drop_group_owner`](../../server/internal/db/migrations/009_drop_group_owner.up.sql)
  removes the device group's owner column.
- [`010_rename_organizations_to_tenants`](../../server/internal/db/migrations/010_rename_organizations_to_tenants.up.sql)
  renames the isolation boundary to `tenants` and every `org_id` column to
  `tenant_id`, freeing the name for the customer level below it.
- [`011_organizations`](../../server/internal/db/migrations/011_organizations.up.sql)
  adds the customer an MSP serves inside a tenant, gives every tenant one, and
  links every device to it.
- [`012_sites`](../../server/internal/db/migrations/012_sites.up.sql)
  turns device groups into sites under a customer, with a composite key so a
  device can only be filed into a site inside its own customer.
- [`013_rules`](../../server/internal/db/migrations/013_rules.up.sql)
  adds `rule_bindings`, `rule_rollout` and `rule_coverage_unsupported`, the
  shared `app_tenant_visible` policy predicate, and the
  `organizations(tenant_id, id)` key those tables' composite foreign keys point
  at.
- [`014_investigations`](../../server/internal/db/migrations/014_investigations.up.sql)
  adds `alerts`, `incidents` and `incident_events`, the alert idempotency key,
  the partial unique index that allows one open incident per grouping key, and
  the check constraints holding severity, status, scope, cause code and event
  kind to their closed sets.
- Each of the above has a matching `.down.sql` walked by the rollback rehearsal.

The automated rollback/dump rehearsal lives in
[`server/internal/db/store_test.go`](../../server/internal/db/store_test.go) and
logs the Wave-0 evidence when run with `go test -v ./internal/db -run
TestMultitenancyMigrationRehearsal`.

On first startup, `NewPostgresStore` opens a connection, runs migrations,
and the server is ready. Schema changes made after Phase 13a land as new
sequentially numbered `.up.sql` / `.down.sql` pairs in the same directory.

Migrations run on their own single-connection pool that carries the
cross-tenant `app.is_admin` / `app.current_tenant` scope, because the deployed role
is `NOBYPASSRLS` and owns tables under forced RLS — a migration that touches
rows is otherwise refused by the tenant policy. The pool that serves application
traffic never carries that scope. See
[ADR-041](../adr/ADR-041-postgres-rls-multitenancy.md).

## Backups

On the cluster a daily `pg_dump` runs as the
[`postgres-backup` CronJob](../../deploy/helm/opengate/templates/postgres-backup-cronjob.yaml):
an init container dumps + gzips the database into a shared `emptyDir`, then a
`curl` container streams it to OCI Object Storage via a **write-only**
pre-authenticated request (PAR) URL — there is no in-cluster backup volume, and
the off-cluster copy survives total cluster loss. Retention is an Object Storage
lifecycle policy on the bucket. The schedule,
retention threshold, and upload image are the `postgres.backup`
[values](../../deploy/helm/opengate/values.yaml); the bucket / PAR / lifecycle setup
commands are in the chart
[`NOTES.txt`](../../deploy/helm/opengate/templates/NOTES.txt). Rationale (and the
50 GB block volume this frees under the OCI free-tier cap):
[ADR-035](../adr/ADR-035-oke-free-tier-block-volume-remediation.md).

## Data directory

The `-data-dir` flag (default: `./data`) holds the TLS/VAPID material that
lives on disk:

```
data/
├── ca.crt      # Self-signed ECDSA P-256 CA certificate
├── ca.key      # CA private key
└── vapid.json  # VAPID keypair for Web Push
```

The production database lives in the app chart's Postgres StatefulSet
([`postgres-statefulset.yaml`](../../deploy/helm/opengate/templates/postgres-statefulset.yaml)).
Production keeps a persistent `oci-bv` volume claim; staging sets
`postgres.storage.persistent=false` and uses `emptyDir` because staging data is
ephemeral E2E/smoke-test state.

## Transport Security Inside Kubernetes

The Helm-generated connection string uses `sslmode=disable` because server ↔
Postgres traffic stays inside the Kubernetes cluster via the chart's headless
Service DNS name. Postgres is not exposed through Ingress, NodePort, or a public
OCI load balancer. If Postgres is ever moved outside the cluster boundary, switch
to `sslmode=verify-full` and provision TLS material on both sides.
