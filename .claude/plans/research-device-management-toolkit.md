# System Tray Feature — Implementation Plan

## Context

The OpenGate agent is a headless Rust service running as a root systemd service. Desktop users need a system tray icon for quick access to agent management (restart, update, chat, logs, build info). The tray must run in the user's desktop session while the agent runs as root — requiring a two-binary architecture with IPC.

---

## Architecture: Two Binaries + Unix Socket IPC

```
┌────────────────────────────────────┐     ┌──────────────────────────────────┐
│  mesh-agent  (root, systemd)       │     │  mesh-agent-tray  (user session) │
│                                    │     │                                  │
│  QUIC connection to server         │◄───►│  System tray icon (tray-icon)    │
│  Session handling                  │ IPC │  Context menu (muda)             │
│  Updates, enrollment               │     │  Webview chat window (wry)       │
│  Log file writer (tracing-appender)│     │  Log tail viewer                 │
│  IPC server on Unix socket         │     │  Desktop notifications           │
└────────────────────────────────────┘     └──────────────────────────────────┘
         /run/mesh-agent/tray.sock (JSON-over-newline)
```

### IPC Protocol

Unix domain socket at `/run/mesh-agent/tray.sock`. Agent creates socket with group-readable permissions (e.g., `mesh-agent` group). JSON-over-newline protocol:

```jsonc
// Tray → Agent (requests)
{"type":"status"}
{"type":"restart"}
{"type":"check_update"}
{"type":"request_chat_token"}
{"type":"get_info"}

// Agent → Tray (responses)
{"type":"status","connected":true,"version":"0.15.4","server_addr":"opengate.example.com:4433","uptime_secs":123456}
{"type":"restart_ack"}
{"type":"update_status","status":"checking|downloading|no_update|applied","version":"0.16.0"}
{"type":"chat_token","url":"https://...","token":"abc123","expires_at":"..."}
{"type":"info","version":"0.15.4","device_id":"...","hostname":"...","os":"...","arch":"...","connected":true,"uptime_secs":123456}

// Agent → Tray (push events)
{"type":"connection_changed","connected":true}
{"type":"update_progress","percent":45,"version":"0.16.0"}
```

### Tray Lifecycle

- `mesh-agent-tray` auto-starts via XDG autostart (`~/.config/autostart/mesh-agent-tray.desktop`)
- Connects to agent's Unix socket on startup
- If agent unavailable: tray shows gray/disconnected icon, retries connection every 5s
- If agent restarts: tray reconnects automatically

---

## Dependencies

### mesh-agent-tray (new crate)

| Crate | Version | Purpose |
|-------|---------|---------|
| `tray-icon` | ~0.19 | System tray icon (Linux SNI/D-Bus, Windows) |
| `muda` | ~0.16 | Context menu construction |
| `wry` | ~0.49 | Embedded webview for chat window |
| `open` | ~5 | Open log files in system editor |
| `notify-rust` | ~4 | Desktop notifications (build info) |
| `arboard` | ~3 | Clipboard access (copy build info) |
| `serde` + `serde_json` | 1.x | IPC JSON serialization |
| `image` | 0.25 (workspace) | Icon format conversion |
| `tokio` | 1.x (workspace) | Async socket I/O |

**Linux runtime**: `libayatana-appindicator3-1` (Ubuntu/Debian) or `libappindicator-gtk3` (Fedora) + `libwebkitgtk-6.0` (for wry/webview)

### mesh-agent additions

| Crate | Purpose |
|-------|---------|
| `tracing-appender` ~0.2 | Rolling file log writer |
| `tokio` UnixListener | IPC socket server |

---

## Menu Items — Implementation Details

### 1. Restart Agent
- Tray sends `{"type":"restart"}` over IPC
- Agent responds `{"type":"restart_ack"}`, then calls `std::process::exit(42)`
- systemd `RestartForceExitStatus=42` restarts the agent
- Tray detects socket disconnect, shows "Restarting..." icon, reconnects when agent is back
- **Confirmation**: Tray shows native confirmation dialog before sending (wry or `zenity`)

### 2. Initiate Agent Update
- Tray sends `{"type":"check_update"}` over IPC
- Agent sends `ControlMessage::RequestUpdate` to server over QUIC
- Agent streams progress back: `{"type":"update_progress","percent":N,...}`
- If no update: `{"type":"update_status","status":"no_update"}` → tray shows notification "Already on latest"
- If update found: download → verify → apply → exit 42 → restart
- **New protocol messages needed**: `RequestUpdate` (agent→server), `UpdateCheckResponse` (server→agent)

### 3. Open Chat (Embedded Webview)
- Tray sends `{"type":"request_chat_token"}` over IPC
- Agent requests short-lived token from server via QUIC (`RequestChatToken`/`ChatTokenResponse`)
- Agent responds with URL + token
- Tray spawns `wry::WebView` window:
  - Positioned bottom-right corner of primary monitor
  - Size: ~400x600px
  - URL: `https://{server}/device/{device_id}/chat?token={token}`
  - Window title: "OpenGate Chat"
  - Borderless or minimal chrome
- If chat window already open: bring to front instead of spawning new
- **New server web route**: `/device/:id/chat` — minimal chat UI (no sidebar/nav)
- **New protocol messages**: `RequestChatToken` (agent→server), `ChatTokenResponse` (server→agent)

### 4. View Agent Logs (Persistent File + Tail)
- Agent writes logs to rotating file via `tracing-appender`:
  - Path: `/var/log/mesh-agent/agent.log`
  - Rotation: daily, keep last 7 days
  - Format: standard tracing fmt (timestamp, level, target, message)
- Tray opens log file with `open::that("/var/log/mesh-agent/agent.log")`
- **Submenu options**:
  - "Open Current Log" → `open::that()` in system text editor
  - "Tail Live Logs" → spawn terminal: `x-terminal-emulator -e tail -f /var/log/mesh-agent/agent.log`
  - (Linux+systemd bonus): "journalctl" → `x-terminal-emulator -e journalctl -fu mesh-agent`

### 5. Agent Build Info (Notification + Clipboard)
- Tray sends `{"type":"get_info"}` over IPC
- Agent responds with full metadata
- Tray shows desktop notification via `notify-rust`:
  ```
  OpenGate Agent v0.15.4
  Connected · workstation-01
  Full details copied to clipboard
  ```
- Tray copies full block to clipboard via `arboard`:
  ```
  Version:   0.15.4
  Device ID: a1b2c3d4-e5f6-...
  Hostname:  workstation-01
  OS:        Ubuntu 24.04 LTS
  Arch:      x86_64
  Server:    opengate.example.com:4433
  Status:    Connected
  Uptime:    3d 14h 22m
  ```

---

## Tray Icon States

| State | Color | Tooltip |
|-------|-------|---------|
| Connected | Green shield | "OpenGate Agent v0.15.4 — Connected" |
| Reconnecting | Yellow shield | "OpenGate Agent — Reconnecting..." |
| Disconnected | Gray shield | "OpenGate Agent — Disconnected" |
| Updating | Blue shield | "OpenGate Agent — Updating..." |
| Tray disconnected from agent | Red outline shield | "OpenGate Agent — Service unavailable" |

Icons embedded via `include_bytes!` (16x16, 22x22, 32x32 PNG).

---

## New Crate Structure

```
agent/crates/mesh-agent-tray/
├── Cargo.toml
├── build.rs                    # Embed version (same pattern as mesh-agent)
├── assets/
│   ├── icon-connected.png
│   ├── icon-reconnecting.png
│   ├── icon-disconnected.png
│   ├── icon-updating.png
│   └── icon-unavailable.png
└── src/
    ├── main.rs                 # Entry point: connect IPC, spawn tray, event loop
    ├── ipc.rs                  # Unix socket client, JSON protocol, reconnect logic
    ├── menu.rs                 # Menu construction with muda, event handling
    ├── tray.rs                 # tray-icon setup, icon state management
    ├── chat.rs                 # wry webview window management
    ├── notifications.rs        # notify-rust + arboard for build info
    └── logs.rs                 # Log file opener, terminal spawner
```

---

## Files to Modify

### Agent-side (mesh-agent)

| File | Change |
|------|--------|
| `agent/crates/mesh-agent/src/main.rs` | Add IPC server spawn, log file setup |
| `agent/crates/mesh-agent/src/ipc_server.rs` | **New** — Unix socket listener, JSON handler, bridges to agent state |
| `agent/crates/mesh-agent/Cargo.toml` | Add `tracing-appender`, `serde_json` deps |
| `agent/Cargo.toml` | Add `mesh-agent-tray` to workspace members, add `tracing-appender` to workspace deps |

### Protocol (mesh-protocol)

| File | Change |
|------|--------|
| `agent/crates/mesh-protocol/src/control.rs` | Add `RequestUpdate`, `RequestChatToken`, `ChatTokenResponse`, `UpdateCheckResponse` messages |

### Server-side (for chat token + update check)

| File | Change |
|------|--------|
| `server/internal/api/handlers.go` | Add `handleRequestChatToken`, `handleCheckUpdate` |
| `server/internal/agentapi/server.go` | Handle new control messages from agent |
| `web/src/pages/DeviceChat.tsx` | **New** — minimal chat UI for `/device/:id/chat` route |
| `web/src/App.tsx` | Add chat route |

### New crate

| File | Change |
|------|--------|
| `agent/crates/mesh-agent-tray/` | **New** — entire crate (see structure above) |

---

## Implementation Phases

### Phase 1: Foundation (IPC + tray shell)
1. Add IPC server to `mesh-agent` (Unix socket, JSON protocol)
2. Add `tracing-appender` rolling file logger to `mesh-agent`
3. Create `mesh-agent-tray` crate with tray icon + menu skeleton
4. Implement IPC client in tray with reconnect logic
5. Implement `status` request/response — tray icon reflects connection state
6. Tests: IPC protocol serialization, socket connect/disconnect handling

### Phase 2: Menu actions
7. Implement "Restart Agent" (IPC → exit 42)
8. Implement "View Agent Logs" (open file + tail submenu)
9. Implement "Agent Build Info" (notification + clipboard)
10. Tests: restart flow, log file rotation, info formatting

### Phase 3: Server integration
11. Add `RequestUpdate`/`UpdateCheckResponse` to mesh-protocol
12. Add `RequestChatToken`/`ChatTokenResponse` to mesh-protocol
13. Implement server handlers for update check + chat token
14. Implement "Initiate Update" in tray (IPC → QUIC → server)
15. Golden file tests for new protocol messages
16. Tests: update check flow, token generation/expiry

### Phase 4: Chat webview
17. Create minimal `/device/:id/chat` web route (React)
18. Implement wry webview window in tray (`chat.rs`)
19. Wire "Open Chat" menu item (IPC → token → webview)
20. Tests: webview lifecycle, token auth flow

### Phase 5: Polish + CI
21. Design and embed tray icons (all states)
22. XDG autostart `.desktop` file generation/installation
23. CI: build `mesh-agent-tray` for x86_64 + aarch64
24. Update release workflow for desktop variant
25. Wiki update

---

## Verification Plan

1. **Unit tests**: IPC JSON serialization/deserialization, log rotation config, info formatting, protocol message golden files
2. **Integration tests**: IPC socket connect/disconnect/reconnect, restart flow (exit code 42), update check round-trip
3. **Manual desktop test**: On Ubuntu/Fedora with GNOME — verify tray icon appears, all menu items work, webview opens at correct position, icon state changes with connection
4. **Headless verification**: Agent without tray binary works identically to today (IPC server listens but no client connects — no effect)
5. **Graceful degradation**: Tray binary started without agent service → shows "unavailable" icon, retries connection
