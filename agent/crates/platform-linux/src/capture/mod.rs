//! Screen capture implementations for Linux.
//!
//! Without feature flags, only [`NullCapture`](mesh_agent_core::NullCapture)
//! is available. Enable the `x11` feature for X11 screen capture.

#[cfg(feature = "x11")]
mod x11_capture;

#[cfg(feature = "x11")]
pub use x11_capture::X11Capture;
