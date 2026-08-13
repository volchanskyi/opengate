# API Reference

## Interactive Documentation

The full API reference is available as an interactive [Scalar](https://github.com/scalar/scalar) viewer:

**[OpenGate API Reference](https://volchanskyi.github.io/opengate/docs/api/)**

The spec is automatically deployed to GitHub Pages on every push to `dev`.

## OpenAPI Specification

The API is defined in `api/openapi.yaml` (OpenAPI 3.0.3). This file is the **single source of truth** — it generates both the Go server interface and the TypeScript client types.

### Code Generation

| Target | Tool | Output |
|--------|------|--------|
| Go server | `oapi-codegen` (strict server + chi) | `server/internal/api/openapi_gen.go` |
| TypeScript client | `openapi-typescript` | `web/src/types/api.d.ts` |

```bash
# Regenerate Go server code
cd server && go generate ./...

# Regenerate TypeScript types
cd web && npm run generate:api
```

### Strict Server Pattern

The Go server uses `oapi-codegen`'s **strict server interface**. Each endpoint is a typed method that receives a request object and returns a response object — no manual JSON encoding/decoding:

```go
func (s *Server) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
    return GetHealth200JSONResponse{Status: "ok"}, nil
}
```

Contract drift between the spec and the server becomes a compile error.

### TypeScript Client

The web client uses `openapi-fetch` with generated types for fully-typed API calls:

```typescript
const { data, error } = await api.GET('/api/v1/sites');
// data is typed as Group[], error is typed as ApiError
```

## Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/health` | GET | No | Health check |
| `/api/v1/auth/register` | POST | No | Register a new user |
| `/api/v1/auth/login` | POST | No | Login and receive JWT |
| `/api/v1/users/me` | GET | JWT | Get current user |
| `/api/v1/users` | GET | JWT (admin) | List all users |
| `/api/v1/users/{id}` | DELETE | JWT (admin) | Delete a user |
| `/api/v1/users/{id}` | PATCH | JWT | Update user — admins can set `is_admin` and `display_name`; non-admins can update their own `display_name` only |
| `/api/v1/audit` | GET | JWT (admin) | List audit events (filterable) |
| `/api/v1/security-groups` | GET | JWT (admin) | List all security groups |
| `/api/v1/security-groups` | POST | JWT (admin) | Create a security group |
| `/api/v1/security-groups/{id}` | GET | JWT (admin) | Get group with members |
| `/api/v1/security-groups/{id}` | DELETE | JWT (admin) | Delete group (403 for system groups) |
| `/api/v1/security-groups/{id}/members` | POST | JWT (admin) | Add user to group |
| `/api/v1/security-groups/{id}/members/{userId}` | DELETE | JWT (admin) | Remove user from group |
| `/api/v1/push/subscribe` | POST | JWT | Subscribe to Web Push notifications |
| `/api/v1/push/subscribe` | DELETE | JWT | Unsubscribe from Web Push |
| `/api/v1/push/vapid-key` | GET | JWT | Get VAPID public key |
| `/api/v1/sites` | POST | JWT (admin) | Create a site under a customer |
| `/api/v1/sites` | GET | JWT | List sites, optionally narrowed by customer |
| `/api/v1/sites/{id}` | GET | JWT | Get a site |
| `/api/v1/sites/{id}` | DELETE | JWT (admin) | Delete a site; its devices stay with the customer, unfiled |
| `/api/v1/devices` | GET | JWT | List devices (optional `organization_id` and `site_id` filters) |
| `/api/v1/devices/summary` | GET | JWT | Fixed-size fleet rollup for the dashboard (status tiles + edge-health bands) |
| `/api/v1/devices/{id}` | GET | JWT | Get a device (includes `capabilities` array) |
| `/api/v1/devices/{id}` | PATCH | JWT (admin) | Update device (file into a `site_id` in its own customer; the all-zeros UUID unfiles it) |
| `/api/v1/devices/{id}` | DELETE | JWT (admin) | Delete a device and purge all its telemetry ([Data Lifecycle](Data-Lifecycle.md)) |
| `/api/v1/devices/{id}/restart` | POST | JWT | Restart agent on device (optional `reason` field) |
| `/api/v1/devices/{id}/hardware` | GET | JWT | Get hardware inventory for device (200 cached / 202 requested from agent) |
| `/api/v1/devices/{id}/logs` | GET | JWT (admin) | Get device log entries (on-demand via agent) |
| `/api/v1/devices/{id}/metrics` | GET | JWT | Downsampled numeric telemetry for a device window, on a request-derived bucket grid |
| `/api/v1/devices/{id}/inventory` | GET | JWT | Get the device's auto-discovered footprint (ports, services, DB engines, containers, packages) |
| `/api/v1/sessions` | POST | JWT | Create a remote session |
| `/api/v1/sessions` | GET | JWT | List sessions (requires `device_id` query param) |
| `/api/v1/sessions/{token}` | DELETE | JWT | Delete a session |
| `/api/v1/amt/devices/{uuid}/power` | POST | JWT | Send AMT power command (on/cycle/soft-off/hard-reset) to the AMT connection identified by `uuid`, which must belong to a device in the caller's tenant |
| `/api/v1/enroll/{token}` | POST | No | Enroll agent (CSR signing, returns CA + cert) |
| `/api/v1/server/ca` | GET | No | Get server CA certificate PEM |
| `/api/v1/enrollment-tokens` | POST | JWT (admin) | Create enrollment token |
| `/api/v1/enrollment-tokens` | GET | JWT (admin) | List enrollment tokens |
| `/api/v1/enrollment-tokens/{id}` | DELETE | JWT (admin) | Delete enrollment token |
| `/api/v1/updates/manifests` | GET | No | List agent update manifests |
| `/api/v1/updates/push` | POST | JWT (admin) | Push update to devices |
| `/api/v1/updates/status/{version}` | GET | JWT | Get update status for a version |
| `/api/v1/updates/signing-key` | GET | JWT | Get Ed25519 update signing public key |
| `/api/v1/server/install.sh` | GET | No | Get agent install script |
| `/api/v1/organizations` | GET | JWT | List the tenant's customers |
| `/api/v1/organizations` | POST | JWT (admin) | Add a customer |
| `/api/v1/organizations/{id}` | GET | JWT | Get one customer |
| `/api/v1/organizations/{id}` | PATCH | JWT (admin) | Rename, retire or restore a customer |
| `/api/v1/organizations/{id}` | DELETE | JWT (admin) | Delete a customer and its devices (refused for a tenant's last one) |
| `/api/v1/devices/{id}/organization` | PUT | JWT (admin) | Move a device to another customer in the same tenant |
| `/api/v1/tenants/{tenantId}/purge` | POST | JWT (admin) | Purge a whole tenant's telemetry (async, tenant-scoped; [Data Lifecycle](Data-Lifecycle.md)) |
| `/api/v1/purge-jobs/{jobId}` | GET | JWT | Get purge job status |
| `/ws/relay/{token}` | GET | Token | WebSocket relay (bidirectional agent↔browser pipe) |

### Device Logs

`GET /api/v1/devices/{id}/logs` brokers raw logs from the agent on demand via the QUIC control path. The request **blocks** until the agent returns a bounded response, which is redacted and streamed straight back; nothing is persisted centrally (see [ADR-046](adr/ADR-046-edge-sentinel-raw-log-broker.md)). Reading raw logs is an elevated action restricted to administrators, and every pull writes a `device.logs.read` audit event.

**Query Parameters**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `level` | string | _(all)_ | Filter by log level: `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `from` | string | _(none)_ | Start timestamp (ISO 8601) |
| `to` | string | _(none)_ | End timestamp (ISO 8601) |
| `search` | string | _(none)_ | Keyword search in log messages |
| `offset` | integer | `0` | Pagination offset |
| `limit` | integer | `300` | Page size (max `1000`) |

**Response Codes**

| Code | Meaning |
|------|---------|
| `200` | Bounded, redacted log entries returned |
| `401` | Unauthorized |
| `403` | Forbidden — administrator access required |
| `404` | Device not found or offline |
| `409` | A log request is already in progress for this device |
| `504` | Device did not return logs in time |

**200 Response Body**

```json
{
  "entries": [
    {
      "timestamp": "2026-04-02T10:15:30.123Z",
      "level": "INFO",
      "target": "mesh_agent::heartbeat",
      "message": "heartbeat sent"
    }
  ],
  "total": 42,
  "has_more": true
}
```

### Device Metrics

`GET /api/v1/devices/{id}/metrics` returns column-oriented numeric telemetry for
a device window, read tenant-scoped from VictoriaMetrics
([`handlers_device_metrics.go`](../server/internal/api/handlers_device_metrics.go)).

The time axis is derived from the request, not from what the store happens to
hold. `chooseStep` picks the smallest whole-second bucket that keeps the point
count within `max_points`, and
[`buildMetricGrid`](../server/internal/api/metrics_assemble.go) lays out exactly
`(to - from) / bucket_s` evenly spaced buckets from it. So the window selector
means something: a seven-day request over a device with twenty minutes of
telemetry returns seven days of buckets with one short run of values and `null`
everywhere else, rather than collapsing to the two points the store answered
with. `bucket_s` reports the width and the end instant is exclusive.

Both edges of the grid sit on a whole multiple of the step, and the range read
is issued at those two instants. That matters because VictoriaMetrics rounds an
unaligned start down to a step multiple once a query has enough points, so a
grid built independently of the query could disagree by one bucket and shift
every value. Issuing the read on the grid's own instants makes the rounding a
no-op; a sample that still arrives off the grid is counted on
`opengate_metrics_grid_misalignment_total` and logged, never dropped in silence.

The narrowest bucket is 60 s, the cadence a device writes its vitals on
([ADR-065](adr/ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md)); a
finer one would ask the store for detail it does not hold. The optional `band`
computes min/max alongside the avg line, and the response labels its provenance:
`avg_of_60s` is min/max across the 60 s averages in the bucket, never host
extrema. The dims a device writes are the fixed vitals vocabulary — each gauge's
average, plus a `.max` for the four where a within-minute spike is the signal.

Clients must render `null` as a gap and never interpolate across one — a
straight line over a device-offline window is a reading nobody took. The uPlot
adapter does this with `spanGaps: false` on every drawn series
([`aligned-data.ts`](../web/src/features/devices/charts/aligned-data.ts)).

**Response Codes**

| Code | Meaning |
|------|---------|
| `200` | Telemetry window (`t`, `series`, `downsampled`, `bucket_s`) |
| `400` | Invalid window (`to` not after `from`) |
| `401` | Unauthorized |
| `403` | Forbidden (the request carries no tenant scope) |
| `404` | Device not found (also the cross-tenant deny) |
| `503` | Telemetry not configured (no VictoriaMetrics URL) or the range query failed |

### Device Inventory

`GET /api/v1/devices/{id}/inventory` returns the device's current
auto-discovered footprint — listening ports, host services, database engines,
containers, and installed packages — as a flat list of items each carrying a
`kind` discriminator. The rows come from the tenant-scoped
[`device_inventory`](Database.md) RLS table, populated from the agent's
[`DiscoveryReport`](Wire-Protocol.md); each report replaces the device's
footprint. It is descriptive attack-surface data only (never a credential or
connection string) and is visible to every member of the tenant, not just
administrators.

**Response Codes**

| Code | Meaning |
|------|---------|
| `200` | The device's inventory (`device_id`, `items`) |
| `401` | Unauthorized |
| `404` | Device not found (also the cross-tenant deny — a device in another tenant is not visible) |
| `503` | Inventory not configured |

### Maintenance Mode

`POST /api/v1/devices/{id}/maintenance` toggles a device's maintenance state —
the server-authoritative desired state that quiets the agent's telemetry and
alerting during disruptive host work (see
[ADR-056](adr/ADR-056-device-maintenance-mode.md) and
[Monitoring](Monitoring.md#maintenance-mode)). The body carries the desired
`enabled` flag and an optional operator `reason`. It is a desired state, not a
live command, so it succeeds even when the agent is offline (no agent-connected
check), and every enter/exit is written to the audit log. Entry stamps
`maintenance_since`/`_by`/`_reason` on the `devices` row and pushes
`SetMaintenanceMode` to a connected agent; exit clears them and pushes the resume.

Toggling maintenance is a device command, open to every member of the device's
tenant. The count of devices currently in maintenance is one field of the
fleet summary below. The four maintenance fields
(`maintenance_on`/`_since`/`_by`/`_reason`) are present on the device DTO only
while a device is in maintenance. The canonical request/response shapes are in
[`api/openapi.yaml`](../api/openapi.yaml).

### Customers and the Fleet Filter

A tenant is the wall the database enforces; an organization is one customer
inside it, and every device belongs to exactly one. A technician sees every
customer in their tenant, so the reads that answer with a set of devices — the
device list and the fleet summary — accept an `organization_id` and narrow to it,
returning the whole tenant when none is given. The rule is enforced against the
specification rather than a hand-kept list: an operation whose 200 response is a
set of devices or a rollup over one must declare the parameter, so a fleet read
added later without it fails the suite.

Customer names are unique within a tenant, not globally. Retiring a customer
keeps its devices and its history and takes it out of the working set; deleting
one takes its devices with it, and a tenant's last customer cannot be deleted.

### Sites

A site is a location or department inside one customer, and it is the level
below the customer that a device is filed into. `PATCH /api/v1/devices/{id}`
files a device into one; the all-zeros UUID unfiles it. A site must belong to the
device's own customer — the request is refused otherwise, rather than storing a
machine under another customer's office — and moving a device to a different
customer unfiles it in the same operation. Deleting a site leaves its devices
with their customer, unfiled. Site names are unique within their customer, so two
customers may each have a "Head Office".

### Fleet Summary

`GET /api/v1/devices/summary` answers the dashboard with a fixed-size rollup of
the caller's tenant: `total`, `online`, `offline`, `maintenance`, and a
`health` object counting devices per edge-health band
(`anomalous`/`watch`/`healthy`/`unknown`). It costs one aggregate row in
Postgres and one instant query in VictoriaMetrics, so both the work and the
payload are the same for a fleet of one and a fleet of ten thousand — the
dashboard never downloads the device table to count it.

It is tenant-scoped for every caller, administrators included, so the
tiles and the health bands always describe one device set and `unknown` is
exact. With telemetry unconfigured or its query failing, the status counts are
still returned and every device lands in `unknown`; the endpoint does not fail.

### Intel AMT

Intel AMT is a property of a managed device, not a separate collection. A device
DTO carries an `amt` object when its hardware supports AMT or an AMT connection
is linked to it: `available` (the agent's Management Engine reading), plus
`status` and `uuid` once a CIRA connection has dialled in. Reading a device is
therefore all the UI needs — there is no AMT list to join against.

The join key is the host's SMBIOS system UUID, which the AMT firmware presents
as its CIRA identity. The server stores it on the hardware row to resolve which
device — and which tenant — a CIRA connection belongs to, and never
returns it in any response. AMT hardware attributes (`amt_available`,
`amt_version`, `amt_model`, `amt_firmware`) live on the
`GET /api/v1/devices/{id}/hardware` payload. See
[ADR-061](adr/ADR-061-amt-as-device-property.md).

## Rate Limiting

All API endpoints are subject to per-IP rate limiting:

| Scope | Rate | Burst |
|-------|------|-------|
| Global | 100 req/s | 200 |
| Auth (`/auth/login`, `/auth/register`) | 10 req/s | 20 |

Requests exceeding the limit receive `429 Too Many Requests`. A 30-second request timeout applies to all API routes (WebSocket routes are excluded).

## Authentication

Protected endpoints require a JWT bearer token in the `Authorization` header:

```
Authorization: Bearer <token>
```

Tokens are obtained via `/api/v1/auth/login` or `/api/v1/auth/register`. JWT claims include `uid` (user ID), `email`, `admin` (boolean), and `tenant` (active tenant ID). The server uses `tenant` to scope repository transactions and RLS policies.

## Error Format

All errors return a JSON object with an `error` field:

```json
{"error": "descriptive error message"}
```
