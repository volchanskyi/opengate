//! Control-message handlers, one submodule per variant group, routed by `handle_control`.

pub mod file;
pub mod keyboard;
pub mod mouse;
pub mod switch;
pub mod terminal_control;
pub mod webrtc;

pub use file::FileHandler;
pub use keyboard::KeyboardHandler;
pub use mouse::MouseHandler;
pub use switch::SwitchHandler;
pub use terminal_control::TerminalControlHandler;
pub use webrtc::{RealWebRtcDispatch, WebRTCHandler, WebRtcDispatch};

/// Marker trait implemented by every grouped `ControlMessage` handler.
pub trait ControlMessageHandler {}
