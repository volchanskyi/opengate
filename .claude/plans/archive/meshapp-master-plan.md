# MeshApp (opengate) — Implementation Plan

## Context

Building a remote device management platform from scratch in an empty repo. The system has three components: a **Rust agent** (runs on managed devices), a **Go server** (central hub), and a **React/TypeScript web client** (browser UI). Communication uses QUIC (primary) with WebSocket fallback, and WebRTC for P2P sessions.

Development follows strict TDD: write failing tests → implement → pass → refactor. Each phase is independently buildable and verifiable.

---

## Dependency Graph

```
Phase 0  (Scaffolding)
   │
   ├──► Phase 1  (Protocol) ───────┬────────┐
   │       │                        │        │
   │       ▼                        ▼        │
   │    Phase 2 (DB, Certs)    Phase 4       │
   │       │                  (Platform)     │
   │       ▼                        │        │
   │    Phase 3 (Transport)         │        │
   │       │                        ▼        │
   │       ▼                   Phase 5       │
   │    Phase 6 (Server API)  (Agent Core)   │
   │       │                                 │
   │       ▼                                 │
   │    Phase 7 (Web Foundation) ◄───────────┘
   │       │
   │       ▼
   │    Phase 8 (Web Features)
   │       │
   │       ▼
   │    Phase 9 (Admin, Notifications)
   │       │
   └───────┴──► Phase 10 (E2E Integration)
```

**Parallelism**: Phase 4 runs alongside Phases 2–3. Phase 5 and Phase 6 can run in parallel.

---

## PHASE 0: Project Scaffolding

**Goal**: All three build systems compile successfully on an empty project.

### Files to create

**Root:**
- `.gitignore` — target/, node_modules/, dist/, *.db, .env
- `Makefile` — targets: build, test, test-short, lint, fmt, golden, ci, e2e, clean
- `CLAUDE.md` — project conventions, TDD mandate, forbidden patterns
- `README.md` — project overview

**Rust workspace** (`agent/`):
- `agent/Cargo.toml` — virtual workspace, resolver="2", members=["crates/*"], shared [workspace.dependencies]
- `agent/rust-toolchain.toml` — pin stable
- `agent/crates/mesh-protocol/Cargo.toml` + `src/lib.rs` — stub
- `agent/crates/mesh-agent-core/Cargo.toml` + `src/lib.rs` — stub
- `agent/crates/platform-linux/Cargo.toml` + `src/lib.rs` — stub
- `agent/crates/platform-windows/Cargo.toml` + `src/lib.rs` — stub

**Go module** (`server/`):
- `server/go.mod` — module github.com/volchanskyi/opengate/server
- `server/cmd/meshserver/main.go` — minimal main
- Stub packages: `internal/{protocol,cert,db,relay,agentapi,clientapi,signaling,notifications,mps,multiserver,api}` — each with a package declaration file

**Web** (`web/`):
- Scaffold via Vite (react-ts template), then configure:
- `package.json` — React 19, Vite, TypeScript strict, Vitest, RTL, Tailwind, Zustand
- `tsconfig.json` — strict: true, noUncheckedIndexedAccess: true
- `vitest.config.ts`, `tailwind.config.ts`
- `src/main.tsx`, `src/App.tsx`, `src/App.test.tsx` (smoke test)

**CI:**
- `.github/workflows/ci.yml` — matrix: rust (clippy + test), go (vet + test), web (lint + test + build)

### Verification
```bash
make build   # cargo build, go build ./..., npm run build — all succeed
make test    # cargo test, go test ./..., npm test — trivial tests pass
make lint    # clippy, go vet, eslint — clean
```

---

## PHASE 1: Shared Protocol Foundation

**Goal**: Define the binary wire protocol in Rust and Go. Verify cross-language compatibility with golden files.

### 1.1 Rust `mesh-protocol` — Types (TDD)

**Tests first** in `agent/crates/mesh-protocol/src/tests/`:
- `test_control_message_roundtrip_msgpack` — all ControlMessage variants
- `test_desktop_frame_roundtrip` — encode → decode → verify match
- `test_frame_type_byte_prefix` — Control=0x01, Desktop=0x02, Terminal=0x03, File=0x04, Ping=0x05, Pong=0x06
- `test_handshake_binary_encoding` — ServerHello=81 bytes, no padding
- `test_session_token_is_32_byte_hex` — 64 hex chars
- `test_device_id_stable_across_serialization`
- `proptest: test_codec_never_panics_on_arbitrary_bytes` (10,000 iterations)
- `proptest: test_control_message_encode_decode_identity`

**Implementation files:**
- `src/types.rs` — DeviceId, SessionToken, GroupId, AgentCapability, DeviceStatus, HandshakeMessage, Permissions. All `#[non_exhaustive]`, derive Serialize/Deserialize/Debug/Clone/PartialEq.
- `src/codec.rs` — Frame enum, encode/decode, wire format: [1-byte type][4-byte BE length][payload]
- `src/control.rs` — ControlMessage enum with all agent↔server variants
- `src/error.rs` — ProtocolError via thiserror

**Key deps**: serde, rmp-serde, bytes, thiserror, uuid, chrono, proptest

### 1.2 Go `internal/protocol` — Mirror Types (TDD)

**Tests first** in `server/internal/protocol/protocol_test.go`:
- `TestControlMessageRoundtrip` — table-driven, every variant
- `TestFrameTypeByteValues` — must match Rust constants
- `TestReadWriteFrameLengthPrefix` — 0-byte, 1-byte, 64KB, 1MB payloads
- `TestHandshakeMessageBinaryLayout` — byte-for-byte match with Rust
- `TestSessionTokenGeneration`
- `TestCrossLanguageCompatibility` — golden file test (reads Rust-generated files)
- `FuzzDecode` — 30s fuzz with no panics

**Implementation files:**
- `types.go` — Go structs mirroring Rust, msgpack tags
- `codec.go` — Encode/Decode, Codec struct, ReadFrame/WriteFrame
- `state.go` — connection state machine

### 1.3 Golden File Cross-Language Tests

- Rust integration test generates golden files to `testdata/golden/`
- Go test reads same files and verifies field-by-field
- Makefile target: `make golden`

### Verification
```bash
cd agent && cargo test -p mesh-protocol      # all pass including proptest
cd agent && cargo clippy -p mesh-protocol -- -D warnings  # zero warnings
cd server && go test ./internal/protocol/... # all pass
make golden                                   # cross-language compat verified
```

---

## PHASE 2: Server Infrastructure

**Goal**: Database abstraction (SQLite + PostgreSQL) and certificate management.

### 2.1 Go `internal/db` — Store Interface + SQLite (TDD)

**Tests first** in `server/internal/db/store_test.go` — shared test suite run against both backends:
- `testDeviceCRUD`, `testDeviceStatusTransitions`
- `testGroupCRUD`, `testUserCRUD`, `testUserEmailLookup`
- `testAgentSessionLifecycle`, `testWebPushSubscriptions`
- `testAuditLogWriteQuery`, `testConcurrentDeviceUpsert` (100 goroutines, -race)
- `testListDevicesEmptyGroup`, `testDeleteDeviceCascadesToSessions`

**Implementation files:**
- `store.go` — Store interface (all methods take context.Context first)
- `models.go` — Device, Session, User, Group, AuditEvent structs
- `sqlite.go` — SQLite impl using `modernc.org/sqlite`, WAL mode
- `postgres.go` — PostgreSQL impl using `pgx/v5`
- `migrations/001_initial.{up,down}.sql` — schema
- `migrate.go` — migration runner via `golang-migrate`

### 2.2 Go `internal/cert` — Certificate Management (TDD)

**Tests first** in `server/internal/cert/cert_test.go`:
- `TestRootCAGenerationDeterminism`, `TestSignedCertIsVerifiableByRootCA`
- `TestAgentServerCertHashIs48Bytes`, `TestHTTPSTLSConfigEnforcesTLS13`
- `TestMPSTLSConfigAllowsTLS10`, `TestVerifyAgentCertExtractsDeviceID`

**Implementation files:**
- `ca.go` — CertAuthority, ECDSA P-384, GetOrCreateRootCA, Sign*Cert
- `tls.go` — TLS config builders (HTTPS=TLS1.3, MPS=TLS1.0+, QUIC=TLS1.3)
- `acme.go` — Let's Encrypt via `autocert`
- `store.go` — encrypted-at-rest key storage

### Verification
```bash
cd server && go test -race ./internal/db/...   # both backends pass
cd server && go test ./internal/cert/...       # all pass
```

---

## PHASE 3: Transport Layer

**Goal**: QUIC server for agents; WebSocket relay for browser↔agent piping.

### 3.1 Go `internal/relay` — WebSocket Relay (TDD)

**Tests first** in `server/internal/relay/relay_test.go`:
- `TestBasicPipeRelaysBytes`, `TestRelayIsSymmetric`
- `TestSessionTokenMustMatchBothSides`, `TestRelayClosesWhenOneSideDisconnects`
- `TestBackpressureSlowConsumer`, `TestActiveSessionCount`
- `TestRelayConcurrency` (100 sessions, 1000 msgs each, -race)
- `TestRelayDoesNotLeakGoroutines`

**Implementation files:**
- `relay.go` — Relay struct, session registry, Register/WaitForPeer
- `websocket.go` — WS handler via `nhooyr.io/websocket`
- `pipe.go` — bidirectional io.Copy with 32KB buffer, backpressure

### 3.2 Go `internal/agentapi` — QUIC Agent Server (TDD)

**Tests first** in `server/internal/agentapi/agentapi_test.go`:
- `TestHandshakeFullExchange`, `TestHandshakeSkipAuthWithValidCachedHash`
- `TestHandshakeSkipAuthWithWrongCachedHash`, `TestHandshakeManInTheMiddleDetection`
- `TestHandshakeTimeout` (10s), `TestHandshakeConcurrency` (50 agents, -race)
- `TestAgentRegistersCapabilities`, `TestAgentHeartbeatUpdatesStatus`
- `TestServerSendsSessionRequest`

**Implementation files:**
- `server.go` — AgentServer, QUIC listener via `quic-go`
- `handler.go` — handshake handler, message router
- `session.go` — per-agent QUIC connection state

### Verification
```bash
cd server && go test -race ./internal/relay/...    # all pass
cd server && go test -race ./internal/agentapi/... # all pass
```

---

## PHASE 4: Platform-Specific Agent Code (parallel with Phases 2–3)

**Goal**: OS-specific screen capture, input injection, service lifecycle behind traits.

### 4.1 Trait Definitions in `mesh-agent-core`

- `src/platform.rs` — `ScreenCapture`, `InputInjector`, `ServiceLifecycle` traits
- `src/types.rs` — Frame (RawFrame), KeyEvent, MouseEvent, ServiceStatus

### 4.2 Linux Platform (`platform-linux`) — TDD

**Tests first:**
- `test_detect_container_via_dockerenv`, `test_detect_bare_metal_systemd`
- `test_null_capture_returns_consistent_error`, `test_null_input_is_available_returns_false`
- `test_filesystem_root_container_with_host_mount`, `test_filesystem_root_bare_metal`
- `test_systemd_unit_file_generation` (pure logic)
- Integration (ignored by default): `test_x11_capture_first_frame_is_valid_resolution`

**Implementation:** `capture.rs`, `input.rs`, `service.rs` — X11/Wayland capture, uinput/XTest injection, systemd lifecycle

### 4.3 Windows Platform (`platform-windows`) — TDD

**Tests first** (all `#[cfg(target_os = "windows")]`):
- `test_dxgi_capture_initializes`, `test_first_frame_has_correct_dimensions`
- `test_win32_input_available`, `test_windows_service_lifecycle_notifies_ready`

**Implementation:** DXGI Desktop Duplication, Win32 SendInput, Windows Service API

### Verification
```bash
cd agent && cargo test -p platform-linux   # unit tests pass
cd agent && cargo test -p platform-windows # unit tests pass (on Windows)
cd agent && cargo clippy --workspace -- -D warnings
```

---

## PHASE 5: Agent Core

**Goal**: Connection management, identity persistence, session handling, reconnect logic. Produces the agent binary.

### 5.1 Identity Persistence (TDD)

**Tests:** `test_generate_new_identity`, `test_load_existing_identity`, `test_identity_cert_is_self_signed`

**Impl:** `identity.rs` — AgentIdentity, ed25519 keypair, device ID, file persistence

### 5.2 Connection Manager (TDD)

**Tests:** `test_reconnect_uses_exponential_backoff`, `test_reconnect_after_server_restart`, `test_connection_state_transitions`

**Impl:** `connection.rs` — QUIC/WS connect, handshake, background heartbeat, exponential backoff (1s→60s cap)

### 5.3 Session Handler (TDD)

**Tests:** `test_relay_session_pipes_desktop_frames`, `test_webrtc_upgrade_attempt`, `test_permissions_respected`

**Impl:** `session.rs` — SessionManager, relay connect, WebRTC upgrade, permission enforcement

### 5.4 Agent Binary

- `agent/crates/mesh-agent/Cargo.toml` — binary crate, depends on core + platform (cfg-gated)
- `agent/crates/mesh-agent/src/main.rs` — clap CLI, config loading, Agent::run()

### Verification
```bash
cd agent && cargo test -p mesh-agent-core  # all pass
cd agent && cargo build -p mesh-agent      # binary compiles
```

---

## PHASE 6: Server Complete API Layer (parallel with Phase 5)

**Goal**: REST API, WebSocket upgrades, auth, SPA serving, signaling, notifications.

### 6.1 Auth Middleware (TDD)

**Tests:** `TestLoginSuccess`, `TestLoginWrongPassword`, `TestProtectedRouteRequiresAuth`, `TestProtectedRouteWithValidToken`, `TestAdminRouteBlocksNonAdmin`, `TestRateLimitingOnLoginEndpoint`

**Impl:** `api/auth.go` — JWT, `api/middleware.go` — chain (logging, CORS, auth, rate limit)

### 6.2 REST Endpoints + SPA Serving (TDD)

**Tests:** `TestGetDevices`, `TestGetDeviceNotFound`, `TestSPAFallbackServesIndexHTML`, `TestStaticAssetsServedWithCaching`, `TestCSPHeaderPresent`, `TestHealthEndpointReturns200`

**Impl:** `api/server.go`, `api/handlers.go`, `api/spa.go`

**Routes:** POST /api/auth/{login,logout}, GET /api/auth/me, GET/DELETE /api/devices[/:id], GET/POST/DELETE /api/groups[/:id], GET/POST /api/users (admin), GET /api/audit, POST /api/push/subscribe, GET /ws/client, GET /ws/relay/:token, GET /health, GET /metrics, /* (SPA fallback)

### 6.3 Supporting Packages (TDD)

- `signaling/signaling.go` — WebRTC SDP/ICE relay
- `notifications/notifications.go` — device status push
- `mps/mps.go` — Intel AMT MPS (stub initially)
- `multiserver/multiserver.go` — peer discovery (stub initially)
- `config/config.go` — ServerConfig from file/env

### 6.4 Server Binary Integration

Update `cmd/meshserver/main.go` — wire all components, graceful shutdown.

### Verification
```bash
cd server && go test -race ./...             # all pass
cd server && go build ./cmd/meshserver       # binary compiles
```

---

## PHASE 7: Web Client Foundation

**Goal**: WebSocket client, Zustand stores, auth flow, device dashboard.

### 7.1 Protocol + Codec (TDD)

**Tests:** encode/decode roundtrip, golden file match, frame type detection

**Impl:** `src/protocol/types.ts`, `src/protocol/codec.ts` (msgpack), `src/lib/crypto.ts`

### 7.2 WebSocket Client (TDD)

**Tests:** connect, send/receive, reconnect with backoff, unsubscribe

**Impl:** `src/protocol/ws-client.ts` — MeshWebSocket class, auto-reconnect

### 7.3 Zustand Stores (TDD)

**Tests:** initial state, fetch devices, update on WS event, auth login/logout

**Impl:** `src/state/{device,auth,session}-store.ts`

### 7.4 Auth Feature (TDD)

**Tests:** render form, submit, error display, redirect guard

**Impl:** `src/features/auth/{LoginPage,AuthGuard}.tsx`, `api.ts`

### 7.5 Devices Dashboard (TDD)

**Tests:** render list, online/offline indicators, filter, select, actions

**Impl:** `src/features/devices/{DeviceList,DeviceDetail,DeviceActions}.tsx`

### Verification
```bash
cd web && npm test -- --run   # all pass
cd web && npm run build       # succeeds
cd web && npm run lint        # clean
```

---

## PHASE 8: Web Client Features

**Goal**: Remote desktop, terminal, file manager, messenger.

### 8.1 WebRTC Utility — `src/lib/webrtc.ts` (TDD)
### 8.2 Remote Desktop — canvas + Web Worker + OffscreenCanvas + input handler (TDD)
### 8.3 Terminal — xterm.js integration (TDD)
### 8.4 File Manager — directory browsing, chunked upload/download (TDD)
### 8.5 Messenger — chat interface (TDD)

### Verification
```bash
cd web && npm test -- --run && npm run build
```

---

## PHASE 9: Admin, Notifications, Service Worker

- Admin dashboard (user/group management, server settings)
- Notification center (Web Push)
- Service worker (offline caching, push handler)

### Verification
```bash
cd web && npm test -- --run && npm run build
```

---

## PHASE 10: Integration & E2E Tests

- Docker Compose test environment (server + Postgres + agent)
- Go integration tests: agent connect, reconnect, session lifecycle
- Playwright E2E: login, device list, remote desktop, terminal, file manager
- Load tests: 100+ concurrent agents, relay throughput
- Cross-language golden file CI gate

### Verification
```bash
make ci     # lint + test + build + golden — all green
make e2e    # Playwright E2E pass
```
