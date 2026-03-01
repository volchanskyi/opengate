//! Core agent logic for OpenGate.
//!
//! This crate provides agent identity management, QUIC connection handling,
//! and control message exchange with the server.

pub mod config;
pub mod connection;
pub mod error;
pub mod identity;

pub use config::AgentConfig;
pub use connection::{AgentConnection, AsyncControlStream, ControlStream};
pub use error::{AgentError, ConnectionError};
pub use identity::AgentIdentity;
