# Device Logs Feature — Implementation Plan

## Context

The agent writes daily-rotated logs to `/var/log/mesh-agent/agent.log` (Linux) using `tracing-appender::rolling::daily`, but there's no mechanism to retrieve them through the UI. This feature adds on-demand log retrieval via the existing QUIC control path, following the same pattern as hardware reports: server requests logs from agent, caches in DB, serves via REST.

**Why control path (not WebSocket streaming):**
- Logs are text (small, compress well in msgpack)
- Historical logs are static (cache-friendly)
- Follows existing RequestHardwareReport/HardwareReport pattern exactly
- Server can cache in DB for offline access
- No streaming complexity needed

---

## Architecture Overview

```
Browser                    Server                      Agent
  |                          |                           |
  | GET /devices/{id}/logs   |                           |
  |------------------------->|                           |
  |                          |-- check DB cache -------->|
  |                          |   (miss or stale)         |
  |                          |                           |
  |                          |-- RequestDeviceLogs ----->|
  |  <-- 202 Accepted -------|   (QUIC control msg)      |
  |                          |                           |
  |                          |                           |-- read log files
  |                          |                           |-- parse & filter
  |                          |                           |
  |                          |<-- DeviceLogsResponse ----|
  |                          |   (or DeviceLogsError)    |
  |                          |-- store in device_logs -->|
  |                          |                           |
  | GET /devices/{id}/logs   |                           |
  |------------------------->|                           |
  |                          |-- check DB cache (hit) -->|
  |  <-- 200 + LogEntries ---|                           |
```

---

## Phase 1: Wire Protocol (mesh-protocol + Go protocol)

### 1.1 Rust — New Types

**File:** `agent/crates/mesh-protocol/src/types/mod.rs` — add `pub mod log_entry;`

**New file:** `agent/crates/mesh-protocol/src/types/log_entry.rs`
```rust
/// A single parsed log entry from the agent's log files.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct LogEntry {
    pub timestamp: String,   // ISO 8601 string
    pub level: String,       // "TRACE", "DEBUG", "INFO", "WARN", "ERROR"
    pub target: String,      // module path, e.g. "mesh_agent::connection"
    pub message: String,     // log message text
}
```

**File:** `agent/crates/mesh-protocol/src/control.rs` — add 3 new variants:
```rust
/// Server requests the agent to collect and send log entries.
RequestDeviceLogs {
    log_level: String,     // filter: "" (all), "INFO", "WARN", "ERROR"
    time_from: String,     // ISO 8601, "" = no lower bound
    time_to: String,       // ISO 8601, "" = no upper bound
    search: String,        // keyword substring filter, "" = none
    log_offset: u32,       // pagination offset (line count)
    log_limit: u32,        // max entries to return (e.g. 100)
},

/// Agent responds with log entries.
DeviceLogsResponse {
    log_entries: Vec<LogEntry>,
    total_count: u32,      // total matching entries (for pagination)
    has_more: bool,
},

/// Agent reports a log retrieval error.
DeviceLogsError {
    error: String,
},
```

### 1.2 Go — Protocol Constants + Fields

**File:** `server/internal/protocol/control.go`

Add constants:
```go
MsgRequestDeviceLogs  ControlMessageType = "RequestDeviceLogs"
MsgDeviceLogsResponse ControlMessageType = "DeviceLogsResponse"
MsgDeviceLogsError    ControlMessageType = "DeviceLogsError"
```

Add fields to `ControlMessage` struct:
```go
// DeviceLogs
LogLevel   string     `msgpack:"log_level,omitempty"`
TimeFrom   string     `msgpack:"time_from,omitempty"`
TimeTo     string     `msgpack:"time_to,omitempty"`
Search     string     `msgpack:"search,omitempty"`
LogOffset  uint32     `msgpack:"log_offset,omitempty"`
LogLimit   uint32     `msgpack:"log_limit,omitempty"`
LogEntries []LogEntry `msgpack:"log_entries,omitempty"`
TotalCount uint32     `msgpack:"total_count,omitempty"`
HasMore    *bool      `msgpack:"has_more,omitempty"`
```

Note: `AckError` already maps to msgpack `"error"`, which `DeviceLogsError.error` also uses. The Go flat struct naturally shares this field — no collision since only one message type is populated at a time.

Add `LogEntry` struct:
```go
type LogEntry struct {
    Timestamp string `msgpack:"timestamp"`
    Level     string `msgpack:"level"`
    Target    string `msgpack:"target"`
    Message   string `msgpack:"message"`
}
```

### 1.3 Golden File Tests

**File:** `agent/crates/mesh-protocol/tests/golden_test.rs` — add 3 tests:
- `golden_control_frame_request_device_logs` — with populated filter fields
- `golden_control_frame_device_logs_response` — with sample LogEntry vec
- `golden_control_frame_device_logs_error` — with error string

**File:** `server/internal/protocol/golden_test.go` — add 3 matching verification tests

**Run:** `make golden` to generate .bin files and verify cross-language compat

---

## Phase 2: Agent Log Collection

### 2.1 Log File Reader Module

**New file:** `agent/crates/mesh-agent/src/logs.rs`

Responsibilities:
- Discover log files in `/var/log/mesh-agent/` (glob `agent.log*`)
- Sort by date (newest first for recent logs, oldest first for time-range queries)
- Parse tracing-subscriber default format: `2026-04-01T12:00:00.000000Z  INFO mesh_agent: message here`
- Apply filters: level, time range, keyword search
- Return paginated results with total count

**Key design decisions:**
- **Manual string parsing** (no regex crate dependency): split on whitespace to extract timestamp, level, target, message
- **Safety limits**: scan max 10,000 lines per request, max 7 log files (1 week of dailies)
- **Memory efficient**: read line-by-line, filter early, collect only matching entries
- **Handle multi-line log entries**: lines without a timestamp prefix are continuation of previous entry (append to message)

```rust
pub struct LogCollector {
    log_dir: PathBuf,
}

pub struct LogFilter {
    pub level: Option<String>,
    pub time_from: Option<String>,
    pub time_to: Option<String>,
    pub search: Option<String>,
    pub offset: u32,
    pub limit: u32,
}

pub struct LogResult {
    pub entries: Vec<LogEntry>,
    pub total_count: u32,
    pub has_more: bool,
}

impl LogCollector {
    pub fn collect(&self, filter: &LogFilter) -> Result<LogResult> { ... }
}
```

**Parsing logic for tracing-subscriber format:**
```
2026-04-01T12:34:56.789012Z  INFO mesh_agent::connection: connected to server
^                            ^    ^                       ^
timestamp                    level target                 message
```
- Split on first two whitespace groups: timestamp, level
- After level, take until `:` for target, rest is message
- Level filtering: parse to enum, compare severity
- Time filtering: string comparison works for ISO 8601

### 2.2 Agent Handler

**File:** `agent/crates/mesh-agent/src/main.rs` — add match arm in control message handler:

```rust
Ok(ControlMessage::RequestDeviceLogs { log_level, time_from, time_to, search, log_offset, log_limit }) => {
    info!("device logs requested by server");
    let collector = LogCollector::new(PathBuf::from(LOG_DIR));
    let filter = LogFilter {
        level: if log_level.is_empty() { None } else { Some(log_level) },
        time_from: if time_from.is_empty() { None } else { Some(time_from) },
        time_to: if time_to.is_empty() { None } else { Some(time_to) },
        search: if search.is_empty() { None } else { Some(search) },
        offset: log_offset,
        limit: log_limit,
    };
    match collector.collect(&filter) {
        Ok(result) => {
            let msg = ControlMessage::DeviceLogsResponse {
                log_entries: result.entries,
                total_count: result.total_count,
                has_more: result.has_more,
            };
            if let Err(e) = conn.send_control(msg).await {
                warn!(error = %e, "failed to send device logs response");
            }
        }
        Err(e) => {
            let msg = ControlMessage::DeviceLogsError {
                error: e.to_string(),
            };
            if let Err(e) = conn.send_control(msg).await {
                warn!(error = %e, "failed to send device logs error");
            }
        }
    }
}
```

### 2.3 Agent Tests

- Unit tests in `logs.rs`: parse various tracing formats, filter by level/time/keyword, pagination, multi-line entries, missing log dir, empty files
- Integration test: write sample log file, collect with filters, verify output

---

## Phase 3: Server — Database + Agent Handler

### 3.1 Database Migration

**New file:** `server/internal/db/migrations/010_device_logs.up.sql`
```sql
CREATE TABLE IF NOT EXISTS device_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp TEXT NOT NULL,
    level TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_device_logs_device_id ON device_logs(device_id);
CREATE INDEX idx_device_logs_level ON device_logs(device_id, level);
CREATE INDEX idx_device_logs_timestamp ON device_logs(device_id, timestamp);
```

**New file:** `server/internal/db/migrations/010_device_logs.down.sql`
```sql
DROP TABLE IF EXISTS device_logs;
```

**Design: Individual rows vs JSON blob:**
Individual rows chosen because:
- SQL-level filtering by level, time range, keyword (LIKE) is efficient with indexes
- Pagination via OFFSET/LIMIT is natural
- No need to deserialize a blob for every query
- Retention cleanup is simple (DELETE WHERE fetched_at < ?)

### 3.2 Database Models

**File:** `server/internal/db/models.go` — add:
```go
type DeviceLogEntry struct {
    ID        int64    `json:"id"`
    DeviceID  DeviceID `json:"device_id"`
    Timestamp string   `json:"timestamp"`
    Level     string   `json:"level"`
    Target    string   `json:"target"`
    Message   string   `json:"message"`
    FetchedAt time.Time `json:"fetched_at"`
}
```

### 3.3 Store Interface

**File:** `server/internal/db/store.go` — add:
```go
// Device Logs
UpsertDeviceLogs(ctx context.Context, deviceID DeviceID, entries []DeviceLogEntry) error
QueryDeviceLogs(ctx context.Context, deviceID DeviceID, filter LogFilter) ([]DeviceLogEntry, int, error)
HasRecentLogs(ctx context.Context, deviceID DeviceID, maxAge time.Duration) (bool, error)
```

**LogFilter struct in `server/internal/db/models.go`:**
```go
type LogFilter struct {
    Level  string
    From   string
    To     string
    Search string
    Offset int
    Limit  int
}
```

### 3.4 SQLite Implementation

**File:** `server/internal/db/sqlite.go` — add methods:

- `UpsertDeviceLogs`: DELETE existing logs for device, INSERT new batch (transaction)
- `QueryDeviceLogs`: SELECT with dynamic WHERE clauses (level, timestamp range, message LIKE), ORDER BY timestamp DESC, LIMIT/OFFSET. Returns entries + total count.
- `HasRecentLogs`: SELECT 1 WHERE fetched_at > (now - maxAge) — for cache freshness check

### 3.5 AgentConn — Send & Receive

**File:** `server/internal/agentapi/conn.go`

Add send method:
```go
func (a *AgentConn) SendRequestDeviceLogs(ctx context.Context, filter db.LogFilter) error {
    return a.sendControl(&protocol.ControlMessage{
        Type:      protocol.MsgRequestDeviceLogs,
        LogLevel:  filter.Level,
        TimeFrom:  filter.From,
        TimeTo:    filter.To,
        Search:    filter.Search,
        LogOffset: uint32(filter.Offset),
        LogLimit:  uint32(filter.Limit),
    })
}
```

Add handler in control message dispatch:
```go
case protocol.MsgDeviceLogsResponse:
    return a.handleDeviceLogsResponse(ctx, msg)
case protocol.MsgDeviceLogsError:
    a.logger.Warn("device logs error from agent", "error", msg.AckError)
    return nil
```

```go
func (a *AgentConn) handleDeviceLogsResponse(ctx context.Context, msg *protocol.ControlMessage) error {
    entries := make([]db.DeviceLogEntry, len(msg.LogEntries))
    for i, le := range msg.LogEntries {
        entries[i] = db.DeviceLogEntry{
            DeviceID:  a.DeviceID,
            Timestamp: le.Timestamp,
            Level:     le.Level,
            Target:    le.Target,
            Message:   le.Message,
        }
    }
    if err := a.store.UpsertDeviceLogs(ctx, a.DeviceID, entries); err != nil {
        return fmt.Errorf("upsert device logs: %w", err)
    }
    a.logger.Debug("device logs stored", "device_id", a.DeviceID, "count", len(entries))
    return nil
}
```

---

## Phase 4: REST API

### 4.1 OpenAPI Spec

**File:** `api/openapi.yaml` — add endpoint and schemas:

```yaml
/api/v1/devices/{id}/logs:
  get:
    operationId: getDeviceLogs
    summary: Get log entries for a device
    security:
      - bearerAuth: []
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
      - name: level
        in: query
        schema:
          type: string
          enum: ["", "TRACE", "DEBUG", "INFO", "WARN", "ERROR"]
      - name: from
        in: query
        schema:
          type: string
          format: date-time
      - name: to
        in: query
        schema:
          type: string
          format: date-time
      - name: search
        in: query
        schema:
          type: string
      - name: offset
        in: query
        schema:
          type: integer
          default: 0
      - name: limit
        in: query
        schema:
          type: integer
          default: 100
          maximum: 500
    responses:
      "200":
        description: Log entries
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/DeviceLogsResponse"
      "202":
        description: Log retrieval requested, poll again shortly
      "401":
        description: Unauthorized
      "403":
        description: Forbidden
      "404":
        description: Device not found or logs unavailable
```

Schema:
```yaml
DeviceLogsResponse:
  type: object
  required: [entries, total, has_more]
  properties:
    entries:
      type: array
      items:
        $ref: "#/components/schemas/DeviceLogEntry"
    total:
      type: integer
    has_more:
      type: boolean

DeviceLogEntry:
  type: object
  required: [timestamp, level, target, message]
  properties:
    timestamp:
      type: string
    level:
      type: string
    target:
      type: string
    message:
      type: string
```

### 4.2 Code Generation

Run `oapi-codegen` to regenerate Go handlers (same as existing build process).

### 4.3 HTTP Handler

**File:** `server/internal/api/handlers_devices.go` — add:

```go
func (s *Server) GetDeviceLogs(ctx context.Context, request GetDeviceLogsRequestObject) (GetDeviceLogsResponseObject, error) {
    device, err := s.store.GetDevice(ctx, request.Id)
    if err != nil {
        if errors.Is(err, db.ErrNotFound) {
            return GetDeviceLogs404JSONResponse{Error: "device not found"}, nil
        }
        return nil, err
    }

    if !s.isGroupOwner(ctx, device.GroupID) {
        return GetDeviceLogs403JSONResponse{Error: msgForbidden}, nil
    }

    filter := db.LogFilter{
        Level:  derefStr(request.Params.Level),
        From:   derefStr(request.Params.From),
        To:     derefStr(request.Params.To),
        Search: derefStr(request.Params.Search),
        Offset: derefInt(request.Params.Offset, 0),
        Limit:  derefInt(request.Params.Limit, 100),
    }

    // Check if we have recent cached logs (5-minute TTL)
    hasRecent, err := s.store.HasRecentLogs(ctx, request.Id, 5*time.Minute)
    if err != nil {
        return nil, err
    }

    if hasRecent {
        entries, total, err := s.store.QueryDeviceLogs(ctx, request.Id, filter)
        if err != nil {
            return nil, err
        }
        return GetDeviceLogs200JSONResponse(deviceLogsToAPI(entries, total, filter)), nil
    }

    // No recent cache — request from agent if online
    ac := s.agents.GetAgent(request.Id)
    if ac == nil {
        // Agent offline — try serving stale cache if any exists
        entries, total, err := s.store.QueryDeviceLogs(ctx, request.Id, filter)
        if err != nil || total == 0 {
            return GetDeviceLogs404JSONResponse{Error: "logs not available — device offline"}, nil
        }
        return GetDeviceLogs200JSONResponse(deviceLogsToAPI(entries, total, filter)), nil
    }

    if err := ac.SendRequestDeviceLogs(ctx, filter); err != nil {
        return nil, err
    }
    return GetDeviceLogs202Response{}, nil
}
```

**Cache strategy:**
- 5-minute TTL: if logs were fetched within 5 minutes, serve from DB
- Stale cache served when agent offline (better than nothing)
- Each fetch from agent replaces previous cached entries (full replace, not append)

---

## Phase 5: Frontend

### 5.1 Types (auto-generated)

After OpenAPI codegen, types will appear in `web/src/types/api.d.ts`:
```typescript
DeviceLogsResponse: {
  entries: DeviceLogEntry[];
  total: number;
  has_more: boolean;
}
DeviceLogEntry: {
  timestamp: string;
  level: string;
  target: string;
  message: string;
}
```

### 5.2 Zustand Store

**File:** `web/src/state/device-store.ts` — add state and action:

```typescript
// State
logs: DeviceLogsResponse | null;
logsLoading: boolean;

// Action
fetchLogs: (id: string, params?: { level?: string; from?: string; to?: string; search?: string; offset?: number; limit?: number }) => Promise<void>;
```

Pattern: same as `fetchHardware` — 202 triggers retry after 3 seconds.

### 5.3 DeviceLogs Component

**New file:** `web/src/features/devices/DeviceLogs.tsx`

Structure:
```
┌─────────────────────────────────────────────────┐
│ Logs                                   [Fetch]  │
│                                                  │
│ ┌──────┐ ┌──────────────────────┐ ┌───────────┐│
│ │Level▾│ │🔍 Search keyword...  │ │From│ │To  ││
│ └──────┘ └──────────────────────┘ └───────────┘│
│                                                  │
│ ┌──────────────────────────────────────────────┐│
│ │ 2026-04-01T12:00:00Z  INFO  connected to srv ││
│ │ 2026-04-01T12:00:01Z  WARN  slow heartbeat   ││
│ │ 2026-04-01T12:00:02Z  ERROR connection lost   ││
│ │ ...                                           ││
│ └──────────────────────────────────────────────┘│
│                                                  │
│ Showing 1-100 of 342            [Load More]     │
│                                                  │
│ ⓘ Cached 3 minutes ago                         │
└─────────────────────────────────────────────────┘
```

**Key UI details:**
- **Level color coding** (monospace font, Tailwind classes):
  - ERROR: `text-red-400`
  - WARN: `text-yellow-400`
  - INFO: `text-blue-400`
  - DEBUG: `text-gray-400`
  - TRACE: `text-gray-500`
- **Filter bar**: level dropdown, search input (debounced 300ms), time range inputs (date-time-local)
- **Log viewer**: scrollable `max-h-96 overflow-y-auto` area, `font-mono text-xs`
- **Pagination**: "Load More" button increments offset by limit
- **States**: loading spinner, "No logs available" empty state, "Device offline" message
- **Fetch button**: triggers `fetchLogs(id)`, disabled while loading

### 5.4 Integration into DeviceDetail

**File:** `web/src/features/devices/DeviceDetail.tsx`

Add `<DeviceLogs />` component after the Hardware section (before action buttons), following the same conditional pattern:

```tsx
{/* After hardware section, before action buttons */}
<DeviceLogs deviceId={device.id} status={device.status} />
```

The component manages its own state (fetch on user click, not on page load — logs are on-demand only).

---

## Phase 6: Tests

### 6.1 Rust Tests
- `agent/crates/mesh-protocol/tests/golden_test.rs` — 3 new golden tests
- `agent/crates/mesh-agent/src/logs.rs` — unit tests:
  - Parse standard tracing format
  - Parse multi-line entries
  - Filter by level (INFO only, WARN+ERROR)
  - Filter by time range
  - Filter by keyword search
  - Pagination (offset/limit)
  - Empty log dir
  - Permission denied (graceful error)

### 6.2 Go Tests
- `server/internal/protocol/golden_test.go` — 3 new golden verification tests
- `server/internal/db/sqlite_test.go` — UpsertDeviceLogs, QueryDeviceLogs with filters
- `server/internal/api/handlers_devices_test.go` — GetDeviceLogs handler tests:
  - 200: cached data available
  - 202: no cache, agent online → triggers request
  - 404: no cache, agent offline
  - 200: stale cache served when agent offline
  - 403: wrong group ownership

### 6.3 Frontend Tests
- `web/src/features/devices/DeviceLogs.test.tsx` — component tests:
  - Renders filter bar and fetch button
  - Displays log entries with color coding
  - Shows loading state
  - Shows empty state
  - Pagination "Load More" works

---

## Phase 7: Wiki & Project Files

- Update `.claude/phases.md` — mark "Device Logs" as completed
- Update `.claude/decisions.md` — add ADR 011 for device logs architecture
- Update wiki: add Device Logs section to API Reference and Architecture pages

---

## Implementation Order

1. Wire protocol (Rust types + Go types + golden tests) — `make golden`
2. Agent log collector (`logs.rs`) + unit tests — `make test`
3. DB migration + models + store methods + tests — `make test`
4. AgentConn send/receive handlers + tests — `make test`
5. OpenAPI spec + codegen + REST handler + tests — `make test`
6. Frontend store + DeviceLogs component + tests — `make test`
7. Integration: wire everything together, `make build && make test && make lint`
8. E2E smoke test (manual or Playwright if feasible)
9. Wiki + project file updates

---

## Files to Create
- `agent/crates/mesh-protocol/src/types/log_entry.rs`
- `agent/crates/mesh-agent/src/logs.rs`
- `server/internal/db/migrations/010_device_logs.up.sql`
- `server/internal/db/migrations/010_device_logs.down.sql`
- `web/src/features/devices/DeviceLogs.tsx`
- `web/src/features/devices/DeviceLogs.test.tsx`

## Files to Modify
- `agent/crates/mesh-protocol/src/types/mod.rs` — add log_entry module
- `agent/crates/mesh-protocol/src/control.rs` — 3 new variants + LogEntry import
- `agent/crates/mesh-protocol/src/lib.rs` — re-export LogEntry if needed
- `agent/crates/mesh-protocol/tests/golden_test.rs` — 3 golden tests
- `agent/crates/mesh-agent/src/main.rs` — handler match arm + mod logs
- `server/internal/protocol/control.go` — 3 constants + 7 fields + LogEntry struct
- `server/internal/protocol/golden_test.go` — 3 golden verification tests
- `server/internal/db/models.go` — DeviceLogEntry + LogFilter
- `server/internal/db/store.go` — 3 interface methods
- `server/internal/db/sqlite.go` — 3 method implementations
- `server/internal/agentapi/conn.go` — SendRequestDeviceLogs + handleDeviceLogsResponse
- `server/internal/api/handlers_devices.go` — GetDeviceLogs handler
- `server/internal/api/converters.go` — deviceLogsToAPI converter
- `api/openapi.yaml` — endpoint + schemas
- `web/src/state/device-store.ts` — logs state + fetchLogs action
- `web/src/features/devices/DeviceDetail.tsx` — integrate DeviceLogs component

## Verification
1. `make golden` — cross-language protocol compatibility
2. `make test` — all unit + integration tests pass
3. `make lint` — clippy + go vet + eslint clean
4. `make build` — full build succeeds
5. Manual test: open device detail → click Fetch Logs → see log entries with filtering
