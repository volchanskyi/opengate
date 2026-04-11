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

The handshake uses **raw binary encoding** (not MessagePack) and occurs before any framed messages. The server initiates:

```
Server                               Agent
  │                                    │
  │──── ServerHello (0x10) ───────────►│
  │◄─── AgentHello  (0x11) ────────────│
  │                                    │
  │     ── framed MessagePack ──       │
```

Handshake type bytes range from `0x10` to `0x15`.

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

This catches any encoding drift between the two codecs. The CI pipeline sequences the golden verification job after the Rust test job to ensure fixtures are always freshly generated.

### Fixture Location

```
testdata/golden/
├── control_message_*.bin    # Framed control messages
├── desktop_frame.bin        # Desktop frame (Zstd encoding)
├── desktop_frame_jpeg.bin   # Desktop frame (Jpeg encoding)
├── handshake_*.bin          # Raw handshake messages
└── ...
```
