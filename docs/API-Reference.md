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
const { data, error } = await api.GET('/api/v1/groups');
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
| `/api/v1/groups` | POST | JWT | Create a device group |
| `/api/v1/groups` | GET | JWT | List groups |
| `/api/v1/groups/{id}` | GET | JWT | Get a group |
| `/api/v1/groups/{id}` | DELETE | JWT | Delete a group |
| `/api/v1/devices` | GET | JWT | List devices (optional `group_id` filter) |
| `/api/v1/devices/{id}` | GET | JWT | Get a device (includes `capabilities` array) |
| `/api/v1/devices/{id}` | PATCH | JWT | Update device (reassign `group_id`) |
| `/api/v1/devices/{id}` | DELETE | JWT | Delete a device and purge all its telemetry ([Data Lifecycle](Data-Lifecycle.md)) |
| `/api/v1/devices/{id}/restart` | POST | JWT | Restart agent on device (optional `reason` field) |
| `/api/v1/devices/{id}/hardware` | GET | JWT | Get hardware inventory for device (200 cached / 202 requested from agent) |
| `/api/v1/devices/{id}/logs` | GET | JWT | Get device log entries (on-demand via agent) |
| `/api/v1/devices/{id}/correlate` | POST | JWT | Rank anomalous metric dimensions for a window (on-demand, server-side over VictoriaMetrics) |
| `/api/v1/devices/{id}/inventory` | GET | JWT | Get the device's auto-discovered footprint (ports, services, DB engines, containers, packages) |
| `/api/v1/sessions` | POST | JWT | Create a remote session |
| `/api/v1/sessions` | GET | JWT | List sessions (requires `device_id` query param) |
| `/api/v1/sessions/{token}` | DELETE | JWT | Delete a session |
| `/api/v1/amt/devices` | GET | JWT | List Intel AMT devices |
| `/api/v1/amt/devices/{uuid}` | GET | JWT | Get AMT device details |
| `/api/v1/amt/devices/{uuid}/power` | POST | JWT | Send AMT power command (on/cycle/soft-off/hard-reset) |
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
| `/api/v1/orgs/{orgId}/purge` | POST | JWT (admin) | Purge a whole tenant's telemetry (async, tenant-scoped; [Data Lifecycle](Data-Lifecycle.md)) |
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

### Metric Correlation

`POST /api/v1/devices/{id}/correlate` ranks the metric dimensions that "broke
pattern" during an incident window. The engine
([`server/internal/correlate`](../server/internal/correlate)) fetches the
device's numeric telemetry from VictoriaMetrics through the tenant-scoped read
client ([`server/internal/telemetry/vm.go`](../server/internal/telemetry/vm.go),
which injects the `org_id` label matcher) and computes the ranking server-side
in Go — VictoriaMetrics' MetricsQL has no native KS test or join.

Each `{focus_start, focus_end}` window is compared against a baseline window
(the equal-length window immediately before focus, unless `baseline_start` /
`baseline_end` are given). Every dimension is scored by three signals — a
two-sample Kolmogorov–Smirnov distribution shift, the fraction of focus samples
outside the baseline band (anomaly rate), and the normalized mean shift
(magnitude/volume). Ranking is on-demand only (no background matrix) and bounded
by a concurrency limiter, a per-request timeout, and per-request caps on series
and points.

**Response Codes**

| Code | Meaning |
|------|---------|
| `200` | Ranked correlation result (`ranked`, `series_considered`, `series_truncated`) |
| `400` | Invalid window (e.g. `focus_end` not after `focus_start`) |
| `401` | Unauthorized |
| `403` | Forbidden (caller does not own the device's group) |
| `404` | Device not found (also the cross-tenant deny — a device in another org is not visible) |
| `503` | Correlation not configured (no VictoriaMetrics URL) or the engine is at capacity |

### Device Inventory

`GET /api/v1/devices/{id}/inventory` returns the device's current
auto-discovered footprint — listening ports, host services, database engines,
containers, and installed packages — as a flat list of items each carrying a
`kind` discriminator. The rows come from the tenant-scoped
[`device_inventory`](Database.md) RLS table, populated from the agent's
[`DiscoveryReport`](Wire-Protocol.md); each report replaces the device's
footprint. It is descriptive attack-surface data only (never a credential or
connection string) and is visible to any device viewer in the organization, not
just administrators.

**Response Codes**

| Code | Meaning |
|------|---------|
| `200` | The device's inventory (`device_id`, `items`) |
| `401` | Unauthorized |
| `403` | Forbidden (caller does not own the device's group) |
| `404` | Device not found (also the cross-tenant deny — a device in another org is not visible) |
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

`GET /api/v1/devices/maintenance-summary` returns the tenant's fleet count of
devices currently in maintenance, served from a partial index over the caller's
organization.

Both are group-owner authorized. The four maintenance fields
(`maintenance_on`/`_since`/`_by`/`_reason`) are present on the device DTO only
while a device is in maintenance. The canonical request/response shapes are in
[`api/openapi.yaml`](../api/openapi.yaml).

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

Tokens are obtained via `/api/v1/auth/login` or `/api/v1/auth/register`. JWT claims include `uid` (user ID), `email`, `admin` (boolean), and `org` (active organization ID). The server uses `org` to scope repository transactions and RLS policies.

## Error Format

All errors return a JSON object with an `error` field:

```json
{"error": "descriptive error message"}
```
