//! Session token and permission types.

use serde::{Deserialize, Serialize};

/// Session token for relay connections. 32 random bytes encoded as 64 hex chars.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct SessionToken(String);

impl SessionToken {
    /// Generate a new random session token (32 bytes = 64 hex chars).
    pub fn generate() -> Self {
        use std::fmt::Write;
        let bytes: [u8; 32] = rand_bytes();
        let mut hex = String::with_capacity(64);
        for b in &bytes {
            write!(hex, "{b:02x}").expect("hex formatting cannot fail");
        }
        Self(hex)
    }

    /// Get the token as a string slice.
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

fn rand_bytes() -> [u8; 32] {
    let mut buf = [0u8; 32];
    getrandom::fill(&mut buf).expect("OS entropy source failed");
    buf
}

/// Permissions granted for a session.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Permissions {
    pub desktop: bool,
    pub terminal: bool,
    pub file_read: bool,
    pub file_write: bool,
    pub input: bool,
}
