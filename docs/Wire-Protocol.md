# Wire Protocol

## Frame Format

All control messages are wrapped in a framed transport:

```
┌──────────────┬─────────────────────┬───────────────────────┐
│ Frame Type   │ Payload Length      │ Payload               │
│ (1 byte)     │ (4 bytes, BE)       │ (variable)            │
└──────────────┴─────────────────────┴───────────────────────┘
```

### Frame Types

| Type Byte | Name | Payload |
|-----------|------|---------|
| `0x01` | Control | MessagePack-encoded control message |
| `0x02` | Desktop | MessagePack-encoded `DesktopFrame` (screen capture) |
| `0x03` | Terminal | MessagePack-encoded `TerminalFrame` (terminal I/O) |
| `0x04` | File | MessagePack-encoded `FileFrame` (file transfer chunk) |
| `0x05` | Ping | None (single byte, no length/payload) |
| `0x06` | Pong | None (single byte, no length/payload) |

Ping and Pong are special: they consist of a single byte with no length prefix or payload.

## Handshake

The handshake uses **raw binary encoding** (not MessagePack) and occurs before
any framed messages. The agent opens the control stream and speaks first; the
server branches on the first handshake byte:

```mermaid
sequenceDiagram
  participant Agent
  participant Server

  alt cold start or fallback
    Agent->>Server: AgentHello (0x11)
    Server-->>Agent: ServerHello (0x10)
  else fast-path reconnect
    Agent->>Server: SkipAuth (0x14)
    Note over Agent,Server: server sends no handshake reply when cached CA hash is current
  end
  Note over Agent,Server: framed MessagePack begins after handshake
```

Active handshake type bytes are `0x10` (`ServerHello`), `0x11`
(`AgentHello`), `0x14` (`SkipAuth`), and `0x15` (`ExpectHash`). The former
proof-message reservations `0x12` and `0x13` are retired and rejected by both
decoders. The canonical constants live in
[`server/internal/protocol/types.go`](../server/internal/protocol/types.go) and
[`agent/crates/mesh-protocol/src/types/handshake.rs`](../agent/crates/mesh-protocol/src/types/handshake.rs).

## Control Messages

After the handshake, all control messages use MessagePack encoding with internally tagged enums:

```rust
#[serde(tag = "type")]
enum ControlMessage {
    Register { ... },
    Heartbeat { ... },
    SessionRequest { ... },
    // ...
}
```

The `type` field is a string that identifies the variant, enabling cross-language deserialization between Rust (`rmp-serde`) and Go (`vmihailenco/msgpack`).

Unknown future control types are tolerated at the message-dispatch layer. The
Go server decodes the unknown `type` string and logs/ignores it without dropping
the agent connection. The Rust protocol crate decodes unknown server-to-agent
tags into `ControlMessage::Unknown`, allowing the agent control loop to ignore
the frame and continue. Malformed frames and oversized payloads remain fatal.

### Control Message Variants

| Variant | Direction | Fields |
|---------|-----------|--------|
| `AgentRegister` | Agent → Server | `capabilities`, `hostname`, `os`, `arch`, `version` |
| `AgentHeartbeat` | Agent → Server | `timestamp` |
| `SessionAccept` | Agent → Server | `token`, `relay_url` |
| `SessionReject` | Agent → Server | `token`, `reason` |
| `SessionRequest` | Server → Agent | `token`, `relay_url`, `permissions` |
| `AgentUpdate` | Server → Agent | `version`, `url`, `sha256`, `signature` |
| `AgentUpdateAck` | Agent → Server | `version`, `success`, `error` |
| `AgentDeregistered` | Server → Agent | `reason` |
| `RelayReady` | Bidirectional | _(none)_ |
| `SwitchToWebRTC` | Bidirectional | `sdp_offer` |
| `SwitchAck` | Bidirectional | _(none)_ |
| `IceCandidate` | Bidirectional | `candidate`, `mid` |
| `MouseMove` | Browser → Agent | `x`, `y` |
| `MouseClick` | Browser → Agent | `button`, `pressed`, `x`, `y` |
| `KeyPress` | Browser → Agent | `key`, `pressed` |
| `TerminalResize` | Browser → Agent | `cols`, `rows` |
| `FileListRequest` | Browser → Agent | `path` |
| `FileListResponse` | Agent → Browser | `path`, `entries` |
| `FileListError` | Agent → Browser | `path`, `error` |
| `FileDownloadRequest` | Browser → Agent | `path` |
| `FileUploadRequest` | Browser → Agent | `path`, `total_size` |
| `ChatMessage` | Bidirectional | `text`, `sender` |
| `RestartAgent` | Server → Agent | `reason` |
| `RequestHardwareReport` | Server → Agent | _(none)_ |
| `HardwareReport` | Agent → Server | `cpu_model`, `cpu_cores`, `ram_total_mb`, `disk_total_mb`, `disk_free_mb`, `network_interfaces` |
| `HardwareReportError` | Agent → Server | `error` |
| `RequestUpdate` | Agent → Server | _(none)_ |
| `UpdateCheckResponse` | Server → Agent | `available`, `version`, `url`, `sha256`, `signature` |
| `RequestChatToken` | Agent → Server | `device_id` |
| `ChatTokenResponse` | Server → Agent | `url`, `token`, `expires_at` |
| `RequestDeviceLogs` | Server → Agent | `log_level`, `time_from`, `time_to`, `search`, `log_offset`, `log_limit` |
| `DeviceLogsResponse` | Agent → Server | `log_entries` (Vec\<LogEntry\>), `total_count`, `has_more` |
| `DeviceLogsError` | Agent → Server | `error` |

### Capabilities

`AgentRegister.capabilities` is the negotiation surface for additive
server-to-agent control messages. The server must not send a new
server-to-agent variant unless the connected agent advertised the matching
capability. Current additive gates:

| Capability | Gates |
|------------|-------|
| `HardwareInventory` | `RequestHardwareReport` |
| `DeviceLogs` | `RequestDeviceLogs` |

Tolerant unknown-message decoding is a backstop for mixed fleets; capability
gating is the primary safety mechanism.

### LogEntry Struct

The `DeviceLogsResponse` message carries an array of `LogEntry` structs:

| Field | Type | Description |
|-------|------|-------------|
| `timestamp` | string | ISO 8601 timestamp of the log line |
| `level` | string | Log level (`TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `target` | string | Rust tracing target (module path) |
| `message` | string | Log message body |

The agent parses daily-rotated log files written by `tracing-subscriber` and returns matching entries. The server caches individual rows in the `device_logs` table with a 5-minute TTL to avoid repeated round-trips.

### Data Frame Types

**DesktopFrame**: `sequence`, `x`, `y`, `width`, `height`, `encoding` (Raw/Zlib/Zstd/Jpeg/H264Idr/H264Delta), `data` (raw bytes)

**TerminalFrame**: `data` (raw bytes)

**FileFrame**: `offset`, `total_size`, `data` (raw bytes, 256 KiB chunks). The browser sends a `FileDownloadRequest` control message, then the agent streams back FileFrame chunks. The browser accumulates chunks via `DownloadAccumulator` and on completion either triggers a browser download (save-to-disk) or displays the content in an in-browser file viewer. Empty files produce a single frame with `total_size: 0` and empty `data`.

## Cross-Language Compatibility

Golden file tests guarantee bit-identical encoding between Rust and Go:

```
  Rust (encoder)                         Go (decoder)
       │                                      │
       │── generate fixtures ──►  testdata/golden/*.bin
       │                                      │
       │                          verify fixtures ──►  pass/fail
```

1. Rust tests serialize known messages to binary and write them to `testdata/golden/`
2. Go tests read the same files and deserialize, asserting field-level equality
3. Go reverse-golden tests serialize representative frames to `go_*.bin`
4. Rust reverse-golden tests read those files and assert field-level equality

This catches encoding drift in both directions. Unknown future control-type
fixtures are included for both agent-to-server and server-to-agent compatibility.
The CI pipeline sequences the golden verification job after the Rust test job
to ensure fixtures are always freshly generated.

### Fixture Location

```
testdata/golden/
├── control_message_*.bin    # Framed control messages
├── desktop_frame.bin        # Desktop frame (Zstd encoding)
├── desktop_frame_jpeg.bin   # Desktop frame (Jpeg encoding)
├── handshake_*.bin          # Raw handshake messages
└── ...
```
