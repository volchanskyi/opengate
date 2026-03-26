//! Linux platform implementations for OpenGate agent.
//!
//! Provides runtime detection, screen capture, input injection,
//! and systemd service lifecycle for Linux hosts and containers.
//!
//! # Features
//!
//! - `x11` — Enable X11 screen capture via x11rb.

mod capture;
pub mod runtime;
pub mod service;

pub use mesh_agent_core::{
    CaptureError, InputError, InputInjector, NullCapture, NullInput, NullServiceLifecycle,
    RawFrame, ScreenCapture, ServiceLifecycle,
};
pub use runtime::{detect_runtime, get_filesystem_root, LinuxRuntime};
pub use service::SystemdLifecycle;

#[cfg(feature = "x11")]
pub use capture::X11Capture;

/// Returns true if a graphical display server (X11 or Wayland) is available.
///
/// Checks environment variables first (works in user sessions), then falls
/// back to probing for running display server processes (works in systemd
/// services which lack user session env vars).
pub fn has_display() -> bool {
    // Fast path: env vars are set in user sessions / containers with display.
    if std::env::var_os("DISPLAY").is_some() || std::env::var_os("WAYLAND_DISPLAY").is_some() {
        return true;
    }

    // Slow path: check if a display server process is running on the system.
    // This handles systemd services that don't inherit user session env vars.
    has_display_server_process()
}

/// Checks if an X11 or Wayland display server process is running.
fn has_display_server_process() -> bool {
    // Check for X11 socket
    if std::path::Path::new("/tmp/.X11-unix").exists() {
        if let Ok(entries) = std::fs::read_dir("/tmp/.X11-unix") {
            if entries.count() > 0 {
                return true;
            }
        }
    }

    // Check for Wayland socket in common locations
    if let Ok(entries) = std::fs::read_dir("/run/user") {
        for entry in entries.flatten() {
            let wayland_path = entry.path().join("wayland-0");
            if wayland_path.exists() {
                return true;
            }
        }
    }

    false
}

/// Create a screen capture instance for the current environment.
///
/// Returns [`X11Capture`] if the `x11` feature is enabled and `DISPLAY` is set,
/// otherwise returns [`NullCapture`].
pub fn create_screen_capture() -> Box<dyn ScreenCapture> {
    #[cfg(feature = "x11")]
    {
        if let Ok(cap) = capture::X11Capture::new() {
            return Box::new(cap);
        }
    }
    Box::new(NullCapture)
}

/// Create an input injector for the current environment.
///
/// Currently returns [`NullInput`]. X11/evdev input injection is planned
/// for a future release.
pub fn create_input_injector() -> Box<dyn InputInjector> {
    Box::new(NullInput)
}

/// Create a service lifecycle notifier for the current environment.
///
/// Returns [`SystemdLifecycle`] if `NOTIFY_SOCKET` is set,
/// otherwise returns [`NullServiceLifecycle`].
pub fn create_service_lifecycle() -> Box<dyn ServiceLifecycle> {
    if std::env::var_os("NOTIFY_SOCKET").is_some() {
        Box::new(SystemdLifecycle::new())
    } else {
        Box::new(NullServiceLifecycle)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_has_display_server_process_returns_bool() {
        // Smoke test: just verify it doesn't panic.
        // Result depends on the host environment.
        let _ = has_display_server_process();
    }

    #[test]
    fn test_has_display_true_via_env_without_process_check() {
        // Env vars should short-circuit — no process check needed.
        std::env::set_var("DISPLAY", ":99");
        std::env::remove_var("WAYLAND_DISPLAY");
        let result = has_display();
        std::env::remove_var("DISPLAY");
        assert!(result);
    }

    #[test]
    fn test_has_display_true_with_display() {
        std::env::set_var("DISPLAY", ":0");
        std::env::remove_var("WAYLAND_DISPLAY");
        let result = has_display();
        std::env::remove_var("DISPLAY");
        assert!(result);
    }

    #[test]
    fn test_has_display_true_with_wayland() {
        std::env::remove_var("DISPLAY");
        std::env::set_var("WAYLAND_DISPLAY", "wayland-0");
        let result = has_display();
        std::env::remove_var("WAYLAND_DISPLAY");
        assert!(result);
    }

    #[tokio::test]
    async fn test_create_screen_capture_returns_null_without_display() {
        // In CI / WSL2 with no DISPLAY and no x11 feature, should get NullCapture
        let mut cap = create_screen_capture();
        assert_eq!(cap.resolution(), (0, 0));
        assert!(cap.next_frame().await.is_err());
    }

    #[test]
    fn test_create_input_injector_returns_null() {
        let input = create_input_injector();
        assert!(!input.is_available());
    }

    #[test]
    fn test_create_service_lifecycle_without_systemd() {
        std::env::remove_var("NOTIFY_SOCKET");
        let svc = create_service_lifecycle();
        // Should not panic
        svc.notify_ready();
        svc.notify_stopping();
    }
}
