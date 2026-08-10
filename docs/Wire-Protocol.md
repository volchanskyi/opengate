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

The handshake type bytes are `0x10` (`ServerHello`), `0x11`
(`AgentHello`), `0x14` (`SkipAuth`), and `0x15` (`ExpectHash`). Both decoders
reject `0x12` and `0x13`. The canonical constants live in
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
| `AgentHealthSummary` | Agent → Server | `ts`, `tenant_id`, `node_anomaly_rate`, `per_family_rates`, `recent_bitmask`, `sampler_ver`, `model_ver` |
| `AgentMetricWindow` | Agent → Server | `ts`, `tenant_id`, `dims` |
| `ProcessReport` | Agent → Server | `ts`, `tenant_id`, `top_n` |
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
| `RequestDeviceLogs` | Server → Agent | `log_level`, `time_from`, `time_to`, `search`, `log_offset`, `log_limit`, `source`, `unit` |
| `DeviceLogsResponse` | Agent → Server | `log_entries` (Vec\<LogEntry\>), `total_count`, `has_more` |
| `DeviceLogsError` | Agent → Server | `error` |
| `RequestHealthWindow` | Server → Agent | `since_ts`, `limit` |
| `HealthWindowResponse` | Agent → Server | `summaries` |
| `DiscoveryReport` | Agent → Server | `ts`, `tenant_id`, `ports`, `services`, `db_engines`, `containers`, `packages`, `truncated` |
| `SetMaintenanceMode` | Server → Agent | `enabled` |
| `MaintenanceApplied` | Agent → Server | `enabled` |

The Edge Sentinel telemetry variants are ingested by the server when received.
The agent sampler runs on every device
([ADR-056](./adr/ADR-056-device-maintenance-mode.md)); it pauses only while the
device is in maintenance mode. Server ingest ignores payload `tenant_id` for
authorization, resolves the device's authoritative tenant after handshake,
applies a telemetry payload cap and interval floor, and drops/counts telemetry
when the bounded persistence path is saturated. The source-of-truth payload definitions are the Rust
[`ControlMessage`](../agent/crates/mesh-protocol/src/control.rs) enum and Go
[`ControlMessage`](../server/internal/protocol/control.go) flat struct; the
store decision is [ADR-044](./adr/ADR-044-edge-sentinel-server-telemetry-ingest.md).

Live host metrics reuse `AgentMetricWindow`: the sampler folds its 1 s samples
into a 60 s window and emits one window per minute over a bounded channel that
drops under pressure, so a burst never backpressures the control stream. Each
`dims` entry is a host-resource series (`cpu.total`, `mem.used_percent`,
`disk.used_percent`, `net.rx_bps`, `net.tx_bps`, `disk.mounts_critical`, and the
five `stall.*` vitals) carrying that window's average, and the four where a
within-minute spike is the signal carry the maximum too, under the same name
suffixed `.max` (`cpu.total.max`, `mem.used_percent.max`, `net.rx_bps.max`,
`net.tx_bps.max`). A minute's average hides a five-second freeze; its maximum is
what recovers it. The net dims are primary-interface throughput in bytes/second
(rounded to whole bytes so they stay on the lossless integer path). The server
writes only these fifteen names — a dim outside the vocabulary is dropped and
counted, so central cardinality is a property of the contract rather than of what
an agent sends. The two disk dims are a per-mount
reduction ([`sampler.rs`](../agent/crates/mesh-agent-core/src/ml/sampler.rs)):
**`disk.used_percent` is the fullest mount**, not a pooled average over every
mount's bytes, and `disk.mounts_critical` counts the mounts at or above the
critical-usage threshold (`MOUNT_CRITICAL_PERCENT` in the same file). Every mount
the platform lists takes part, network shares and removable media included. A
mount reporting no capacity takes part in neither number, and a host with no
measurable mount ships neither dim rather than a zero, because a dim a sample
could not read is absent from the window rather than substituted. The five
`stall.*` dims are the share of the last 60 s that tasks spent stalled on CPU,
memory and I/O, read from the kernel's own pressure accounting
([`pressure.rs`](../agent/crates/mesh-agent-core/src/ml/pressure.rs)); a host
whose kernel publishes no such accounting ships none of them, for the same
reason. Because the kernel has already averaged each of those readings over
60 s, a stall dim carries the window's latest reading where the other dims carry
their mean. The 60 s fold matches reconnect-backfill's roll-up exactly, values
and maxima alike, so a live point and a later gap-filled point for the same
`(dim, ts)` land in one series. On the on-demand log query, `RequestDeviceLogs.source` selects the log
source (`host` resolves the platform system log, journald on Linux; empty or
`self` reads the agent's own files) and `unit` narrows host logs to one emitting
unit; `DeviceLogsResponse.available_units` enumerates the source's distinct units
for the UI unit dropdown. The `source` vocabulary is the extension point for a
further platform's log reader and is wider than any one agent implements — an
agent that names a source it has no reader for gets `DeviceLogsError` naming that
source, never an empty page and never another source's records.

`DiscoveryReport` carries a non-intrusive, read-only host profile: listening
ports (transport, port, owning process basename), host services (systemd unit
name + run state), database engines inferred from listening
ports (engine family + port, no probe), containers from a local runtime
(runtime, image, name, state), and installed packages (name, version). Each
category is per-device bounded on the agent, and `truncated` is set when any hit
its cap; the payload never carries a bound address, connection string, or
credential. The agent's discovery task profiles on a long interval and forwards a
report over a bounded channel **only when the profile changed** since the last one
shipped — so a
steady host is silent and a burst never backpressures the control stream. The
server assigns the authoritative tenant, so the agent leaves `tenant_id`
empty.

`SetMaintenanceMode` carries the server's desired maintenance state for the
device (`enabled`), pushed on the Active↔Maintenance transition and, for a device
already in maintenance, on reconnect. The agent applies it — suppressing the
sampler, discovery, and alert evaluation while `enabled` is true —
and echoes `MaintenanceApplied { enabled }` as its applied-state report. Both
carry an explicit boolean, so a `false` (resume) is distinct from an absent field.
`SetMaintenanceMode` is universal control and is not capability-gated; the agent
resets to Active on every registration and suppresses only when the server pushes
`true`. See [ADR-056](./adr/ADR-056-device-maintenance-mode.md).

### Capabilities

`AgentRegister.capabilities` is the negotiation surface for additive
server-to-agent control messages. The server must not send a new
server-to-agent variant unless the connected agent advertised the matching
capability. Current additive gates:

| Capability | Gates |
|------------|-------|
| `HardwareInventory` | `RequestHardwareReport` |
| `DeviceLogs` | `RequestDeviceLogs` |
| `HealthWindow` | `RequestHealthWindow` |
| `Backfill` | `GrantBackfill`, `DeferBackfill`, `MetricBackfillAck`, `RequestLocalHistory` |

`Discovery` gates no server-to-agent message; the agent advertises it to signal
that it emits `DiscoveryReport` inventory (so the server knows to ingest it).

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

The agent parses daily-rotated log files written by `tracing-subscriber` and returns matching entries. The server redacts known secrets from the bounded response and streams it straight back to the requesting administrator; nothing is persisted centrally (see [ADR-046](adr/ADR-046-edge-sentinel-raw-log-broker.md)).

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
