//! Control-message handlers, one submodule per variant group, routed by the session's features.

pub mod file;
pub mod keyboard;
pub mod mouse;
pub mod terminal_control;

pub use file::{FileHandler, FileTasks, DOWNLOAD_RUNNING};
pub use keyboard::KeyboardHandler;
pub use mouse::MouseHandler;
pub use terminal_control::TerminalControlHandler;

/// Marker trait implemented by every grouped `ControlMessage` handler.
pub trait ControlMessageHandler {}
