# Phase 9: Agent Session Handler + WebRTC Signaling

## Context

Phases 0-8 built the full stack: protocol, server (DB, auth, QUIC, relay), web client (desktop/terminal/file/chat UI), and platform traits. However, the Rust agent **cannot participate in sessions** — it receives `SessionRequest` but has no code to connect to the relay or stream data. WebRTC signaling messages (`SwitchToWebRTC`, `IceCandidate`, `SwitchAck`) are defined in all 3 languages but unimplemented. The `server/internal/signaling/` package is an empty stub.

Phase 9 delivers **end-to-end session flow**: agent connects to relay and streams data, then optionally upgrades to WebRTC for direct P2P.

---

## Part A: Agent Relay Session Handler

### A1. WebSocket Client for Relay Connection

**New file**: `agent/crates/mesh-agent-core/src/session.rs`

- `SessionHandler` struct: manages one relay session
  - Fields: `token: SessionToken`, `permissions: Permissions`, `ws_stream` (tokio-tungstenite WebSocket)
  - `async fn connect(relay_url: &str, token: &SessionToken) -> Result<Self, SessionError>`
  - `async fn run(&mut self, capture: Box<dyn ScreenCapture>, injector: Box<dyn InputInjector>) -> Result<(), SessionError>`
  - Internal: read/write loops using `Frame` codec over WebSocket binary messages
  - Sends frames: DesktopFrame, TerminalFrame, FileFrame
  - Receives frames: ControlMessage (input events, file ops, signaling)

**New file**: `agent/crates/mesh-agent-core/src/session_error.rs`
- `SessionError` enum: `WebSocket(String)`, `Protocol(ProtocolError)`, `Capture(CaptureError)`, `Io(io::Error)`

**Modify**: `agent/crates/mesh-agent-core/Cargo.toml`
- Add: `tokio-tungstenite = { version = "0.24", features = ["native-tls"] }`, `webrtc = "0.12"`

**Modify**: `agent/crates/mesh-agent-core/src/lib.rs`
- Add `pub mod session;` and `pub mod webrtc;`

### A2. Session Dispatch in Agent Connection

**Modify**: `agent/crates/mesh-agent-core/src/connection.rs`

- Add `handle_session_request()` method to `AgentConnection`:
  - Receives `SessionRequest { token, relay_url, permissions }`
  - Sends `SessionAccept { token, relay_url }` back on control stream
  - Spawns `SessionHandler::connect(relay_url, token)` on a new tokio task
  - Stores task handle for cleanup on disconnect
- Keep control stream running (session runs on separate WebSocket, not on QUIC control stream)

### A3. Desktop Capture Streaming

**Modify**: `agent/crates/mesh-agent-core/src/session.rs`

- `capture_loop()`: calls `capture.next_frame()` in loop, encodes as `Frame::Desktop(DesktopFrame)`, writes to WebSocket
- Rate limiting: configurable FPS cap (default 30), skip if previous frame still sending
- Frame sequence numbering for ordering

### A4. Terminal Forwarding

**New file**: `agent/crates/mesh-agent-core/src/terminal.rs`

- `TerminalSession` struct: spawns PTY (via `portable-pty` or raw libc), bridges stdin/stdout to relay
  - `fn spawn(cols: u16, rows: u16) -> Result<Self, SessionError>`
  - `async fn run(&mut self, ws_tx: Sender<Frame>) -> Result<(), SessionError>`
  - Handles `KeyPress` -> PTY stdin, `TerminalResize` -> PTY resize
  - PTY stdout -> `Frame::Terminal(TerminalFrame)` -> relay

**Modify**: `agent/crates/mesh-agent-core/Cargo.toml`
- Add: `portable-pty = "0.8"` (cross-platform PTY)

### A5. File Operations Handler

**New file**: `agent/crates/mesh-agent-core/src/file_ops.rs`

- `FileOpsHandler` struct: processes file control messages
  - `handle_file_list(path: &str) -> ControlMessage::FileListResponse`
  - `handle_file_download(path: &str, ws_tx: Sender<Frame>) -> Result<()>` — streams 256 KiB chunks as `Frame::File`
  - `handle_file_upload(path: &str, total_size: u64) -> UploadReceiver` — accepts incoming file frames
- Permission checks against session `Permissions` struct

### A6. Tests for Part A

- `session.rs` `#[cfg(test)]`: connect to mock WS server, verify frame encoding
- `terminal.rs` `#[cfg(test)]`: spawn terminal, write input, read output
- `file_ops.rs` `#[cfg(test)]`: list directory, download file, verify chunks
- `connection.rs` `#[cfg(test)]`: receive SessionRequest -> sends SessionAccept

---

## Part B: WebRTC Signaling

### B1. Server — ICE Configuration

**New file**: `server/internal/signaling/config.go`
```
type ICEServer struct {
    URLs       []string
    Username   string
    Credential string
}

type Config struct {
    ICEServers       []ICEServer
    UpgradeTimeout   time.Duration  // default 30s
    ICEGatherTimeout time.Duration  // default 10s
}
```

**Modify**: `api/openapi.yaml` — Add `ICEServer` schema, extend `CreateSessionResponse` with `ice_servers` array

**Modify**: `server/internal/api/handlers_sessions.go` — Return `ice_servers` in CreateSession response

**Modify**: `server/internal/api/api.go` — Add `signaling.Config` to `Server` struct

### B2. Server — Signaling State Machine

**New file**: `server/internal/signaling/state.go`
- `Phase` enum: `Relay`, `Offered`, `Answered`, `ICEGathering`, `Connected`, `Failed`
- `SessionState` struct with mutex-protected phase transitions
- Valid transitions enforced (e.g., Relay->Offered OK, Relay->Connected invalid)

**New file**: `server/internal/signaling/tracker.go`
- `Tracker` struct: `sync.Map[SessionToken]*SessionState`
- `StartSignaling()`, `GetState()`, `RecordAck()`, `Remove()`
- Metrics: upgrade success/failure counters

### B3. Browser — WebRTC Transport

**New file**: `web/src/lib/transport/webrtc-transport.ts`
- `WebRTCTransport` class wrapping `RTCPeerConnection`
- 3 data channels: `control` (ordered, reliable), `desktop` (unordered, maxRetransmits=0), `bulk` (ordered, reliable)
- Same frame encoding as WSTransport (reuse `encodeFrame`/`decodeFrame`)
- Methods: `createOffer(config)`, `handleAnswer(sdp)`, `addIceCandidate(candidate, mid)`
- ICE candidate buffering until remote description is set
- `onLocalIceCandidate` callback for trickle ICE

### B4. Browser — Connection Store Upgrade Flow

**Modify**: `web/src/state/connection-store.ts`
- New fields: `webrtcTransport`, `signalingState` (`relay-only` | `upgrading` | `webrtc` | `fallback`), `iceServers`
- `initiateWebRTCUpgrade()`:
  1. Create `WebRTCTransport` with `iceServers`
  2. `createOffer()` -> send `SwitchToWebRTC { sdp_offer }` via WSTransport
  3. Set `signalingState = 'upgrading'`
- Intercept control messages in `onControlMessage`:
  - `SwitchToWebRTC` (agent answer) -> `webrtcTransport.handleAnswer()`
  - `IceCandidate` -> `webrtcTransport.addIceCandidate()`
  - `SwitchAck` -> switch `activeTransport` to WebRTC, send own `SwitchAck`
- On WebRTC failure: remain on relay, set `signalingState = 'fallback'`

**Modify**: `web/src/state/session-store.ts` — Pass `ice_servers` from CreateSession response

### B5. Rust Agent — WebRTC Peer Connection

**New file**: `agent/crates/mesh-agent-core/src/webrtc.rs`
- `AgentPeerConnection` struct wrapping `webrtc-rs` `RTCPeerConnection`
- `handle_offer(sdp) -> Result<String>` — set remote desc, create answer
- `add_ice_candidate(candidate, mid)` with buffering
- `on_ice_candidate()` callback
- 3 data channels matching browser (accept from remote)
- `send_frame(Frame)` routes to appropriate channel

### B6. Agent — Signaling in Session Handler

**Modify**: `agent/crates/mesh-agent-core/src/session.rs`
- Add signaling message dispatch in the receive loop:
  - `SwitchToWebRTC { sdp_offer }` -> create `AgentPeerConnection`, generate answer, send back
  - `IceCandidate` -> forward to peer connection
  - `SwitchAck` -> mark upgrade complete, redirect frame output to data channels
- Keep relay WebSocket alive as fallback

### B7. Golden Files

**New golden files** in `testdata/golden/`:
- `control_switch_to_webrtc.bin`
- `control_switch_ack.bin`
- `control_ice_candidate.bin`

**Modify**: Rust golden generator + Go `golden_test.go` to cover these 3 message types

---

## Implementation Order

```
A1 (WS client) -> A2 (dispatch) -> A3 (capture) -> A4 (terminal) -> A5 (files)
                                                                         |
B1 (ICE config) -> B2 (state machine) ─────────────────────────────────> |
B3 (browser WebRTC) -> B4 (store upgrade) ─────────────────────────────> |
                                           B5 (agent WebRTC) -> B6 ──> B7
```

Parallel tracks:
- **Track A** (agent relay): A1 -> A2 -> A3/A4/A5 (A3-A5 can parallelize)
- **Track B server**: B1 -> B2
- **Track B browser**: B3 -> B4
- **Track B agent**: B5 -> B6
- **Final**: B7 (golden files), integration tests

---

## New Dependencies

| Crate/Package | Purpose |
|---|---|
| `tokio-tungstenite` 0.24 | Agent WebSocket client for relay |
| `webrtc` 0.12 | Agent-side WebRTC (webrtc-rs) |
| `portable-pty` 0.8 | Cross-platform PTY for terminal sessions |

---

## Files Summary

### New Files (14)
| File | Purpose |
|---|---|
| `agent/crates/mesh-agent-core/src/session.rs` | Agent session handler (relay WS + frame streaming) |
| `agent/crates/mesh-agent-core/src/session_error.rs` | Session error types |
| `agent/crates/mesh-agent-core/src/terminal.rs` | PTY spawning and terminal forwarding |
| `agent/crates/mesh-agent-core/src/file_ops.rs` | File list/download/upload handler |
| `agent/crates/mesh-agent-core/src/webrtc.rs` | Agent WebRTC peer connection |
| `server/internal/signaling/config.go` | ICE server config types |
| `server/internal/signaling/config_test.go` | Config tests |
| `server/internal/signaling/state.go` | Signaling state machine |
| `server/internal/signaling/state_test.go` | State transition tests |
| `server/internal/signaling/tracker.go` | Per-session signaling tracker |
| `server/internal/signaling/tracker_test.go` | Tracker tests |
| `web/src/lib/transport/webrtc-transport.ts` | Browser WebRTC transport |
| `web/src/lib/transport/webrtc-transport.test.ts` | WebRTC transport tests |
| `server/tests/integration/signaling_test.go` | Integration tests |

### Modified Files (10)
| File | Change |
|---|---|
| `agent/crates/mesh-agent-core/Cargo.toml` | Add tokio-tungstenite, webrtc, portable-pty |
| `agent/crates/mesh-agent-core/src/lib.rs` | Add pub mod session, webrtc, terminal, file_ops |
| `agent/crates/mesh-agent-core/src/connection.rs` | Add handle_session_request, spawn session task |
| `agent/Cargo.toml` | Workspace deps: tokio-tungstenite, webrtc, portable-pty |
| `api/openapi.yaml` | ICEServer schema, extend CreateSessionResponse |
| `server/internal/api/api.go` | Add signaling config to Server |
| `server/internal/api/handlers_sessions.go` | Return ice_servers in CreateSession |
| `web/src/state/connection-store.ts` | WebRTC upgrade orchestration |
| `web/src/state/connection-store.test.ts` | Signaling test cases |
| `web/src/state/session-store.ts` | Pass ice_servers from session response |

---

## Verification

1. **Unit tests**: `make test` — all new tests in session.rs, terminal.rs, file_ops.rs, webrtc.rs, signaling/*.go, webrtc-transport.test.ts, connection-store.test.ts
2. **Golden files**: `make golden` — verify new signaling message golden files
3. **Integration**: `go test ./tests/integration/...` — signaling flow via relay
4. **Lint**: `make lint` — clippy, go vet, eslint, actionlint
5. **Manual E2E**: Start server, connect agent, create session from browser, verify desktop/terminal/file data flows through relay, then verify WebRTC upgrade attempt
