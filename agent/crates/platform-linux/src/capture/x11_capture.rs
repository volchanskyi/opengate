//! X11 screen capture using x11rb.

use mesh_agent_core::{CaptureError, RawFrame, ScreenCapture};
use x11rb::connection::Connection;
use x11rb::protocol::xproto::{ConnectionExt, ImageFormat};

/// X11 screen capture.
///
/// Connects to the X11 display server and captures frames from the root window
/// using `GetImage` (ZPixmap format). Converts BGRX → RGBA on each frame.
pub struct X11Capture {
    conn: x11rb::rust_connection::RustConnection,
    root: u32,
    width: u32,
    height: u32,
}

impl X11Capture {
    /// Attempt to connect to the X11 display server.
    ///
    /// Returns [`CaptureError::NoDisplay`] if `DISPLAY` is not set or
    /// the connection fails.
    pub fn new() -> Result<Self, CaptureError> {
        let display = std::env::var("DISPLAY").map_err(|_| CaptureError::NoDisplay)?;

        let (conn, screen_num) =
            x11rb::connect(Some(&display)).map_err(|e| CaptureError::Backend(e.to_string()))?;
        let screen = &conn.setup().roots[screen_num];
        let root = screen.root;
        let width = screen.width_in_pixels as u32;
        let height = screen.height_in_pixels as u32;

        Ok(Self {
            conn,
            root,
            width,
            height,
        })
    }
}

#[async_trait::async_trait]
impl ScreenCapture for X11Capture {
    async fn next_frame(&mut self) -> Result<RawFrame, CaptureError> {
        let reply = self
            .conn
            .get_image(
                ImageFormat::Z_PIXMAP,
                self.root,
                0,
                0,
                self.width as u16,
                self.height as u16,
                !0, // all planes
            )
            .map_err(|e| CaptureError::Backend(e.to_string()))?
            .reply()
            .map_err(|e| CaptureError::Backend(e.to_string()))?;

        // X11 ZPixmap on 24/32-bit depth = BGRX (4 bytes/pixel, little-endian).
        // Convert BGRX → RGBA in-place for browser compatibility.
        let mut data = reply.data;
        for chunk in data.chunks_exact_mut(4) {
            let (b, g, r) = (chunk[0], chunk[1], chunk[2]);
            chunk[0] = r;
            chunk[1] = g;
            chunk[2] = b;
            chunk[3] = 255; // alpha
        }

        Ok(RawFrame {
            width: self.width,
            height: self.height,
            data,
        })
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

    #[tokio::test]
    #[ignore = "requires X11 display server"]
    async fn test_x11_capture_next_frame_returns_rgba() {
        let mut cap = X11Capture::new().expect("DISPLAY must be set");
        let frame = cap.next_frame().await.expect("capture should succeed");
        assert_eq!(frame.data.len(), (frame.width * frame.height * 4) as usize);
        // Every 4th byte (alpha) should be 255
        for chunk in frame.data.chunks_exact(4) {
            assert_eq!(chunk[3], 255, "alpha channel must be 255");
        }
    }
}
