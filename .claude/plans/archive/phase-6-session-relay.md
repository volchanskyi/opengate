# Phase 6: Server Session & WebSocket Relay API

## Context

Phases 1-5 are complete. The server has QUIC agent connections, a relay engine, REST API, and auth. The agent has protocol types, identity, connection handling, and platform traits. The missing piece is **session orchestration** — the HTTP endpoints that let a browser request a session to a connected agent and WebSocket endpoints that wire both sides into the relay.

This maps to the plan doc's Phase 6 (`internal/api` expansion) and Phase 3 relay integration (`GET /ws/relay/:token`). We're scoping to the session/relay core — no signaling, notifications, or MPS yet.

## What Changes

### 1. Add `nhooyr.io/websocket` dependency

No WebSocket library exists in `go.mod`. The plan doc suggests `nhooyr.io/websocket` (better context support than gorilla). Add it.

**File:** `server/go.mod`

### 2. Inject `*agentapi.AgentServer` and `*relay.Relay` into API Server

Currently `api.NewServer(store, jwtCfg, logger)` has no access to connected agents or the relay. We need to add these dependencies.

**File:** `server/internal/api/api.go`
- Add `agentSrv *agentapi.AgentServer` and `relay *relay.Relay` fields to `Server` struct
- Update `NewServer` signature: `NewServer(store, jwtCfg, agentSrv, relay, logger)`
- Add new routes in `routes()`:
  - `POST /api/v1/sessions` (protected) — create session
  - `GET /api/v1/sessions` (protected) — list sessions for a device
  - `DELETE /api/v1/sessions/{token}` (protected) — terminate session
  - `GET /ws/relay/{token}` (public — token acts as auth) — WebSocket relay upgrade

### 3. Update `main.go` wiring

Pass `agentSrv` and `agentRelay` to `api.NewServer`.

**File:** `server/cmd/meshserver/main.go`

### 4. Update test helpers

`newTestServer` in `helpers_test.go` must pass the new params (nil agentSrv + a real relay for tests).

**File:** `server/internal/api/helpers_test.go`

### 5. New handler: `handlers_sessions.go`

**File:** `server/internal/api/handlers_sessions.go`

Three handlers:

#### `handleCreateSession` — `POST /api/v1/sessions`
- Request body: `{ "device_id": "uuid", "permissions": { "desktop": true, ... } }`
- Validates device_id, looks up agent via `s.agentSrv.GetAgent(deviceID)`
- Returns 404 if agent not connected
- Generates token via `protocol.GenerateSessionToken()`
- Stores session in DB via `s.store.CreateAgentSession()`
- Builds relay URL: `wss://{host}/ws/relay/{token}`
- Sends `SessionRequest` to agent via `agentConn.SendSessionRequest()`
- Returns `201 { "token": "...", "relay_url": "..." }`

#### `handleListSessions` — `GET /api/v1/sessions?device_id=uuid`
- Requires `device_id` query param
- Returns `s.store.ListActiveSessionsForDevice()`

#### `handleDeleteSession` — `DELETE /api/v1/sessions/{token}`
- Deletes from DB via `s.store.DeleteAgentSession()`
- Returns 204

### 6. New handler: `handlers_relay.go`

**File:** `server/internal/api/handlers_relay.go`

#### `handleRelayWebSocket` — `GET /ws/relay/{token}`
- Upgrades HTTP to WebSocket via `nhooyr.io/websocket`
- Extracts `{token}` from URL
- Validates token exists in DB via `s.store.GetAgentSession()`
- Determines side: if request has valid JWT → `SideBrowser`, else → `SideAgent`
- Wraps `*websocket.Conn` into an `io.ReadWriteCloser` adapter (for relay.Conn interface)
- Calls `s.relay.Register(ctx, token, wrappedConn, side)`
- Calls `s.relay.WaitForPeer(ctx, token)` — blocks until peer connects
- Blocks on context cancellation (relay handles piping)

### 7. WebSocket-to-relay adapter

**File:** `server/internal/api/wsconn.go`

A thin adapter wrapping `*websocket.Conn` into `relay.Conn` (io.ReadWriteCloser):
- `Read()` → calls `conn.Read()` returning binary message bytes
- `Write()` → calls `conn.Write()` sending binary message
- `Close()` → calls `conn.Close()`

### 8. Tests (TDD — write first)

#### Testability: `AgentLookup` interface

`TestCreateSession_Success` needs to simulate a connected agent without a real QUIC server. Introduce an interface in `api.go`:

```go
type AgentLookup interface {
    GetAgent(deviceID protocol.DeviceID) *agentapi.AgentConn
}
```

`*agentapi.AgentServer` already satisfies this. Tests inject a `stubAgentLookup` that returns a fake `AgentConn` backed by a `bytes.Buffer` stream — we can then inspect what was written to verify `SendSessionRequest` was called correctly.

#### Unit Tests: Session Handlers

**File:** `server/internal/api/session_handlers_test.go`

Table-driven with `t.Run`, following patterns in existing handler tests:

```
TestCreateSession/unauthenticated                → 401
TestCreateSession/invalid_json_body              → 400
TestCreateSession/invalid_device_id              → 400 (not a UUID)
TestCreateSession/device_not_in_db               → 404
TestCreateSession/agent_not_connected            → 409 Conflict (device in DB but offline)
TestCreateSession/success                        → 201 { token, relay_url }
  - token is 64 hex chars
  - relay_url contains /ws/relay/{token}
  - session retrievable from DB via store.GetAgentSession(token)
  - SessionRequest control frame written to agent stream (decode & verify fields)
TestCreateSession/default_permissions            → 201 (permissions omitted → all false)
TestCreateSession/custom_permissions             → 201 (desktop=true, terminal=true passed through)

TestListSessions/unauthenticated                 → 401
TestListSessions/missing_device_id               → 400
TestListSessions/invalid_device_id               → 400
TestListSessions/empty                           → 200 []
TestListSessions/returns_sessions                → 200 (seed 2 sessions via testutil.SeedAgentSession, verify both)
TestListSessions/only_returns_for_given_device   → 200 (seed sessions on 2 devices, verify filtering)

TestDeleteSession/unauthenticated                → 401
TestDeleteSession/not_found                      → 404
TestDeleteSession/success                        → 204, session gone from DB
TestDeleteSession/idempotent                     → 404 on second delete (not 500)
```

#### Unit Tests: WebSocket Adapter

**File:** `server/internal/api/wsconn_test.go`

```
TestWSConn_ReadWriteRoundtrip          → write bytes via adapter, read back via underlying conn
TestWSConn_CloseClosesUnderlying       → Close() propagates, subsequent Read returns error
TestWSConn_ConcurrentReadWrite         → parallel reads and writes with -race (no data race)
```

#### Unit Tests: Relay WebSocket Handler

**File:** `server/internal/api/relay_handler_test.go`

Uses `httptest.NewServer` (WebSocket needs real TCP, not httptest.ResponseRecorder):

```
TestRelayWebSocket/token_not_in_db              → WS upgrade succeeds, then close with "session not found"
TestRelayWebSocket/browser_connects_waits       → single side (browser with JWT), blocks waiting for peer
                                                   cancel context → clean shutdown
TestRelayWebSocket/both_sides_connect_data_flows → two WS clients connect with same token
                                                   (?side=browser with JWT, ?side=agent without)
                                                   send "hello" from agent side
                                                   verify browser side receives "hello"
                                                   send "world" from browser side
                                                   verify agent side receives "world"
TestRelayWebSocket/disconnect_closes_peer       → one side disconnects, other side gets closed within 1s
TestRelayWebSocket/invalid_side_param           → WS close with "invalid side" error
```

Side detection: `?side=browser` (requires valid JWT header) or `?side=agent` (no JWT needed, token is auth). This is simple, explicit, and avoids protocol-level ambiguity.

#### Integration Tests: Full Session Lifecycle

**File:** `server/tests/integration/session_test.go`

End-to-end tests with real QUIC agent + real HTTP server + real WebSocket relay. Extends the existing `agentTestEnv` pattern from `agentapi_test.go`.

New `sessionTestEnv` struct bundles:
- `testutil.NewTestStore(t)` — ephemeral SQLite
- `cert.NewManager(t.TempDir())` — fresh CA
- `relay.NewRelay()` — relay engine
- `agentapi.NewAgentServer(...)` — QUIC on `127.0.0.1:0`
- `api.NewServer(...)` — HTTP via `httptest.NewServer` (real TCP)
- `auth.JWTConfig` — for generating test JWTs

Helpers:
- `connectAgent(t, groupID)` — reuse existing QUIC handshake pattern
- `dialRelayWS(t, token, side, jwt)` — WebSocket client to `/ws/relay/{token}`
- `createSession(t, jwt, deviceID, perms)` — `POST /api/v1/sessions`
- `listSessions(t, jwt, deviceID)` — `GET /api/v1/sessions?device_id=...`
- `deleteSession(t, jwt, token)` — `DELETE /api/v1/sessions/{token}`

```
TestSessionLifecycle_CreateAndRelay
```
1. Start full test env
2. Connect QUIC agent, wait for registration (device online)
3. `POST /api/v1/sessions` → 201, get token + relay_url
4. Read `SessionRequest` from QUIC control stream, verify token & permissions match
5. Agent sends `SessionAccept` back
6. Browser opens WebSocket to relay URL (`?side=browser` + JWT)
7. Agent opens WebSocket to relay URL (`?side=agent`)
8. Agent WS sends test payload → browser WS receives it
9. Browser WS sends test payload → agent WS receives it
10. `DELETE /api/v1/sessions/{token}` → 204

```
TestSessionLifecycle_AgentRejectsSession
```
1. Connect agent, create session
2. Agent reads `SessionRequest`, sends `SessionReject { reason: "busy" }`
3. Server logs rejection (verify via test logger or DB state)
4. Relay has no active session for this token

```
TestSessionLifecycle_AgentDisconnectDuringSession
```
1. Create session, both sides connected to relay, data flowing
2. Close QUIC agent stream
3. Verify device goes offline in DB (Eventually, 5s)
4. Verify relay active session count drops to 0
5. Browser WebSocket receives close/EOF

```
TestSessionLifecycle_MultipleSessionsSameDevice
```
1. Connect agent, create 2 sessions
2. `GET /api/v1/sessions?device_id=...` → 2 sessions
3. Delete one → 1 session remains
4. Delete other → 0 sessions

```
TestSessionLifecycle_ConcurrentSessions
```
1. Connect 3 agents
2. Create sessions for all 3 simultaneously (3 goroutines)
3. All 3 relay pairs exchange data concurrently
4. All data arrives intact and in order per session
5. Must pass with `-race`

## Implementation Order (TDD)

1. `go get nhooyr.io/websocket` — add dependency
2. Define `AgentLookup` interface in `api.go`, update `Server` struct and `NewServer`
3. Update `helpers_test.go` — `newTestServer` passes stub lookup + real relay
4. Write `session_handlers_test.go` — all session endpoint tests (will fail to compile)
5. Implement `handlers_sessions.go` — make session tests pass
6. Write `wsconn_test.go` then implement `wsconn.go` — WebSocket adapter
7. Write `relay_handler_test.go` — relay handler tests (will fail)
8. Implement `handlers_relay.go` — make relay tests pass
9. Update `main.go` — wire everything together
10. Write `session_test.go` integration tests
11. Run full test suite: `cd server && go test -race -timeout 5m ./...`
12. Run benchmarks: `cd server && go test -bench=. -benchmem -run='^$' ./internal/...`
13. Run Rust + Web tests to verify nothing broke

## Files Modified

| File | Action |
|------|--------|
| `server/go.mod` | Add `nhooyr.io/websocket` |
| `server/internal/api/api.go` | Add AgentLookup + relay fields, update NewServer, add routes |
| `server/internal/api/helpers_test.go` | Update newTestServer with new params, add stubAgentLookup |
| `server/internal/api/handlers_sessions.go` | **New** — session CRUD handlers |
| `server/internal/api/handlers_relay.go` | **New** — WebSocket relay upgrade handler |
| `server/internal/api/wsconn.go` | **New** — WebSocket-to-io.ReadWriteCloser adapter |
| `server/internal/api/session_handlers_test.go` | **New** — 15 unit tests for session endpoints |
| `server/internal/api/relay_handler_test.go` | **New** — 5 unit tests for relay WebSocket handler |
| `server/internal/api/wsconn_test.go` | **New** — 3 unit tests for WS adapter |
| `server/tests/integration/session_test.go` | **New** — 5 integration tests for full lifecycle |
| `server/cmd/meshserver/main.go` | Pass agentSrv + relay to NewServer |

## Verification

1. `cd server && go test -race -timeout 5m ./...` — all tests pass (unit + integration)
2. `cd server && go test -bench=. -benchmem -run='^$' ./internal/...` — benchmarks run clean
3. `cd agent && cargo test --workspace` — Rust tests unaffected
4. `cd web && npx vitest run` — Web tests unaffected
5. Verify coverage: `go test -coverprofile=cover.out ./internal/api/... && go tool cover -func=cover.out` — session handlers ≥80%
