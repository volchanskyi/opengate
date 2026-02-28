//! Shared protocol types and codec for the OpenGate wire protocol.
//!
//! This crate defines every wire type, codec, and structure that crosses
//! the agent–server boundary. Both the Rust agent and Go server implement
//! this protocol identically.

pub mod codec;
pub mod control;
pub mod error;
pub mod types;

pub use codec::*;
pub use control::*;
pub use error::*;
pub use types::*;
