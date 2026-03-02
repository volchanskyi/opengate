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
