# Agent Version Alignment — Implementation Plan

## Context

The mesh-agent binary reports version `0.8.0` at startup and during server registration, despite the project being at `v0.14.x`. The agent version is hardcoded in `Cargo.toml` and never overridden during CI/CD builds. This means:

- The admin dashboard shows all agents as version `0.8.0`
- OTA update `should_skip_version()` compares against `0.8.0`, not the real installed version
- No way to tell which release an agent is actually running

### Root Cause

1. All three agent Cargo.toml files have `version = "0.8.0"` (never bumped)
2. `build.rs` supports `OPENGATE_VERSION` env var override → `AGENT_VERSION` compile-time constant
3. `release-agent.yml` does NOT set `OPENGATE_VERSION` during builds
4. `main.rs:239` startup log uses `env!("CARGO_PKG_VERSION")` instead of `env!("AGENT_VERSION")`

---

## Fix 1: Inject version from git tag in `release-agent.yml`

**File:** `.github/workflows/release-agent.yml`

Add a step to extract the semver from the git tag and export `OPENGATE_VERSION`:

```yaml
- name: Determine version
  id: version
  run: |
    TAG="${{ inputs.tag || github.ref_name }}"
    VERSION="${TAG#v}"  # strip leading 'v'
    echo "version=$VERSION" >> "$GITHUB_OUTPUT"
    echo "Agent version: $VERSION"
```

Then pass it to both build steps:

```yaml
- name: Build release binary (native)
  if: matrix.target == 'x86_64-unknown-linux-musl'
  env:
    OPENGATE_VERSION: ${{ steps.version.outputs.version }}
  run: cargo build --release --target ${{ matrix.target }} -p mesh-agent

- name: Build release binary (cross)
  if: matrix.target == 'aarch64-unknown-linux-musl'
  env:
    OPENGATE_VERSION: ${{ steps.version.outputs.version }}
  run: cross build --release --target ${{ matrix.target }} -p mesh-agent
```

## Fix 2: Use `AGENT_VERSION` consistently in `main.rs`

**File:** `agent/crates/mesh-agent/src/main.rs`

Line 239: change `env!("CARGO_PKG_VERSION")` → `env!("AGENT_VERSION")` in the startup log so it matches what's sent to the server during registration.

## Fix 3: Bump Cargo.toml versions to current

Update all three crate versions to match the current project version so local/dev builds also report a reasonable version:

- `agent/crates/mesh-agent/Cargo.toml` — `version = "0.14.1"`
- `agent/crates/mesh-agent-core/Cargo.toml` — `version = "0.14.1"`
- `agent/crates/mesh-protocol/Cargo.toml` — `version = "0.14.1"`

These will be overridden by `OPENGATE_VERSION` in CI, but serve as the fallback for local development builds.

## Fix 4: Also inject version into `build-image.yml` for server container

**File:** `.github/workflows/build-image.yml`

The server Dockerfile uses `-ldflags="-s -w"` but doesn't inject a version. Not strictly needed now (server doesn't expose version), but should add `OPENGATE_VERSION` as a build arg for consistency when we add server version reporting later. **Skip for now** — out of scope.

---

## Files to Modify

| File | Change |
|------|--------|
| `.github/workflows/release-agent.yml` | Add version extraction step, pass `OPENGATE_VERSION` env to both build steps |
| `agent/crates/mesh-agent/src/main.rs:239` | `CARGO_PKG_VERSION` → `AGENT_VERSION` |
| `agent/crates/mesh-agent/Cargo.toml` | `version = "0.14.1"` |
| `agent/crates/mesh-agent-core/Cargo.toml` | `version = "0.14.1"` |
| `agent/crates/mesh-protocol/Cargo.toml` | `version = "0.14.1"` |

## Verification

1. `make build` — agent compiles, startup log shows `0.14.1` (from Cargo.toml fallback)
2. `OPENGATE_VERSION=99.0.0 cargo build -p mesh-agent` — verify override works (grep binary for `99.0.0`)
3. `make test` — all tests pass (including `should_skip_version` which uses `AGENT_VERSION`)
4. Inspect `release-agent.yml` — on tag `v0.15.0`, the build would set `OPENGATE_VERSION=0.15.0`

---

## Previous Plans (reference only, not part of this task)

### Test Coverage Gaps
Plan at memory: `project_test_coverage_gaps.md`. 5 phases, ~17 tests. Not started.

### Phase 1 (from prior plan): Agent SessionHandler Unit Tests (Rust)

**File:** `agent/crates/mesh-agent-core/src/session/handler.rs` — add `#[cfg(test)] mod tests` block

**Why first:** No infrastructure dependencies, purely unit-test work. `SessionHandler` is easy to construct: `SessionHandler::new(token, Permissions { ... })`. Dependencies (`frame_tx`, `FileOpsHandler`, `InputInjector`, `webrtc_pc`) are injectable via channels and trait objects.

**Tests to add (8 tests):**

| # | Test Name | What It Verifies |
|---|-----------|-----------------|
| 1 | `test_handle_frame_ping_responds_pong` | `handle_frame(Frame::Ping, ...)` sends `Frame::Pong` on `frame_rx` |
| 2 | `test_handle_frame_terminal_no_session` | `Frame::Terminal(...)` with `terminal: None` — no panic, no output |
| 3 | `test_handle_frame_unexpected_type_ignored` | `Frame::Desktop(...)` — silently ignored, no output on `frame_rx` |
| 4 | `test_handle_control_mouse_move_permitted` | `permissions.input = true` → `InputInjector::inject_mouse_move` called |
| 5 | `test_handle_control_mouse_move_denied` | `permissions.input = false` → `inject_mouse_move` NOT called |
| 6 | `test_handle_control_file_list_success` | `FileListRequest("/tmp")` → `FileListResponse` on `frame_rx` |
| 7 | `test_handle_control_file_list_error` | `FileListRequest("/nonexistent_abc123")` → `FileListError` on `frame_rx` |
| 8 | `test_send_frame_closed_channel` | Drop receiver, call `send_frame` → returns `Err(SessionError::WebSocket(...))` |

**Mock pattern for InputInjector:**
```rust
struct RecordingInjector {
    calls: Arc<Mutex<Vec<String>>>,
}
impl InputInjector for RecordingInjector { ... }
```

**Dependencies to construct:**
- `frame_tx/frame_rx`: `mpsc::channel::<Vec<u8>>(64)`
- `file_ops`: `FileOpsHandler::new(true, false)` (read=true, write=false)
- `webrtc_pc`: `Arc::new(tokio::sync::Mutex::new(None))`
- `terminal`: `None` (tests 2/3) or skip terminal-dependent tests
- `injector`: `RecordingInjector` (tests 4/5) or `NullInput` (others)

---

## Phase 2: Relay Protocol Frame Integration Test (Go)

**File:** `server/tests/integration/relay_data_test.go` — add `TestRelayProtocolFrameRoundTrip`

**Why:** Existing relay tests send raw bytes. This test sends properly encoded protocol frames (msgpack control messages with `[type][4-byte len][payload]` framing) through the full QUIC+WebSocket relay path.

**Test: `TestRelayProtocolFrameRoundTrip`** — table-driven with `t.Run`:

| Sub-test | Direction | Frame Type | Payload |
|----------|-----------|------------|---------|
| `control_mouse_move` | browser→agent | `FrameControl` | `MsgMouseMove{X:100, Y:200}` |
| `control_file_list_request` | browser→agent | `FrameControl` | `MsgFileListRequest{Path:"/home"}` |
| `terminal_frame` | agent→browser | `FrameTerminal` | `TerminalFrame{Data:"ls -la\n"}` |
| `bidirectional_control` | both ways | `FrameControl` | Mouse + FileListResponse simultaneously |

**How encoding works in test:**
```go
codec := &protocol.Codec{}
// Encode
payload, _ := codec.EncodeControl(&protocol.ControlMessage{Type: MsgMouseMove, X: 100, Y: 200})
var buf bytes.Buffer
codec.WriteFrame(&buf, protocol.FrameControl, payload)
// Send via WebSocket
agentConn.Write(ctx, websocket.MessageBinary, buf.Bytes())
// Receive & decode
_, data, _ := browserConn.Read(ctx)
reader := bytes.NewReader(data)
frameType, framePayload, _ := codec.ReadFrame(reader)
msg, _ := codec.DecodeControl(framePayload)
```

**Reuses:** `setupRelayPair` helper from existing tests.

---

## Phase 3: Middleware + WebSocket Integration Test (Go)

**File:** `server/tests/integration/middleware_ws_test.go` (new file)

**Why:** The `http.Hijacker` fix is critical — if middleware wrapping breaks `Hijack()`, WebSocket upgrades silently fail. Current tests verify Hijacker implementation in isolation but never test it through the full `Recoverer → RequestID → HTTPMiddleware → SecurityHeaders → MaxBodySize → RequestLogger` stack.

**Tests to add (2 tests):**

| # | Test Name | What It Verifies |
|---|-----------|-----------------|
| 1 | `TestWebSocketUpgradeThroughFullMiddlewareStack` | Connect WS relay pair through `httptest.Server` (which uses full `api.NewServer` router). Verify: (a) security headers present on REST request, (b) WS upgrade succeeds, (c) bidirectional data flows |
| 2 | `TestRelayRouteBypassesRequestTimeout` | Send a message through relay, wait >30s (the RequestTimeout value), verify relay connection still alive. Uses `newSessionTestEnv` which sets up full middleware. |

**Implementation note:** Test 1 is partially covered by existing `setupRelayPair` usage, but this test makes the middleware verification _explicit_ and named. Test 2 confirms the relay route lives outside the timeout group.

---

## Phase 4: WebRTC Signaling Flow Through Relay (Go)

**File:** `server/tests/integration/signaling_relay_test.go` (new file)

**Why:** The relay-to-WebRTC upgrade is the most complex untested cross-component flow. No test currently exercises the signaling message exchange through the relay WebSocket path.

**Tests to add (2 tests):**

| # | Test Name | What It Verifies |
|---|-----------|-----------------|
| 1 | `TestSignalingFlowThroughRelay` | Full signaling flow via relay: browser sends `SwitchToWebRTC` (fake SDP offer) → agent receives it → agent sends `SwitchToWebRTC` (fake SDP answer) → ICE candidate exchange → both send `SwitchAck` → verify `sigTracker` state reaches `PhaseConnected` |
| 2 | `TestSignalingTimeout` | Start signaling, send offer, do NOT send answer → verify tracker records `PhaseFailed` after timeout |

**Approach:** Use real WebSocket relay but fake SDP strings (no actual WebRTC). The relay is message-agnostic — it just forwards binary frames. The signaling state machine on the server tracks phase transitions via the tracker. Test verifies the control message round-trip AND state machine integration.

**Key detail:** The signaling tracker is available on `sessionTestEnv` via the `signaling.Tracker` passed to `api.NewServer`. We need to expose it or add a getter. Check if `env` already stores it — `newSessionTestEnv` creates `sigTracker` but doesn't store it on the struct. Need to add `sigTracker *signaling.Tracker` to `sessionTestEnv`.

**Message encoding pattern:**
```go
// Browser sends SwitchToWebRTC offer
offerMsg := &protocol.ControlMessage{
    Type:     protocol.MsgSwitchToWebRTC,
    SDPOffer: "v=0\r\no=- 123 2 IN IP4 127.0.0.1\r\n...", // fake SDP
}
payload, _ := codec.EncodeControl(offerMsg)
var buf bytes.Buffer
codec.WriteFrame(&buf, protocol.FrameControl, payload)
browserConn.Write(ctx, websocket.MessageBinary, buf.Bytes())

// Agent reads it
_, data, _ := agentConn.Read(ctx)
// Decode and verify
```

---

## Phase 5: OTA Update Integration Tests (Go + Rust)

### Go Integration Test

**File:** `server/tests/integration/update_test.go` (new file)

**Tests to add (3 tests):**

| # | Test Name | What It Verifies |
|---|-----------|-----------------|
| 1 | `TestUpdatePublishAndPush` | Admin publishes manifest → pushes update → connected agent receives `AgentUpdate` control message on QUIC stream → agent sends `AgentUpdateAck` → DB records status=success |
| 2 | `TestUpdatePush_SkipsCurrentVersion` | Agent already on target version → push returns `pushed_count=0` |
| 3 | `TestUpdatePush_NoMatchingOS` | Manifest for linux/amd64, agent reports windows/amd64 → not pushed |

**Setup extension:** `newSessionTestEnv` needs `Signing` and `Manifests`:
```go
signingKeys, _ := updater.LoadOrGenerateSigningKeys(t.TempDir())
manifestStore := updater.NewManifestStore(t.TempDir())
// Add to ServerConfig:
Signing:   signingKeys,
Manifests: manifestStore,
```
Store both on `sessionTestEnv` struct for test access.

### Rust Integration Test

**File:** `agent/crates/mesh-agent-core/src/update.rs` — add to existing `mod tests`

**Test to add (1 test):**

| # | Test Name | What It Verifies |
|---|-----------|-----------------|
| 1 | `test_apply_update_full_pipeline` | Mock HTTP server (use `wiremock` or `mockito`) serves a fake binary → call `apply_update()` → verify SHA256 match, Ed25519 signature valid, original backed up to `.prev`, new binary in place, `.update-pending` sentinel exists |

**Dev dependency:** Add `mockito` to `Cargo.toml` dev-dependencies if not present (lighter than `wiremock`).

---

## Critical Files to Modify

| File | Changes |
|------|---------|
| `agent/crates/mesh-agent-core/src/session/handler.rs` | Add `#[cfg(test)] mod tests` with 8 unit tests |
| `server/tests/integration/relay_data_test.go` | Add `TestRelayProtocolFrameRoundTrip` |
| `server/tests/integration/middleware_ws_test.go` | New file: 2 middleware+WS tests |
| `server/tests/integration/signaling_relay_test.go` | New file: 2 signaling tests |
| `server/tests/integration/update_test.go` | New file: 3 OTA tests |
| `server/tests/integration/session_test.go` | Add `sigTracker` + `signingKeys` + `manifestStore` to `sessionTestEnv` |
| `agent/crates/mesh-agent-core/src/update.rs` | Add 1 integration test |
| `agent/crates/mesh-agent-core/Cargo.toml` | Add `mockito` dev-dependency (if needed for OTA test) |

## Verification

After each phase:
1. `make test` — all unit tests pass
2. `make test-integration` — Go integration tests pass
3. `make lint` — no clippy/vet/eslint warnings
4. Run `/precommit` before committing

**Total: 17 new tests** (8 Rust unit + 4 Go sub-tests + 2 middleware + 2 signaling + 3 OTA Go + 1 OTA Rust + ~1 sub-test overhead)
