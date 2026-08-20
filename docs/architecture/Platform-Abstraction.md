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

The agent implements Linux. Every trait a platform crate does not implement
resolves to its null implementation, which is what headless hosts, containers,
and CI runs use.

| Trait | Linux |
|-------|-------|
| `ScreenCapture` | Null (Linux = Terminal + FileManager only) |
| `InputInjector` | Null |
| `ServiceLifecycle` | systemd `sd_notify` |

A further platform plugs in by adding a crate that implements the three traits
and exposes the same three factory functions. Nothing in `mesh-agent-core`
changes to accommodate one.

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

Linux agents statically report **Terminal**, **FileManager**, **HardwareInventory**, and **DeviceLogs** capabilities. There is no runtime display detection — the capability set is fixed at compile time. An agent whose platform crate provides desktop capture and input injection additionally reports those capabilities.

Capabilities are sent in the `AgentRegister` control message, persisted to the `devices.capabilities` JSON column in the database, and exposed via the Device REST API. The web client uses them to hide unsupported session tabs (e.g., Desktop and Chat tabs are hidden for agents without `RemoteDesktop`). The server also gates newer server-to-agent messages on these advertised capabilities so old agents never receive variants they cannot decode.

## Compilation

- Linux implementations live in `platform-linux` and compile unconditionally (ServiceLifecycle only)
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
