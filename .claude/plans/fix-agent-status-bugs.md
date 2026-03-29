# Fix: Agent Online/Offline Status Misreporting

## Context

Agents are falsely shown as "online" when offline and "offline" when online. Root cause analysis found **5 distinct bugs** across all three layers (server, agent, web client) that compound to make device status unreliable.

---

## Bug Summary

| # | Layer | Severity | Description |
|---|-------|----------|-------------|
| 1 | Server | CRITICAL | Reconnection race: old connection's defer deletes NEW connection from sync.Map |
| 2 | Server | HIGH | No startup reconciliation: devices stay "online" in DB after server crash/restart |
| 3 | Agent | CRITICAL | Agent never sends heartbeats (AgentHeartbeat defined but never used) |
| 4 | Agent | MEDIUM | No QUIC transport config: keepalive/idle timeout mismatch with server |
| 5 | Web | HIGH | DeviceList and Dashboard never poll: status stale from initial page load |

---

## Implementation Plan

### Fix 1: Reconnection Race Condition

**File:** [server.go](server/internal/agentapi/server.go#L240-L261)

**Problem:** `s.conns.Store(deviceID, newConn)` overwrites old entry, then old connection's `defer` runs `s.conns.Delete(deviceID)` which removes the NEW connection. Also sets status to offline in DB.

**Fix:** Replace unconditional `s.conns.Delete` with `s.conns.CompareAndDelete(result.DeviceID, ac)`. Only decrement count, set offline status, and send notification if the delete succeeds (i.e., we're still the registered connection). Stream/conn close runs unconditionally.

**Tests:**
- Unit test: `TestAcceptRaceCondition` — simulate two connections for same device, verify old defer doesn't remove new connection
- Update integration test `reconnect_test.go` to verify status stays online during fast reconnect

---

### Fix 2: Startup Reconciliation

**Files:**
- [store.go](server/internal/db/store.go) — add `ResetAllDeviceStatuses(ctx) error` to interface
- [sqlite.go](server/internal/db/sqlite.go) — implement: `UPDATE devices SET status='offline', updated_at=? WHERE status='online'`
- [metrics/store.go](server/internal/metrics/store.go) — add instrumented wrapper
- [main.go](server/cmd/meshserver/main.go#L65-L70) — call after store creation, before QUIC listener starts

**Tests:**
- `TestSQLiteStore_ResetAllDeviceStatuses` — seed online+offline devices, verify all become offline
- `TestSQLiteStore_ResetAllDeviceStatuses_Empty` — no error on empty table

---

### Fix 3: Agent Heartbeats

**File:** [main.rs](agent/crates/mesh-agent/src/main.rs#L447-L522)

**Problem:** The control loop only has `receive_control()` and shutdown signal branches. `AgentHeartbeat` is defined in protocol but never sent.

**Fix:** Add `tokio::time::interval(Duration::from_secs(60))` branch to `tokio::select!`. On tick, send `ControlMessage::AgentHeartbeat { timestamp }`. On send failure, break inner loop to trigger reconnect.

60s interval chosen because server MaxIdleTimeout=90s — heartbeat arrives well within window.

**Tests:**
- Unit test: verify heartbeat frame encoding roundtrips correctly
- Integration: existing server-side `TestAgentConn_HandleHeartbeat` already validates the handler

---

### Fix 4: QUIC Transport Config on Agent

**File:** [main.rs](agent/crates/mesh-agent/src/main.rs#L151-L182)

**Problem:** `build_quic_config` creates `quinn::ClientConfig` with no `TransportConfig`. Server uses MaxIdleTimeout=90s, KeepAlivePeriod=30s. Quinn defaults may differ.

**Fix:** After creating `quinn_config`, set:
```rust
let mut transport = quinn::TransportConfig::default();
transport.max_idle_timeout(Some(Duration::from_secs(90).try_into()?));
transport.keep_alive_interval(Some(Duration::from_secs(30)));
quinn_config.transport_config(Arc::new(transport));
```

**Tests:** Existing QUIC connection tests validate the full path still works.

---

### Fix 5: Web Client Polling

**Files:**
- [DeviceList.tsx](web/src/features/devices/DeviceList.tsx#L16-L19) — add 15s polling interval for `fetchDevices`
- [Dashboard.tsx](web/src/features/dashboard/Dashboard.tsx#L25-L31) — add 15s polling interval for `fetchDevices`

Pattern matches existing polling in [DeviceDetail.tsx](web/src/features/devices/DeviceDetail.tsx#L43-L48).

**Tests:**
- `DeviceList.test.tsx`: verify `fetchDevices` called again after 15s with fake timers
- `Dashboard.test.tsx`: same pattern

---

## Implementation Order

1. **Fix 1** (server race) + **Fix 2** (startup reconciliation) — server-side, no cross-deps
2. **Fix 4** (QUIC config) — must land before Fix 3
3. **Fix 3** (agent heartbeats) — depends on correct QUIC timeouts
4. **Fix 5** (web polling) — independent, can parallel with 2-3

## Verification

1. `make test` — all unit tests pass
2. `make lint` — clippy + go vet + eslint clean
3. Integration scenario: start server, connect agent, kill agent process, verify status flips to offline within ~90s
4. Integration scenario: connect agent, restart server, verify agent reconnects and status returns to online
5. Web: open DeviceList, connect/disconnect agent, verify status updates within 15s without page refresh
