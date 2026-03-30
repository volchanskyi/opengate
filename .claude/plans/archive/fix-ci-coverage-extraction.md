# Phase 4: Platform-Specific Agent Code

## Context

The plan document's Phase 4 implements platform abstractions for screen capture, input injection, and service lifecycle. The platform crates (`platform-linux`, `platform-windows`) are currently empty stubs. This phase defines the traits in `mesh-agent-core` (so both platform crates share them), adds input wire types to `mesh-protocol`, and implements Linux runtime detection + systemd lifecycle + null/X11 capture backends.

**Environment constraint:** Dev/CI is Linux WSL2 — no X11 display, no Windows. X11 code is feature-gated; Windows implementations are `#[cfg(windows)]` stubs that compile on Linux.

## Key Design Decision: async trait object safety

`ScreenCapture::next_frame` is async, but factory functions return `Box<dyn ScreenCapture>`. Native `async fn` in traits is NOT object-safe. Solution: use `async_trait` crate (only for this one trait). The existing `ControlStream` avoids this because it uses generics, not `dyn`.

---

## Implementation Steps (TDD order)

### Step 1: Add input wire types to `mesh-protocol`

**File:** `agent/crates/mesh-protocol/src/types.rs`

Add `KeyCode` enum (`#[non_exhaustive]`, ~50 variants: letters, digits, modifiers, nav, function keys, punctuation, numpad, special), `KeyEvent { key: KeyCode, pressed: bool }`, `MouseButton` enum (`#[non_exhaustive]`: Left, Right, Middle, Back, Forward). All derive `Serialize, Deserialize, Debug, Clone, PartialEq, Eq`.

**File:** `agent/crates/mesh-protocol/src/lib.rs` — re-export new types.

**Tests first:**
- `test_key_event_msgpack_roundtrip` — encode/decode KeyEvent
- `test_mouse_button_msgpack_roundtrip` — encode/decode MouseButton
- `test_key_code_all_variants_serializable` — iterate all variants, roundtrip each

### Step 2: Add platform traits + null impls to `mesh-agent-core`

**New file:** `agent/crates/mesh-agent-core/src/platform.rs`

**Add workspace dep:** `async-trait = "0.1"` in root `Cargo.toml`, add to `mesh-agent-core/Cargo.toml`.

Types and traits:
- `RawFrame { width: u32, height: u32, data: Vec<u8> }` — BGRA pixels
- `CaptureError` (`#[non_exhaustive]`): `NoDisplay`, `Backend(String)`, `Timeout`
- `InputError` (`#[non_exhaustive]`): `NotAvailable`, `Backend(String)`
- `ScreenCapture` trait (`#[async_trait]`): `async fn next_frame(&mut self) -> Result<RawFrame, CaptureError>`, `fn resolution(&self) -> (u32, u32)`
- `InputInjector` trait: `fn inject_key`, `fn inject_mouse_move`, `fn inject_mouse_button`, `fn is_available`
- `ServiceLifecycle` trait: `fn notify_ready`, `fn notify_reloading`, `fn notify_stopping`
- `NullCapture`, `NullInput`, `NullServiceLifecycle` — null impls

**File:** `agent/crates/mesh-agent-core/src/lib.rs` — add `pub mod platform;` + re-exports.

**Tests first (in `platform.rs`):**
- `test_null_capture_returns_no_display_error` — `NullCapture.next_frame()` returns `CaptureError::NoDisplay`
- `test_null_capture_resolution_is_zero` — `(0, 0)`
- `test_null_input_is_not_available` — `is_available() == false`
- `test_null_input_inject_returns_error` — all inject methods return `InputError::NotAvailable`
- `test_null_service_lifecycle_does_not_panic` — call all three notify methods

### Step 3: Implement `platform-linux`

**Update:** `agent/crates/platform-linux/Cargo.toml` — add `mesh-protocol`, `tracing`, `async-trait` (workspace), `x11rb` (optional feature).

**New files:**

| File | Contents |
|---|---|
| `src/lib.rs` | Factory functions (`create_screen_capture`, `create_input_injector`, `create_service_lifecycle`), re-exports |
| `src/runtime.rs` | `LinuxRuntime` enum, `detect_runtime()`, `get_filesystem_root()` |
| `src/service.rs` | `SystemdLifecycle` — sends `READY=1`/`RELOADING=1`/`STOPPING=1` via `UnixDatagram` to `NOTIFY_SOCKET` |
| `src/capture.rs` | Re-exports `NullCapture`; `#[cfg(feature = "x11")] mod x11_capture` with `X11Capture` stub |

**Tests first (in `runtime.rs`):**
- `test_detect_bare_metal_other_default` — no NOTIFY_SOCKET, no /.dockerenv → `BareMetalOther`
- `test_detect_bare_metal_systemd_via_notify_socket` — set env → `BareMetalSystemd`
- `test_filesystem_root_bare_metal_returns_slash` — no /host → `/`

**Tests first (in `service.rs`):**
- `test_systemd_lifecycle_sends_ready` — create temp `UnixDatagram`, set `NOTIFY_SOCKET`, call `notify_ready()`, verify `READY=1` received

**Tests first (in `lib.rs`):**
- `test_create_screen_capture_returns_null_without_display` — resolution `(0, 0)`
- `test_create_input_injector_returns_null` — `is_available() == false`
- `test_create_service_lifecycle_without_systemd` — does not panic

### Step 4: Implement `platform-windows` (compile-only on Linux)

**Update:** `agent/crates/platform-windows/Cargo.toml` — add `mesh-protocol`, `tracing`, `async-trait` (workspace). Add `[target.'cfg(windows)'.dependencies]` for `windows` crate (future).

**New files:**

| File | Contents |
|---|---|
| `src/lib.rs` | Factory functions — on `#[cfg(not(windows))]` return null impls |
| `src/capture.rs` | `#[cfg(windows)] struct DxgiCapture` with `todo!()` body; compiles as no-op on Linux |
| `src/input.rs` | `#[cfg(windows)] struct Win32Input` with `todo!()` body |
| `src/service.rs` | `#[cfg(windows)] struct WindowsServiceLifecycle` with `todo!()` body |

**Tests first:**
- `test_factory_returns_null_on_non_windows` — on Linux, `create_screen_capture().resolution() == (0, 0)`

---

## Files Modified/Created Summary

| Action | Path |
|---|---|
| Edit | `agent/Cargo.toml` — add `async-trait = "0.1"` to workspace deps |
| Edit | `agent/crates/mesh-protocol/src/types.rs` — add KeyCode, KeyEvent, MouseButton |
| Edit | `agent/crates/mesh-protocol/src/lib.rs` — re-export new types |
| Edit | `agent/crates/mesh-agent-core/Cargo.toml` — add `async-trait` dep |
| Create | `agent/crates/mesh-agent-core/src/platform.rs` — traits + null impls |
| Edit | `agent/crates/mesh-agent-core/src/lib.rs` — add platform module |
| Edit | `agent/crates/platform-linux/Cargo.toml` — add deps + x11 feature |
| Create | `agent/crates/platform-linux/src/runtime.rs` |
| Create | `agent/crates/platform-linux/src/service.rs` |
| Create | `agent/crates/platform-linux/src/capture.rs` |
| Rewrite | `agent/crates/platform-linux/src/lib.rs` |
| Edit | `agent/crates/platform-windows/Cargo.toml` — add deps |
| Create | `agent/crates/platform-windows/src/capture.rs` |
| Create | `agent/crates/platform-windows/src/input.rs` |
| Create | `agent/crates/platform-windows/src/service.rs` |
| Rewrite | `agent/crates/platform-windows/src/lib.rs` |

## Verification

1. `cd agent && cargo test --workspace` — all tests pass (including new platform tests)
2. `cd agent && cargo clippy --workspace -- -D warnings` — zero warnings
3. `cd agent && cargo doc --workspace --no-deps` — all public items documented
4. `cd agent && cargo bench -p mesh-protocol` — benchmarks still run
5. `cd server && go test -race -timeout 5m ./...` — server unaffected
6. `cd web && npx vitest run` — web unaffected
7. No `unwrap()` in non-test code
8. All public enums have `#[non_exhaustive]`
