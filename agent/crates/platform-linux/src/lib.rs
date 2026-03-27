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

use tracing::debug;

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
        debug!("display detected via environment variable");
        return true;
    }

    // Slow path: probe display server sockets for connectivity.
    // This handles systemd services that don't inherit user session env vars.
    has_display_server_socket()
}

/// Probes X11 and Wayland sockets for actual connectivity.
///
/// Unlike a simple existence check, this tries to `connect()` to each
/// socket. A successful connect proves a display server is listening.
/// This avoids false positives from stub sockets (e.g. WSLg on headless
/// WSL2) and false negatives from only checking `wayland-0`.
fn has_display_server_socket() -> bool {
    // Probe X11 sockets
    if let Ok(entries) = std::fs::read_dir("/tmp/.X11-unix") {
        for entry in entries.flatten() {
            if probe_socket(&entry.path(), "X11") {
                return true;
            }
        }
    }

    // Probe Wayland sockets in all user runtime dirs
    if let Ok(uid_dirs) = std::fs::read_dir("/run/user") {
        for uid_dir in uid_dirs.flatten() {
            let runtime_dir = uid_dir.path();
            let entries = match std::fs::read_dir(&runtime_dir) {
                Ok(e) => e,
                Err(e) => {
                    debug!(path = %runtime_dir.display(), error = %e, "cannot read user runtime dir");
                    continue;
                }
            };
            for entry in entries.flatten() {
                let name = entry.file_name();
                let name_str = name.to_string_lossy();
                if !name_str.starts_with("wayland-") || name_str.ends_with(".lock") {
                    continue;
                }
                if probe_socket(&entry.path(), "Wayland") {
                    return true;
                }
            }
        }
    }

    debug!("no connectable display server socket found");
    false
}

/// Attempt a Unix socket connect to verify a display server is listening.
fn probe_socket(path: &std::path::Path, kind: &str) -> bool {
    use std::os::unix::net::UnixStream;

    debug!(path = %path.display(), kind, "probing socket");
    match UnixStream::connect(path) {
        Ok(_) => {
            debug!(path = %path.display(), kind, "socket is connectable");
            true
        }
        Err(e) => {
            debug!(path = %path.display(), kind, error = %e, "socket not connectable");
            false
        }
    }
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
    fn test_has_display_server_socket_returns_bool() {
        // Smoke test: just verify it doesn't panic.
        // Result depends on the host environment.
        let _ = has_display_server_socket();
    }

    #[test]
    fn test_connect_rejects_regular_file() {
        // A regular file is not a Unix socket — connect must fail.
        let dir = tempfile::tempdir().unwrap();
        let fake_socket = dir.path().join("X99");
        std::fs::write(&fake_socket, b"").unwrap();
        assert!(std::os::unix::net::UnixStream::connect(&fake_socket).is_err());
    }

    #[test]
    fn test_has_display_true_with_display_env() {
        std::env::set_var("DISPLAY", ":0");
        std::env::remove_var("WAYLAND_DISPLAY");
        let result = has_display();
        std::env::remove_var("DISPLAY");
        assert!(result);
    }

    #[test]
    fn test_has_display_false_without_env_or_sockets() {
        // With no env vars and no connectable sockets, should return false
        // (unless the host actually has a display server running).
        std::env::remove_var("DISPLAY");
        std::env::remove_var("WAYLAND_DISPLAY");
        // We can't guarantee false on all hosts, but we verify no panic.
        let _ = has_display();
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
        // Remove DISPLAY so X11Capture::new() cannot connect.
        let saved = std::env::var("DISPLAY").ok();
        std::env::remove_var("DISPLAY");
        std::env::remove_var("WAYLAND_DISPLAY");

        let mut cap = create_screen_capture();
        assert_eq!(cap.resolution(), (0, 0));
        assert!(cap.next_frame().await.is_err());

        // Restore env vars to avoid affecting other tests.
        if let Some(val) = saved {
            std::env::set_var("DISPLAY", val);
        }
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
