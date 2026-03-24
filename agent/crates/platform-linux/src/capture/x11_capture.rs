//! X11 screen capture using x11rb.

use mesh_agent_core::{CaptureError, RawFrame, ScreenCapture};
use x11rb::connection::Connection;

/// X11 screen capture.
///
/// Connects to the X11 display server and captures frames from the root window.
/// Requires the `DISPLAY` environment variable to be set.
pub struct X11Capture {
    width: u32,
    height: u32,
}

impl X11Capture {
    /// Attempt to connect to the X11 display server.
    ///
    /// Returns [`CaptureError::NoDisplay`] if `DISPLAY` is not set or
    /// the connection fails.
    pub fn new() -> Result<Self, CaptureError> {
        let _display = std::env::var("DISPLAY").map_err(|_| CaptureError::NoDisplay)?;

        let (conn, screen_num) =
            x11rb::connect(None).map_err(|e| CaptureError::Backend(e.to_string()))?;
        let screen = &conn.setup().roots[screen_num];

        Ok(Self {
            width: screen.width_in_pixels as u32,
            height: screen.height_in_pixels as u32,
        })
    }
}

#[async_trait::async_trait]
impl ScreenCapture for X11Capture {
    async fn next_frame(&mut self) -> Result<RawFrame, CaptureError> {
        // Full X11 frame grabbing (XGetImage / MIT-SHM) is deferred
        // to a later iteration. For now, return a placeholder error
        // indicating the capture is not yet fully implemented.
        Err(CaptureError::Backend(
            "X11 frame capture not yet implemented".to_string(),
        ))
    }

    fn resolution(&self) -> (u32, u32) {
        (self.width, self.height)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_x11_capture_requires_display() {
        // When DISPLAY is not set, X11Capture::new() should fail
        let saved = std::env::var("DISPLAY").ok();
        std::env::remove_var("DISPLAY");

        let result = X11Capture::new();
        assert!(matches!(result, Err(CaptureError::NoDisplay)));

        // Restore
        if let Some(val) = saved {
            std::env::set_var("DISPLAY", val);
        }
    }

    #[test]
    #[ignore = "requires X11 display server"]
    fn test_x11_capture_connects_when_display_set() {
        let cap = X11Capture::new().expect("DISPLAY must be set");
        let (w, h) = cap.resolution();
        assert!(w > 0);
        assert!(h > 0);
    }
}
