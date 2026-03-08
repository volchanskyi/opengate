//! Platform abstraction traits for screen capture, input injection,
//! and service lifecycle.
//!
//! These traits define the contract that platform-specific crates
//! (`platform-linux`, `platform-windows`) implement. The core agent
//! programs against these traits, enabling cross-platform operation.

use mesh_protocol::{KeyEvent, MouseButton};

/// A raw captured screen frame (BGRA pixel data).
#[derive(Debug, Clone)]
pub struct RawFrame {
    /// Frame width in pixels.
    pub width: u32,
    /// Frame height in pixels.
    pub height: u32,
    /// BGRA pixel data, length = width * height * 4.
    pub data: Vec<u8>,
}

/// Errors from screen capture operations.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum CaptureError {
    /// No display server available (headless/container environment).
    #[error("no display available")]
    NoDisplay,
    /// The capture backend encountered an error.
    #[error("capture failed: {0}")]
    Backend(String),
    /// Timeout waiting for next frame.
    #[error("frame timeout")]
    Timeout,
}

/// Errors from input injection operations.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum InputError {
    /// No input subsystem available.
    #[error("input not available")]
    NotAvailable,
    /// The injection backend encountered an error.
    #[error("injection failed: {0}")]
    Backend(String),
}

/// Trait for capturing screen frames.
///
/// Uses `async_trait` because factory functions return `Box<dyn ScreenCapture>`,
/// which requires object safety. Native async fn in traits is not object-safe.
#[async_trait::async_trait]
pub trait ScreenCapture: Send + 'static {
    /// Capture the next frame. Blocks until a new frame is available.
    async fn next_frame(&mut self) -> Result<RawFrame, CaptureError>;
    /// Return the current screen resolution as (width, height).
    fn resolution(&self) -> (u32, u32);
}

/// Trait for injecting keyboard and mouse input.
pub trait InputInjector: Send + Sync + 'static {
    /// Inject a keyboard event.
    fn inject_key(&self, event: KeyEvent) -> Result<(), InputError>;
    /// Inject a mouse movement to absolute coordinates.
    fn inject_mouse_move(&self, x: i32, y: i32) -> Result<(), InputError>;
    /// Inject a mouse button press or release.
    fn inject_mouse_button(&self, button: MouseButton, pressed: bool) -> Result<(), InputError>;
    /// Returns true if input injection is available on this platform.
    fn is_available(&self) -> bool;
}

/// Trait for platform service lifecycle notifications (e.g., systemd, Windows SCM).
pub trait ServiceLifecycle: Send + 'static {
    /// Notify the service manager that the service is ready.
    fn notify_ready(&self);
    /// Notify the service manager that the service is reloading configuration.
    fn notify_reloading(&self);
    /// Notify the service manager that the service is stopping.
    fn notify_stopping(&self);
}

/// Null screen capture that always returns [`CaptureError::NoDisplay`].
pub struct NullCapture;

#[async_trait::async_trait]
impl ScreenCapture for NullCapture {
    async fn next_frame(&mut self) -> Result<RawFrame, CaptureError> {
        Err(CaptureError::NoDisplay)
    }

    fn resolution(&self) -> (u32, u32) {
        (0, 0)
    }
}

/// Null input injector that reports as unavailable.
pub struct NullInput;

impl InputInjector for NullInput {
    fn inject_key(&self, _event: KeyEvent) -> Result<(), InputError> {
        Err(InputError::NotAvailable)
    }

    fn inject_mouse_move(&self, _x: i32, _y: i32) -> Result<(), InputError> {
        Err(InputError::NotAvailable)
    }

    fn inject_mouse_button(&self, _button: MouseButton, _pressed: bool) -> Result<(), InputError> {
        Err(InputError::NotAvailable)
    }

    fn is_available(&self) -> bool {
        false
    }
}

/// No-op service lifecycle for non-service environments.
pub struct NullServiceLifecycle;

impl ServiceLifecycle for NullServiceLifecycle {
    fn notify_ready(&self) {}
    fn notify_reloading(&self) {}
    fn notify_stopping(&self) {}
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_protocol::KeyCode;

    #[tokio::test]
    async fn test_null_capture_returns_no_display_error() {
        let mut cap = NullCapture;
        let result = cap.next_frame().await;
        assert!(result.is_err());
        assert!(matches!(result.unwrap_err(), CaptureError::NoDisplay));
    }

    #[test]
    fn test_null_capture_resolution_is_zero() {
        let cap = NullCapture;
        assert_eq!(cap.resolution(), (0, 0));
    }

    #[test]
    fn test_null_input_is_not_available() {
        let input = NullInput;
        assert!(!input.is_available());
    }

    #[test]
    fn test_null_input_inject_returns_error() {
        let input = NullInput;
        assert!(input
            .inject_key(KeyEvent {
                key: KeyCode::KeyA,
                pressed: true,
            })
            .is_err());
        assert!(input.inject_mouse_move(100, 200).is_err());
        assert!(input.inject_mouse_button(MouseButton::Left, true).is_err());
    }

    #[test]
    fn test_null_service_lifecycle_does_not_panic() {
        let svc = NullServiceLifecycle;
        svc.notify_ready();
        svc.notify_reloading();
        svc.notify_stopping();
    }

    #[tokio::test]
    async fn test_null_capture_as_dyn_trait_object() {
        // Verify Box<dyn ScreenCapture> works (object safety)
        let mut cap: Box<dyn ScreenCapture> = Box::new(NullCapture);
        assert_eq!(cap.resolution(), (0, 0));
        assert!(cap.next_frame().await.is_err());
    }

    #[test]
    fn test_null_input_as_dyn_trait_object() {
        // Verify Box<dyn InputInjector> works (object safety)
        let input: Box<dyn InputInjector> = Box::new(NullInput);
        assert!(!input.is_available());
    }
}
