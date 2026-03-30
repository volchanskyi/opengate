# Plan: Linux agent — Terminal + FileManager only, remove tray

## Context
Display detection on Linux is unreliable (WSL2 false positives, CentOS Stream 10 false negatives). Instead of fixing detection, Linux agents will only support Terminal and FileManager. RemoteDesktop/Chat remain for Windows/Mac. The system tray is desktop-only UI and no longer needed for Linux.

## Part 1: Remove tray and IPC infrastructure

### 1a. Delete crate directories
- `agent/crates/mesh-agent-tray/` (entire directory)
- `agent/crates/mesh-agent-ipc/` (entire directory)

### 1b. `agent/Cargo.toml` — workspace config
- Remove `mesh-agent-ipc` from `default-members`
- Remove the comment about mesh-agent-tray / GTK
- Since tray is gone, `members = ["crates/*"]` now works without exclusion — OR keep explicit list without ipc/tray

### 1c. `agent/crates/mesh-agent/Cargo.toml`
- Remove `mesh-agent-ipc = { path = "../mesh-agent-ipc" }` dependency

### 1d. `agent/crates/mesh-agent/src/ipc_server.rs`
- Delete this file entirely

### 1e. `agent/crates/mesh-agent/src/main.rs`
- Remove `mod ipc_server;`
- Remove IPC state init block (lines ~389-407): `ipc_state`, `ipc_server::start()`
- Remove IPC action handling in select! loop (lines ~537-563): the entire `ipc_actions` arm
- Remove IPC connection notifications (lines ~498-500, ~620-622, ~627-629): `handle.notify_connection_changed()`
- Remove IPC cleanup on shutdown (lines ~639-641): `ipc_server::cleanup()`
- Remove all `ipc_handle` and `ipc_actions` variables
- Remove `use mesh_agent_ipc` if present

### 1f. `.github/workflows/ci.yml`
- Delete the entire `rust-tray` job (lines 90-113)
- Remove `rust-tray` from `merge-to-main.needs` (line 900)

### 1g. `Makefile`
- Remove `--exclude mesh-agent-tray` from all targets (build, test-short, test-rust, lint) — it no longer exists

### 1h. `server/internal/api/install-tray.sh`
- Delete this file entirely

### 1i. `server/internal/api/install.sh`
- Remove the tray auto-detect + install section (lines ~220-229)

## Part 2: Linux = Terminal + FileManager only

### 2a. `agent/crates/mesh-agent/src/main.rs` (~line 474-482)
Replace the capabilities block:
```rust
capabilities: vec![
    mesh_protocol::AgentCapability::Terminal,
    mesh_protocol::AgentCapability::FileManager,
],
```
Remove the `has_display()` conditional entirely.

### 2b. `agent/crates/platform-linux/src/lib.rs`
Remove dead display detection code:
- `has_display()` function (lines 31-41)
- `has_display_server_socket()` function (lines 49-85)
- `probe_socket()` function (lines 87-102)
- Related tests: `test_has_display_server_socket_returns_bool`, `test_has_display_true_with_display_env`, `test_has_display_false_without_env_or_sockets`, `test_has_display_true_with_wayland`
- Keep `create_screen_capture()` and its test (still called as fallback in session handler — returns NullCapture safely)

### 2c. `agent/crates/mesh-agent/tests/connection_test.rs`
- Update to expect 2 capabilities (Terminal + FileManager)
- Remove `RemoteDesktop` from the test's `AgentRegister` message (~line 88)
- Update assertion: `assert_eq!(capabilities.len(), 2)` (~line 55)
- Remove `assert!(capabilities.contains(&AgentCapability::RemoteDesktop))` (~line 56)

## Files summary

| Action | File |
|--------|------|
| DELETE dir | `agent/crates/mesh-agent-tray/` |
| DELETE dir | `agent/crates/mesh-agent-ipc/` |
| DELETE file | `agent/crates/mesh-agent/src/ipc_server.rs` |
| DELETE file | `server/internal/api/install-tray.sh` |
| EDIT | `agent/Cargo.toml` |
| EDIT | `agent/crates/mesh-agent/Cargo.toml` |
| EDIT | `agent/crates/mesh-agent/src/main.rs` |
| EDIT | `agent/crates/platform-linux/src/lib.rs` |
| EDIT | `agent/crates/mesh-agent/tests/connection_test.rs` |
| EDIT | `.github/workflows/ci.yml` |
| EDIT | `Makefile` |
| EDIT | `server/internal/api/install.sh` |

## Not changed
- `mesh-protocol`: `AgentCapability::RemoteDesktop` enum stays (used by Windows/Mac, golden tests, benchmarks)
- `platform-windows`: keeps its display detection and screen capture
- Server/web: no changes — already handles missing RemoteDesktop (tabs hidden)
- Golden tests/benchmarks: use RemoteDesktop in fixtures, unrelated to Linux runtime

## Verification
1. `make build` — compiles clean
2. `make test` — all tests pass
3. `make lint` — no clippy warnings, no dead code
4. `make golden` — golden files unchanged
