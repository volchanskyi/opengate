//! Screen capture implementations for Windows.
//!
//! On Windows, uses DXGI Desktop Duplication for high-performance screen capture.
//! On non-Windows targets, this module is empty.

#[cfg(windows)]
mod dxgi {
    use mesh_agent_core::{CaptureError, RawFrame, ScreenCapture};

    /// DXGI Desktop Duplication screen capture.
    ///
    /// Uses the IDXGIOutputDuplication interface for GPU-accelerated
    /// screen capture with minimal CPU overhead.
    pub struct DxgiCapture {
        width: u32,
        height: u32,
    }

    impl DxgiCapture {
        /// Initialize DXGI Desktop Duplication on the primary monitor.
        pub fn new() -> Result<Self, CaptureError> {
            // TODO: Initialize D3D11 device, get DXGI adapter/output,
            // create IDXGIOutputDuplication
            Err(CaptureError::Backend(
                "DXGI capture not yet implemented".to_string(),
            ))
        }
    }

    #[async_trait::async_trait]
    impl ScreenCapture for DxgiCapture {
        async fn next_frame(&mut self) -> Result<RawFrame, CaptureError> {
            // TODO: AcquireNextFrame, map staging texture, copy pixel data
            Err(CaptureError::Backend(
                "DXGI frame capture not yet implemented".to_string(),
            ))
        }

        fn resolution(&self) -> (u32, u32) {
            (self.width, self.height)
        }
    }
}

#[cfg(windows)]
pub use dxgi::DxgiCapture;
