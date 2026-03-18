//! Core agent logic for OpenGate.
//!
//! This crate provides agent identity management, QUIC connection handling,
//! control message exchange with the server, and relay session management.

pub mod config;
pub mod connection;
pub mod error;
pub mod file_ops;
pub mod identity;
pub mod platform;
pub mod session;
pub mod session_error;
pub mod terminal;
pub mod update;
pub mod webrtc;

pub use config::AgentConfig;
pub use connection::{reconnect_with_backoff, AgentConnection, AsyncControlStream, ControlStream};
pub use error::{AgentError, ConnectionError};
pub use identity::AgentIdentity;
pub use platform::{
    CaptureError, InputError, InputInjector, NullCapture, NullInput, NullServiceLifecycle,
    RawFrame, ScreenCapture, ServiceLifecycle,
};
pub use session::{SessionHandler, TerminalHandle};
pub use session_error::SessionError;
pub use update::{UpdateConfig, UpdateError};
