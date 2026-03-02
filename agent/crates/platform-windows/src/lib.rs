//! Windows platform implementations for OpenGate agent.
//!
//! On non-Windows targets, all factory functions return null implementations
//! so the crate compiles for CI and cross-compilation checks.

mod capture;
mod input;
mod service;

pub use mesh_agent_core::{
    CaptureError, InputError, InputInjector, NullCapture, NullInput, NullServiceLifecycle,
    RawFrame, ScreenCapture, ServiceLifecycle,
};

#[cfg(windows)]
pub use capture::DxgiCapture;
#[cfg(windows)]
pub use input::Win32Input;
#[cfg(windows)]
pub use service::WindowsServiceLifecycle;

/// Create a screen capture instance.
///
/// On Windows, attempts to initialize DXGI Desktop Duplication.
/// Falls back to [`NullCapture`] on failure or non-Windows targets.
pub fn create_screen_capture() -> Box<dyn ScreenCapture> {
    #[cfg(windows)]
    {
        match capture::DxgiCapture::new() {
            Ok(cap) => return Box::new(cap),
            Err(e) => {
                tracing::warn!("DXGI capture unavailable: {e}");
            }
        }
    }
    Box::new(NullCapture)
}

/// Create an input injector.
///
/// On Windows, returns [`Win32Input`]. On non-Windows targets,
/// returns [`NullInput`].
pub fn create_input_injector() -> Box<dyn InputInjector> {
    #[cfg(windows)]
    {
        return Box::new(input::Win32Input::new());
    }
    #[cfg(not(windows))]
    Box::new(NullInput)
}

/// Create a service lifecycle notifier.
///
/// On Windows, returns [`WindowsServiceLifecycle`]. On non-Windows targets,
/// returns [`NullServiceLifecycle`].
pub fn create_service_lifecycle() -> Box<dyn ServiceLifecycle> {
    #[cfg(windows)]
    {
        return Box::new(service::WindowsServiceLifecycle::new());
    }
    #[cfg(not(windows))]
    Box::new(NullServiceLifecycle)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_factory_returns_null_on_non_windows() {
        let mut cap = create_screen_capture();
        assert_eq!(cap.resolution(), (0, 0));
        assert!(cap.next_frame().await.is_err());

        let input = create_input_injector();
        assert!(!input.is_available());

        let svc = create_service_lifecycle();
        svc.notify_ready(); // should not panic
    }
}
