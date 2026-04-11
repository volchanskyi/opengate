# Platform Abstraction

The agent uses platform traits defined in `mesh-agent-core` to abstract OS-specific operations. Each trait has a factory function that detects the runtime environment and returns the appropriate implementation.

## Traits

| Trait | Purpose | Dispatch | Null Fallback |
|-------|---------|----------|---------------|
| `ScreenCapture` | Frame grabbing | `async_trait` + `Box<dyn ScreenCapture>` | `NullCapture` |
| `InputInjector` | Keyboard/mouse injection | Sync, object-safe | `NullInput` |
| `ServiceLifecycle` | Service manager notifications | Sync, object-safe | `NullServiceLifecycle` |

`ScreenCapture` requires `async_trait` because frame capture is inherently async. The other two traits are synchronous and natively object-safe.

## Platform Implementations

| Trait | Linux | Windows |
|-------|-------|---------|
| `ScreenCapture` | Not implemented (Linux = Terminal + FileManager only) | DXGI Desktop Duplication |
| `InputInjector` | Not implemented | Win32 `SendInput` |
| `ServiceLifecycle` | systemd `sd_notify` | Windows SCM |

## Factory Functions

```rust
// Returns the best available implementation for the current platform.
// Falls back to null implementations in headless/container environments.
create_screen_capture()      -> Box<dyn ScreenCapture>
create_input_injector()      -> Box<dyn InputInjector>
create_service_lifecycle()   -> Box<dyn ServiceLifecycle>
```

## Runtime Detection (Linux)

`platform-linux` provides `detect_runtime()` which distinguishes between:

```
detect_runtime()
    │
    ├── Container (Docker/Podman)    → checks /.dockerenv, /proc/1/cgroup
    ├── Systemd bare metal           → checks NOTIFY_SOCKET env
    └── Other                        → fallback
```

Null implementations are returned when running in containers or environments where the real backend is unavailable. On Linux, `platform-linux` only provides `create_service_lifecycle()` — no screen capture or input injection factories exist.

## Capability Detection

Linux agents statically report **Terminal** and **FileManager** capabilities. Windows/Mac agents additionally report **RemoteDesktop**. There is no runtime display detection on Linux — the capability set is fixed at compile time.

Capabilities are sent in the `AgentRegister` control message, persisted to the `devices.capabilities` JSON column in the database, and exposed via the Device REST API. The web client uses them to hide unsupported session tabs (e.g., Desktop and Chat tabs are hidden for agents without `RemoteDesktop`).

## Compilation

- Linux implementations live in `platform-linux` and compile unconditionally (ServiceLifecycle only)
- Windows implementations live in `platform-windows` and are behind `#[cfg(windows)]` — they compile on Linux as stubs
- Null implementations live in `mesh-agent-core` and are always available

## Input Wire Types

Keyboard and mouse events use shared wire types defined in `mesh-protocol`:

```rust
struct KeyEvent {
    key: KeyCode,      // USB HID-inspired key codes
    pressed: bool,
}

enum MouseButton { Left, Right, Middle, X1, X2 }
```

`KeyCode` covers the full standard keyboard layout (letters, digits, modifiers, arrows, function keys, numpad, media keys) and is serialized via MessagePack for cross-language compatibility.
