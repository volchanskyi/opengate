# Tenancy and Access

Who a machine belongs to, who may look at it, and what the console records about
what people did.

## The four levels

| Level | What it is |
|---|---|
| **Tenant** | The service provider. This is the wall the database itself enforces |
| **Customer** | One customer inside a tenant (an organization) |
| **Site** | A location or department inside one customer |
| **Device** | One managed machine, in exactly one customer and at most one of its sites |

**The isolation boundary is the tenant, and only the tenant.** A customer is
structural: it decides what a technician is looking at, and what a rule or a
ceiling applies to. Filtering by customer is a query concern — every fleet read
accepts a customer and narrows to it, and returns the whole tenant when none is
given.

The wall itself, and the mechanism that enforces it, are in
[Database](../architecture/Database.md#multi-tenancy).

### Rules the structure always obeys

- **Nothing is orphaned.** Every tenant has at least one customer and every device
  names one. A device created without a customer takes the tenant's oldest.
- **A tenant's last customer cannot be deleted.** Deleting any other customer
  cascades its sites, its devices and everything hanging off them — telemetry,
  inventory, hardware and update records.
- **A site belongs to its customer.** A device's site must be one of its own
  customer's sites; the database refuses the mismatch outright rather than
  storing it.
- **An unfiled machine is normal.** Site is optional.
- **Closing an office unfiles its machines**, it does not decommission them:
  deleting a site clears the filing and leaves the devices.
- **Moving a device to another customer clears its site** in the same step.
- **Site names are unique within a customer**, not across the tenant — "Head
  Office" names a different building for each one.
- **Filing is decided by the server.** A registering agent's site counts only if
  it belongs to the customer the device lands in, and a reconnect never refiles a
  machine.

### The settings ladder

Configurable values resolve in one fixed order: **device, then its site, then its
customer, then the tenant, then what shipped**. Every feature that has tunable
values reads that same ladder, so the ordering exists in one place and cannot
drift between features.

One class of setting reads the ladder the other way up — a customer-wide stop must
not be undone by a value set on one machine — and that exception is stated
explicitly rather than left implied.

## Users, groups and permissions

Access is granted through **security groups**. The **Administrators** group is
created with the system and cannot be deleted.

| Behaviour | Detail |
|---|---|
| Bootstrap | The first registered user is added to Administrators automatically |
| Last administrator | The final member of Administrators cannot be removed — the request is refused |
| Admin rights | Come from Administrators membership, and are attached to the session at sign-in |
| Scope | The administrator flag is **per tenant** — there is no administrator scoped to one customer |

The tables behind groups and membership are in
[Database](../architecture/Database.md#security-groups).

## Settings

The settings section is administrator-only. Every user, administrator or not, has
a `/profile` page for their own display name.

| Screen | Path | What you can do |
|---|---|---|
| Customers | `/settings/customers` | Add, rename, retire and delete the tenant's customers |
| Users | `/settings/users` | List users, grant or remove admin, delete a user |
| Audit log | `/settings/audit` | Search and page through recorded actions |
| Agent settings | `/settings/updates` | Enrollment tokens, update manifests, pushing updates, the signing key |
| Permissions | `/settings/security/permissions` | Security groups and their membership |
| Data lifecycle | `/settings/data-lifecycle` | Run and watch a tenant-wide erasure |

The **Settings** link appears in the navigation bar only for administrators.

## Audit log

Security-relevant actions are recorded with the actor and the moment, and are
searchable from `/settings/audit`.

| Area | Actions |
|---|---|
| Users and sessions | `user.register`, `user.login`, `user.update`, `user.delete`, `session.create`, `session.delete` |
| Security groups | `security_group.create`, `security_group.delete`, `security_group.add_member`, `security_group.remove_member` |
| Devices | `device.restart`, `device.maintenance.enter`, `device.maintenance.exit`, `device.logs.read`, `device.delete` |
| Enrollment and updates | `enrollment.create`, `enrollment.delete`, `update.publish`, `update.push` |
| Rules and labels | `rule.binding.set`, `rule.binding.delete`, `rule.rollout.set`, `rule.clamp.acknowledge`, `alert.limits.set`, `device.tag.assign`, `device.tag.clear`, `device.tag.label.create`, `device.tag.label.delete` |
| Incidents | `incident.assign`, `incident.status` |
| Customers and erasure | `organization.delete`, `tenant.purge` |

## Related

- [Fleet and Devices](./Fleet-and-Devices.md) — the customer picker and site sidebar
- [Rule Administration](./Rule-Administration.md) — what tuning resolves down the ladder
- [Data Erasure](./Data-Erasure.md) — deleting a customer, a device or a tenant
