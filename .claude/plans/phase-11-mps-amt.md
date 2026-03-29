# Phase 11: Intel AMT Management Presence Server (MPS)

## Context
OpenGate needs an MPS (Management Presence Server) to accept TLS connections from Intel AMT devices on TCP 4433. Per the architecture doc (S2.1, S2.3, S5.1, S5.2) and TDD plan (Phase 2.2 cert tests, Phase 6 package structure), the scope for this phase is the **MPS infrastructure**: TLS listener, RSA 2048 cert, connection accept/tracking, and HTTP API for AMT device management.

The CIRA protocol (Intel AMT Port Forwarding Protocol / APF) has been thoroughly researched from MeshCentral and device-management-toolkit implementations. Full protocol reference saved at `memory/cira-protocol-reference.md`. Per user decision, **full CIRA protocol implementation is deferred to Phase 11b** — this phase builds the MPS infrastructure (cert, DB, TLS listener, basic APF framing, API, web UI). Phase 11b will add the complete APF message handling, channel multiplexing, and WSMAN tunneling.

**Pre-requisite**: Phase 10 has a minor gap — `ServerSettings.tsx` was not implemented. Add it before starting Phase 11.

## Memory Correction
Memory incorrectly states `cert.NewMPSTLSConfig()` and `TestMPSTLSConfigAllowsTLS10` already exist. They do NOT — must be built in Part A.

---

## Part 0: Phase 10 Gap — ServerSettings.tsx

Add the missing admin settings page before starting Phase 11.

### Files to create
- `web/src/features/admin/ServerSettings.tsx` — Display VAPID public key (copyable), connected agent count, server version
- `web/src/features/admin/ServerSettings.test.tsx`

### Files to modify
- [AdminLayout.tsx](web/src/features/admin/AdminLayout.tsx) — Add nav item `{ to: '/admin/settings', label: 'Settings' }`
- [router.tsx](web/src/router.tsx) — Add route `{ path: 'settings', element: <ServerSettings /> }` under admin children

**~3 tests**

---

## Part A: Cert Expansion — RSA 2048 MPS Certificate + TLS Config

**Why first**: Everything depends on MPS having a valid TLS cert. Explicitly specified in TDD plan Phase 2.2.

### Files to modify
- [cert.go](server/internal/cert/cert.go) — Add two methods to `Manager`:
  - `SignMPS(hostnames []string) (*tls.Certificate, error)` — RSA 2048 server cert signed by ECDSA P-256 CA, CN="OpenGate MPS", `ExtKeyUsageServerAuth`, 1-year validity, hostnames as SANs. Uses `rsa.GenerateKey(rand.Reader, 2048)` + `x509.MarshalPKCS1PrivateKey()` (differs from ECDSA `SignServer()`). Needs `"crypto/rsa"` import.
  - `MPSTLSConfig(hostnames []string) (*tls.Config, error)` — `MinVersion: tls.VersionTLS10` (NOT 1.3), `ClientAuth: tls.NoClientCert` (AMT uses digest auth, not mTLS), RSA cert from `SignMPS`.
- [cert_test.go](server/internal/cert/cert_test.go) — Table-driven tests (from TDD plan):
  - `TestSignMPS/signs_valid_RSA_2048_cert` — verify `*rsa.PublicKey`, 2048-bit modulus, correct CN/SANs, ExtKeyUsageServerAuth
  - `TestSignMPS/cert_verifies_under_CA` — pool-based verification
  - `TestMPSTLSConfig/min_version_TLS10` — assert `cfg.MinVersion == tls.VersionTLS10` (arch doc S5.2)
  - `TestMPSTLSConfig/no_client_auth_required` — assert `cfg.ClientAuth == tls.NoClientCert`
  - `TestMPSTLSConfig/has_RSA_certificate` — assert leaf cert public key is `*rsa.PublicKey`

**~6 tests**

---

## Part B: Database — AMT Device + CIRA Session Tables

**Why second**: MPS server needs persistence. Isolated migration, no runtime impact.

### Files to create
- `server/internal/db/migrations/002_amt_devices.up.sql`:
  ```sql
  CREATE TABLE IF NOT EXISTS amt_devices (
      guid TEXT PRIMARY KEY,
      hostname TEXT NOT NULL DEFAULT '',
      firmware_version TEXT NOT NULL DEFAULT '',
      sku TEXT NOT NULL DEFAULT '',
      status TEXT NOT NULL DEFAULT 'disconnected',
      power_state TEXT NOT NULL DEFAULT 'unknown',
      features TEXT NOT NULL DEFAULT '',
      last_seen TEXT NOT NULL DEFAULT (datetime('now')),
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      updated_at TEXT NOT NULL DEFAULT (datetime('now'))
  );
  CREATE TABLE IF NOT EXISTS cira_sessions (
      id TEXT PRIMARY KEY,
      amt_guid TEXT NOT NULL REFERENCES amt_devices(guid) ON DELETE CASCADE,
      remote_addr TEXT NOT NULL DEFAULT '',
      started_at TEXT NOT NULL DEFAULT (datetime('now')),
      ended_at TEXT
  );
  CREATE INDEX IF NOT EXISTS idx_amt_devices_status ON amt_devices(status);
  CREATE INDEX IF NOT EXISTS idx_cira_sessions_amt_guid ON cira_sessions(amt_guid);
  ```
- `server/internal/db/migrations/002_amt_devices.down.sql`

### Files to modify
- [models.go](server/internal/db/models.go) — Add types:
  - `AMTDeviceGUID = string`, `AMTDeviceStatus string` (Connected/Disconnected), `AMTPowerState string` (On/Off/Sleep/Hibernate/Unknown)
  - `AMTDevice` struct, `CIRASession` struct
- [store.go](server/internal/db/store.go) — Add Store interface methods:
  - `UpsertAMTDevice`, `GetAMTDevice`, `ListAMTDevices`, `SetAMTPowerState`, `SetAMTDeviceStatus`, `DeleteAMTDevice`
  - `CreateCIRASession`, `EndCIRASession`, `GetActiveCIRASession`
- [sqlite.go](server/internal/db/sqlite.go) — Implement all 9 methods following existing `scan*From` pattern
- [sqlite_test.go](server/internal/db/sqlite_test.go) — Table-driven CRUD tests + cascade delete

**~15 tests**

---

## Part C: MPS Server — TLS Listener + Connection Tracking

**Why third**: Depends on Parts A (cert) and B (DB). Mirrors `agentapi.AgentServer` pattern.

**Scope**: TLS listener accepting connections, tracking connected devices in `sync.Map`, basic CIRA message type constants and framing. Full CIRA protocol handling (channel multiplexing, digest auth, WSMAN XML) is deferred — the docs don't specify it and it needs real AMT hardware for testing.

### Files to create (in `server/internal/mps/`)

1. **`mps.go`** — Replace stub. Core server:
   - `AMTDeviceGetter` interface: `GetAMTConnection(guid string) *AMTConn`
   - `Server` struct: cert Manager, store, notifier, `sync.Map` conns, `atomic.Int64` count, logger, addrCh/addrOnce (same pattern as [server.go:25-35](server/internal/agentapi/server.go#L25-L35))
   - `NewServer(cm, store, notifier, hostnames, logger)` constructor
   - `ListenAndServe(ctx, addr)` — calls `cm.MPSTLSConfig()`, `tls.Listen("tcp", addr, cfg)`, accept loop (mirror [server.go:69-119](server/internal/agentapi/server.go#L69-L119) but TCP TLS instead of QUIC)
   - `Addr() string`, `GetAMTConnection(guid)`, `ConnectedDeviceCount()`
   - `accept(ctx, conn)` — read initial CIRA message to extract device GUID, register in conns map + upsert DB, run keepalive loop, deregister + notify on disconnect

2. **`conn.go`** — AMT connection:
   - `AMTConn` struct: GUID, Hostname, FWVersion, net.Conn, store, logger, sessionID, mu sync.Mutex
   - `Close() error` — ends CIRA session in DB, closes conn
   - `PowerAction(ctx, action) error` — returns `ErrNotImplemented` for now (WSMAN deferred)

3. **`apf.go`** — APF protocol constants and basic message framing (from `memory/cira-protocol-reference.md`):
   - All 21 APF message type constants with correct byte values (0-211)
   - `APFMessage` struct: Type byte, Data []byte
   - Helper functions: `readUint32(r io.Reader)`, `readString(r io.Reader)`, `writeUint32(w, val)`, `writeString(w, s)`
   - `readAPFMessage(r io.Reader) (byte, []byte, error)` — reads first byte (type), dispatches to type-specific parser
   - `parseProtocolVersion(data) (guid string, major, minor uint32)` — extracts device GUID from 93-byte message
   - `ErrNotImplemented` — sentinel for unhandled operations
   - Service name constants: `ServicePFwd = "pfwd@amt.intel.com"`, `ServiceAuth = "auth@amt.intel.com"`
   - Channel type constants: `ChanDirectTCPIP = "direct-tcpip"`, `ChanForwardedTCPIP = "forwarded-tcpip"`

4. **`noop.go`** — `NoopMPS` implementing `AMTDeviceGetter` (returns nil), for test injection

### Test files
- `mps_test.go`:
  - `TestNewServer` — constructor, initial state
  - `TestListenAndServe/accepts_TLS_connections` — dial with TLS 1.0 client, verify accept
  - `TestListenAndServe/context_cancellation_stops_listener`
  - `TestGetAMTConnection/returns_nil_for_unknown`
  - `TestConnectedDeviceCount/increments_and_decrements`
- `cira_test.go`:
  - `TestReadMessage/valid_message` — encode known bytes, decode, verify
  - `TestReadMessage/truncated_input` — returns error, not panic
  - `TestWriteMessage/roundtrip` — write then read
  - `TestMessageTypeConstants` — verify byte values match CIRA spec

**~12 tests**

---

## Part D: Notification Events + API Endpoints

### Files to modify
- [notifier.go](server/internal/notifications/notifier.go) — Add EventTypes:
  - `EventAMTDeviceDiscovered`, `EventAMTDeviceDisconnected`
  - Extend `EventToPayload` switch

- [openapi.yaml](api/openapi.yaml) — Add schemas + 5 endpoints:
  - `GET /api/v1/amt/devices` — list AMT devices
  - `GET /api/v1/amt/devices/{guid}` — get single device
  - `DELETE /api/v1/amt/devices/{guid}` — remove device
  - `POST /api/v1/amt/devices/{guid}/power` — power action (returns 501 until CIRA channels implemented)
  - `GET /api/v1/amt/sessions` — list CIRA sessions (optional `?amt_guid` filter)

- Run codegen: `oapi-codegen` to regenerate [openapi_gen.go](server/internal/api/openapi_gen.go)

- [api.go](server/internal/api/api.go) — Add `mps AMTDeviceGetter` field to `Server`, update `NewServer` signature (add `mps` param after `notifier`)

### Files to create
- `server/internal/api/handlers_amt.go` — 5 handler methods:
  - `ListAMTDevices` -> `store.ListAMTDevices(ctx)`
  - `GetAMTDevice` -> `store.GetAMTDevice(ctx, guid)`, 404 on ErrNotFound
  - `DeleteAMTDevice` -> `store.DeleteAMTDevice(ctx, guid)` + audit log
  - `AMTPowerAction` -> `mps.GetAMTConnection(guid)`, 409 if nil (not connected), attempts `conn.PowerAction()`, returns 501 if ErrNotImplemented
  - `ListCIRASessions` -> store query
- `server/internal/api/amt_handlers_test.go` — tests for all 5 handlers

### Files to update
- All test helpers calling `api.NewServer(...)` — add `NoopMPS{}` param
- [helpers_test.go](server/internal/api/helpers_test.go) — update `newTestServer`

**~10 tests**

---

## Part E: Main.go Wiring + Integration Tests

### Files to modify
- [main.go](server/cmd/meshserver/main.go):
  - Add flags: `--mps-listen :4433`, `--mps-hostnames localhost`
  - Create `mps.NewServer(certMgr, store, notifier, hostnames, logger)`
  - Pass MPS to `api.NewServer(..., mpsServer, ...)` as AMTDeviceGetter
  - Launch `go mpsServer.ListenAndServe(ctx, *mpsListen)`
  - MPS shutdown handled by context cancellation (same as QUIC)

### Files to create
- `server/tests/integration/mps_test.go`:
  - `TestMPS_TLSHandshake` — start MPS, dial with TLS 1.0 client, verify handshake succeeds
  - `TestMPS_TLS13Client` — verify TLS 1.3 also works (MPS allows 1.0+)
  - `TestMPS_CertIsRSA` — verify RSA 2048 cert via TLS ConnectionState
  - `TestMPS_GracefulShutdown` — cancel context, verify listener closes

### Files to modify
- [testutil.go](server/internal/testutil/testutil.go) — Add `SeedAMTDevice`, `SeedCIRASession` helpers

**~6 tests**

---

## Part F: Web Client — AMT Device List + Power Control Panel

### Files to create
- `web/src/state/amt-store.ts` — Zustand store (mirrors [admin-store.ts](web/src/state/admin-store.ts)):
  - `fetchDevices`, `fetchDevice`, `deleteDevice`, `powerAction`, `fetchSessions`
- `web/src/state/amt-store.test.ts`
- `web/src/features/admin/AMTDeviceList.tsx` — Table: GUID, hostname, firmware, status, power state, last seen. Power button disabled with "Coming soon" tooltip (CIRA not yet implemented).
- `web/src/features/admin/AMTDeviceList.test.tsx`

### Files to modify
- [AdminLayout.tsx](web/src/features/admin/AdminLayout.tsx) — Add nav item `{ to: '/admin/amt', label: 'AMT Devices' }`
- [router.tsx](web/src/router.tsx) — Add route `{ path: 'amt', element: <AMTDeviceList /> }` under admin children
- Regenerate TS types from updated OpenAPI spec

**~6 tests**

---

## Dependency Graph
```
Part 0 (ServerSettings gap) ── first, standalone

Part A (cert RSA) ───┐
                      ├── Part C (MPS server) ──┐
Part B (DB models) ──┘                           ├── Part E (main.go wiring)
                                                  │
Part D (API + notifications) ────────────────────┘
                                                  │
Part F (web UI) ─────────────────────────────────┘
```
Parts A and B are parallel. C depends on both. D can start after B. E wires everything. F is last.

## Total: ~58 new tests

## What's Deferred (Future Phase 11b: Full CIRA/APF Protocol)
Full protocol reference: `memory/cira-protocol-reference.md`
- **APF message dispatch loop**: handle all 21 message types in `accept()` goroutine
- **Auth flow**: SERVICE_REQUEST → SERVICE_ACCEPT → USERAUTH_REQUEST → USERAUTH_SUCCESS
- **Port binding**: GLOBAL_REQUEST "tcpip-forward" for ports 16992/16993/5900
- **Channel multiplexing**: CHANNEL_OPEN/CONFIRM/DATA/CLOSE with flow control (window credits)
- **Keep-alive**: KEEPALIVE_REQUEST/REPLY with cookie echo, KEEPALIVE_OPTIONS negotiation
- **WSMAN tunneling**: Open channel to port 16992, send HTTP+WSMAN XML, parse response
- **Power control**: `CIM_PowerManagementService.RequestPowerStateChange` via WSMAN
- **GUID extraction**: Parse PROTOCOLVERSION message (bytes 13-28, Intel LE byte reorder)
- **Device info**: Hostname from TLS cert CN or reverse DNS, firmware from WSMAN query

## Verification
1. `make test` — all unit tests pass
2. `make lint` — clippy + go vet + eslint + actionlint
3. `make test-integration` — MPS TLS handshake integration tests
4. `make golden` — existing golden files still pass (no protocol changes)
5. Manual: start server with `--mps-listen :4433`, verify TLS 1.0 handshake with `openssl s_client -connect localhost:4433 -tls1`
6. Web: navigate to `/admin/amt`, verify empty device list renders; `/admin/settings` shows VAPID key
