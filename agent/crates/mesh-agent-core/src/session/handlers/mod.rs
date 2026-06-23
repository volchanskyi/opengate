//! Control-message handlers used by `SessionHandler::handle_control`.
//!
//! Each related variant group lives in a dedicated submodule (mouse, keyboard,
//! file, webrtc, switch, terminal-control) behind a `ControlMessageHandler`
//! marker trait. The outer match in [`super::handler`] stays as a thin
//! multiplexer that routes each variant to the owning handler's associated
//! function.
//!
//! **Mutation-score guard:** the workspace must preserve the 89.5% baseline
//! on `mesh-agent-core` (see [`.github/workflows/mutation.yml`](../../../../../../.github/workflows/mutation.yml)).
//! Each handler ships direct unit tests in its own file plus an
//! integration test under `tests/` so coverage does not drop when the
//! match arm becomes a one-liner delegate.

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
///
/// The trait carries no methods — dispatch happens in [`super::handler`]
/// via direct calls to each handler's associated functions. The trait
/// exists so each impl is a documented participant in the control-message
/// surface, separately testable, and discoverable by `cargo doc`.
pub trait ControlMessageHandler {}
