# Remote Sessions

Taking a machine over from the browser: its screen, a shell, its filesystem, and
a chat window back to whoever is sitting in front of it. A session starts on the
relay, which always works, and upgrades itself to a direct peer connection when
the network allows.

Which machines a technician can open a session on is in
[Tenancy and Access](./Tenancy-and-Access.md); the frame encoding is in
[Wire Protocol](../architecture/Wire-Protocol.md); establishment, relay and
teardown are in [Overview](../architecture/Overview.md#session-lifecycle).

## The session surface

| Feature | Path | Description |
|---------|------|-------------|
| **Session View** | `/sessions/:token` | Tab container with toolbar and connection status |
| **Remote Desktop** | Desktop tab | Canvas-based screen viewer with mouse/keyboard input forwarding |
| **Terminal** | Terminal tab | xterm.js terminal connected to relay |
| **File Manager** | Files tab | Directory browsing, file download/upload with progress, in-browser file viewer |
| **Messenger** | Chat tab | Real-time chat over relay control messages |

Every tab rides the binary frame protocol ([`web/src/lib/protocol/`](../../web/src/lib/protocol)) over a single WebSocket connection managed by a Zustand store ([`connection-store.ts`](../../web/src/features/session/state/connection-store.ts)).

**Capability-based tab visibility**: The Session View dynamically shows/hides tabs based on the device's reported capabilities. Linux agents report Terminal + FileManager only; an agent whose platform crate provides desktop capture and input injection additionally reports RemoteDesktop. The web client receives capabilities via the Device API and passes them to Session View via React Router state. Devices without the `RemoteDesktop` capability will only show Terminal and Files tabs; Desktop and Chat tabs require it.

## Session lifecycle notifications

A remote session starting and ending are two of the four events browser push
carries (`session_started`, `session_ended`); see
[Fleet and Devices](./Fleet-and-Devices.md#browser-notifications).

## Agent Session Handler

When the server assigns a session to an agent, the agent connects to the relay and streams data:

```
Server                         Agent
  │                              │
  │──── SessionRequest ────────►│  (token, relay_url, permissions)
  │◄─── SessionAccept ──────────│  (confirms intent)
  │                              │
  │   Agent connects to relay    │
  │   at relay_url?side=agent    │
  │                              │
  │◄── Desktop/Terminal/File ────│  (binary frames via relay)
  │──── Input/Control ──────────►│  (mouse, keyboard, file ops)
```

The `SessionHandler` (Rust) manages the full lifecycle:

- **Desktop capture**: Streams JPEG-encoded screen frames at ~10 FPS via the relay (quality 70, falls back to raw on encode failure)
- **Terminal**: Spawns a PTY (`portable-pty`) and bridges stdin/stdout over terminal frames
- **File operations**: Directory listing, chunked download (256 KiB), permission-gated access
- **Input injection**: Mouse/keyboard events forwarded to the OS via platform traits
- **Chat echo**: `ChatMessage` from the browser is echoed back with `sender: "agent"`, enabling basic chat between the browser user and the agent

## WebRTC Upgrade (Optional P2P)

Sessions start on the relay (always works) and can optionally upgrade to a direct WebRTC connection for lower latency:

```
Browser                    Relay                      Agent
  │                          │                          │
  │── SwitchToWebRTC ───────►│──── SwitchToWebRTC ─────►│
  │   (SDP offer)            │    (SDP offer)           │
  │                          │                          │
  │◄─ SwitchToWebRTC ────────│◄─── SwitchToWebRTC ─────│
  │   (SDP answer)           │    (SDP answer)          │
  │                          │                          │
  │◄─► IceCandidate ────────►│◄──► IceCandidate ───────►│
  │   (trickle ICE)          │    (trickle ICE)         │
  │                          │                          │
  │◄─ SwitchAck ─────────────│◄─── SwitchAck ──────────│
  │   (upgrade complete)     │                          │
  │                          │                          │
  │◄═══════ Data Channels ══════════════════════════════│
  │   control (ordered)      │                          │
  │   desktop (unordered)    │                          │
  │   bulk (ordered)         │                          │
```

Three data channels match the frame routing:

| Channel | ID | Ordered | Reliable | Purpose |
|---------|-----|---------|----------|---------|
| `control` | 0 | Yes | Yes | Control messages, signaling |
| `desktop` | 1 | No | No (maxRetransmits=0) | Screen frames (latest wins) |
| `bulk` | 2 | Yes | Yes | Terminal I/O, file transfers |

The signaling state machine (`server/internal/signaling/`) tracks upgrade progress: `Relay` → `Offered` → `Answered` → `ICEGathering` → `Connected` (or `Failed`). On failure, the relay connection remains active as fallback.

### ICE Configuration

The server provides STUN/TURN server URLs in the `CreateSession` response (`ice_servers` field). The browser and agent both use these to establish connectivity. Default configuration uses Google's public STUN server.
