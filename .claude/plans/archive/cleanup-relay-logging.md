# Cleanup: Remove Debug Logging, Establish Relay Logging Strategy

## Context

The WebSocket relay bug (metrics `statusWriter` missing `http.Hijacker`) took multiple debugging sessions to identify because the relay had **zero operational logging** — no visibility into connection lifecycle, upgrade failures, or pipe errors. After adding temporary `[RELAY-DEBUG]` logs across 3 files (commit `fb70177`) to trace the issue, these logs are now noise and need cleanup. This plan replaces them with a proper, permanent logging strategy consistent with the rest of the codebase.

## Bug Summary (for wiki)

**Root cause:** The metrics middleware's `statusWriter` (`server/internal/metrics/middleware.go`) wrapped `http.ResponseWriter` but did not implement `http.Hijacker`. When `nhooyr.io/websocket.Accept()` attempted to hijack the connection, it could not find `http.Hijacker` in the wrapper chain → upgrade failed → both browser and agent WebSocket connections silently rejected.

**How it was introduced:** The metrics middleware was added in Phase 12 (commit `ea2cfb5` era) and applied globally to the chi router via `r.Use()`. The WebSocket relay route (`/ws/relay/{token}`) was registered on the same router, inheriting all middleware. The `statusWriter` was modeled after common Go examples but omitted `Hijack()` delegation. This went unnoticed because the relay was never used end-to-end until Phase 9 connected the agent through it.

**Fix:** Added `Hijack()` method to `statusWriter` that delegates to the underlying writer (commit `e7c074c`). Also fixed duplicate `WriteHeader` forwarding that caused "superfluous response.WriteHeader" warnings.

**Timeline of contributing factors:**
- Phase 6: Relay created, WebSocket route registered under global middleware — but never exercised
- Phase 9: Agent connected through relay for the first time → bug surfaced
- Phase 12: Metrics middleware added with `statusWriter` lacking `http.Hijacker` — the actual breaking change
- Commit `2812558`: Fixed HTTP timeout killing hijacked WebSocket (real bug, keep)
- Commit `ea2cfb5`: Fixed message boundary splitting (real bug, keep)
- Commit `e7c074c`: Fixed `statusWriter` missing `Hijacker` (the actual root cause)

## 1. Remove All `[RELAY-DEBUG]` Temporary Logs

### 1a. `server/internal/api/wsconn.go`

Remove all `slog` debug logging from `ReadMessage`, `WriteMessage`, `Close`. These are hot-path methods called per-message — logging here is performance-harmful and duplicates what the relay layer can log. Remove the `"log/slog"` import.

**Before:**
```go
func (w *WSConn) ReadMessage() ([]byte, error) {
    msgType, data, err := w.conn.Read(context.Background())
    if err != nil {
        slog.Error("[RELAY-DEBUG] WSConn.ReadMessage failed", ...)
    } else {
        slog.Info("[RELAY-DEBUG] WSConn.ReadMessage OK", ...)
    }
    return data, err
}
```

**After:**
```go
func (w *WSConn) ReadMessage() ([]byte, error) {
    _, data, err := w.conn.Read(context.Background())
    return data, err
}
```

Same treatment for `WriteMessage` (remove if/else logging, keep the `Write` call) and `Close` (remove the `slog.Info`, keep the `Close` call).

The `label` field on `WSConn` struct stays — it's useful for the handler-level logging in `handlers_relay.go` (passed to `NewWSConn`).

### 1b. `server/internal/relay/relay.go`

Remove all `[RELAY-DEBUG]` prefixed logs from `copyMessages` and `pipe`. Replace with **permanent, properly-leveled** structured logs (see Section 2).

### 1c. `server/internal/api/handlers_relay.go`

Remove all `[RELAY-DEBUG]` prefixed logs. Replace with **permanent, properly-leveled** structured logs using the injected `s.logger` (see Section 2).

## 2. Permanent Logging Strategy

### Principles
1. **Use injected logger, not global `slog`** — relay.go currently uses `slog.*` (global). It should accept a `*slog.Logger` to match `agentapi`, `notifications`, `mps`, etc.
2. **Use `slog.Debug` for per-message/high-frequency events** — visible only when `LOG_LEVEL=debug`
3. **Use `slog.Info` for lifecycle events** — session start/end, connection established
4. **Use `slog.Error` for failures** — upgrade failures, pipe errors
5. **No bracket prefixes** — use structured fields (`"component", "relay"`) not `[RELAY-DEBUG]`
6. **Consistent field names** — `token_prefix`, `side`, `error`, `direction`, `bytes`, `msgs_copied`

### 2a. Relay package — inject logger

**File:** `server/internal/relay/relay.go`

Add `logger *slog.Logger` to the `Relay` struct and `NewRelay`:

```go
type Relay struct {
    sessions     sync.Map
    count        atomic.Int64
    logger       *slog.Logger
    OnSessionEnd func(token protocol.SessionToken)
}

func NewRelay(logger *slog.Logger) *Relay {
    return &Relay{logger: logger}
}
```

Update `main.go` to pass logger: `relay.NewRelay(logger)`.

### 2b. Relay permanent logs

**`pipe` function — Info level (lifecycle):**
- Pipe started: `r.logger.Info("relay session started", "token_prefix", tp)`
- Pipe ended: `r.logger.Info("relay session ended", "token_prefix", tp)`

**`copyMessages` function — Error level (failures only):**
- Read error: `r.logger.Error("relay read error", "direction", direction, "token_prefix", tp, "msgs_copied", count, "error", err)`
- Write error: `r.logger.Error("relay write error", "direction", direction, "token_prefix", tp, "msgs_copied", count, "error", err)`

**No per-message Info logging.** The old "copyMessages forwarded" log at Info level is removed. Per-message logging is only useful during active debugging and belongs at Debug level if added back.

`copyMessages` becomes a method on `Relay` (so it can access `r.logger`).

### 2c. Handler permanent logs

**File:** `server/internal/api/handlers_relay.go`

Keep existing `s.logger` usage but clean up messages. All use `s.logger` (already injected):

| Event | Level | Message | Fields |
|-------|-------|---------|--------|
| Token not found | `Warn` | `"relay token not found"` | `token_prefix` |
| Token validation error | `Error` | `"relay token validation error"` | `token_prefix`, `error` |
| Invalid side param | `Warn` | `"relay invalid side param"` | `token_prefix`, `side_param` |
| WebSocket upgrade failed | `Error` | `"relay websocket upgrade failed"` | `token_prefix`, `side` |
| Registration failed | `Error` | `"relay register failed"` | `token_prefix`, `error` |
| WaitForPeer failed | `Error` | `"relay wait for peer failed"` | `token_prefix`, `error` |
| Handler connected | `Info` | `"relay session connected"` | `token_prefix`, `side` |
| Handler disconnected | `Info` | `"relay session disconnected"` | `token_prefix`, `side` |

**Removed** (too verbose for Info, adds no operational value):
- "upgrading WebSocket" — logged by RequestLogger middleware already
- "registered, waiting for peer" — internal state, not operationally useful
- "peer connected, blocking on ctx.Done()" — internal flow detail
- "ctx.Done() fired" — covered by "relay session disconnected"

## 3. Update Wiki

**File:** `/home/ivan/opengate.wiki/Architecture.md`

Add a short "Observability" or "Logging" subsection under "WebSocket Relay" (after line 111) documenting:
- Relay session lifecycle logged at Info level
- Pipe read/write errors logged at Error level
- Per-message tracing available via `LOG_LEVEL=debug`
- Structured fields: `token_prefix`, `side`, `direction`, `error`

## 4. Files to Modify

| File | Action |
|------|--------|
| `server/internal/api/wsconn.go` | Remove all `slog` debug logs, remove `"log/slog"` import |
| `server/internal/relay/relay.go` | Inject logger, replace `[RELAY-DEBUG]` with permanent structured logs, make `copyMessages` a method |
| `server/internal/api/handlers_relay.go` | Replace `[RELAY-DEBUG]` with clean permanent logs |
| `server/cmd/meshserver/main.go` | Pass `logger` to `relay.NewRelay(logger)` |
| `server/internal/relay/relay_test.go` | Update `NewRelay()` calls to pass test logger |
| `opengate.wiki/Architecture.md` | Add relay observability note |

## 5. Verification

1. `make test` — all tests pass (relay_test.go updated for new `NewRelay` signature)
2. `make lint` — clean (no unused imports, no `[RELAY-DEBUG]` strings remaining)
3. `grep -r "RELAY-DEBUG" server/` — zero matches
4. `grep -r "slog\." server/internal/api/wsconn.go` — zero matches
5. Deploy and verify: relay logs show clean `"relay session started"` / `"relay session ended"` at Info level, no per-message spam
