//! Control message handlers carved out of `SessionHandler::handle_control`.
//!
//! Per [ADR-024](../../../../../../docs/adr/ADR-024-rust-control-message-handler-trait.md),
//! the inner `handle_control` fan-out (10 methods) moves behind a
//! `ControlMessageHandler` marker trait, with each related variant group
//! living in a dedicated submodule (mouse, keyboard, file, webrtc, switch,
//! terminal-control). The outer match in [`super::handler`] stays as a thin
//! multiplexer that routes each variant to the owning handler's associated
//! function.
//!
//! **Pilot scope:** this commit carves `MouseHandler` (MouseMove +
//! MouseClick — the largest input-event group). Subsequent commits add the
//! remaining handlers opportunistically as `handle_control` arms are
//! touched.
//!
//! **Mutation-score guard:** the workspace must preserve the 89.5% baseline
//! on `mesh-agent-core` (see [`.github/workflows/mutation.yml`](../../../../../../.github/workflows/mutation.yml)).
//! Each carved handler ships direct unit tests in its own file plus an
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
pub use webrtc::WebRTCHandler;

/// Marker trait implemented by every grouped `ControlMessage` handler.
///
/// The trait carries no methods — dispatch happens in [`super::handler`]
/// via direct calls to each handler's associated functions. The trait
/// exists so the carve-out earns the port discipline ADR-020 §3.6
/// requires: each impl is a documented participant in the control-message
/// surface, separately testable, and discoverable by `cargo doc`.
pub trait ControlMessageHandler {}
