# Tenancy and Access

Who a machine belongs to, who may look at it, and what the console records about
what people did.

## Four levels

Four levels, matching what the rest of the market means by the words:

| Level | What it is |
|---|---|
| Tenant | The MSP. The wall the database enforces. |
| Organization | One customer inside a tenant. |
| Site | A location or department inside one customer. |
| Device | One managed machine, in exactly one customer and at most one of its sites. |

The isolation boundary is the **tenant** and only the tenant. An organization is
structural — it decides what a technician is looking at and what a rule or a
ceiling applies to — so it carries the tenant policy like every other
tenant-scoped table and adds no second wall of its own. Filtering by customer is
a query concern: every fleet read accepts an `organization_id` and narrows to it,
and returns the whole tenant when none is given.

Every tenant has at least one organization and every device names one, so nothing
is ever orphaned:
[`011_organizations`](../../server/internal/db/migrations/011_organizations.up.sql)
gives each existing tenant its own, a row written without a customer takes the
tenant's oldest, and deleting a tenant's last customer is refused. Deleting any
other customer cascades its sites, its devices and, through them, their
telemetry, inventory, hardware and update rows.

A device's site must belong to the device's own customer, and that is a pair
rather than a value, so
[`012_sites`](../../server/internal/db/migrations/012_sites.up.sql) enforces it as a
composite key — `devices (organization_id, site_id)` references
`sites (organization_id, id)` — and the database refuses the mismatch outright.
The site stays nullable, since an unfiled machine is normal, and a null
referencing column leaves the pair unchecked. Two consequences follow from the
same key: deleting a site clears only `site_id`, so closing an office unfiles its
machines rather than decommissioning them; and moving a device to another
customer clears the site in the same statement, so the office it left never
travels with it. A site name is unique within its customer rather than across the
tenant, because "Head Office" names a different building for each one.

Filing is a server-side decision. A registering agent's site counts only when it
belongs to the customer the device lands in, and a reconnect never refiles —
otherwise a machine moved to another customer would come back naming an office
that customer does not have, and the pair would refuse the reconnect.

The order these levels resolve in is one shared primitive,
[`internal/settings`](../../server/internal/settings/settings.go): device, then its
site, then its customer, then the tenant, then what shipped. Where a configurable
value is *stored* belongs to whatever feature it configures, so the ordering
exists in one place and cannot drift between the things that depend on it. One
class of setting reads the ladder the other way up — a customer-wide stop must
not be undone by a value set on a single machine — and that exception is named
rather than implied.


The wall the database enforces, and the mechanism that enforces it, are in
[Database](../architecture/Database.md#multi-tenancy).

## Security groups and RBAC

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


The tables behind them are in
[Database](../architecture/Database.md#security-groups).

## Settings (Admin)

The web client includes a settings section (`/settings`) protected by `AdminGuard`. Old `/admin/*` routes redirect to `/settings`.

| Route | Component | Description |
|-------|-----------|-------------|
| `/settings/customers` | `OrganizationManagement` | Add, rename, retire and delete the tenant's customers |
| `/settings/users` | `UserManagement` | List, toggle admin, delete users |
| `/settings/audit` | `AuditLog` | Searchable, paginated audit event viewer |
| `/settings/updates` | `AgentUpdates` | Agent update manifests, push updates, enrollment tokens, signing key display |
| `/settings/security/permissions` | `Permissions` | Security groups and membership (RBAC) |

The "Settings" link appears in the navbar only for users with `is_admin=true`. State is managed by `admin-store.ts` and `push-store.ts` (Zustand).

Every user has a `/profile` page of their own, outside the admin section, for
editing their display name.

## Audit log

Security-relevant actions are recorded to the `audit_events` table via fire-and-forget goroutines:

- `user.register`, `user.login`, `user.delete`, `user.update`
- `session.create`, `session.delete`
- `device.delete` (triggers agent deregistration)

The audit log is queryable via `GET /api/v1/audit` (admin-only) with optional `user_id`, `action`, `limit`, and `offset` parameters.
