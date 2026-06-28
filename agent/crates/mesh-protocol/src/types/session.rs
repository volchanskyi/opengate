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

    /// Return a redacted form safe for logging: the first 8 characters followed
    /// by `...`, or `***` when the token is 8 characters or shorter. Mirrors the
    /// server-side `protocol.RedactToken` so neither end leaks a full token.
    pub fn redacted(&self) -> String {
        redact_token(&self.0)
    }
}

/// Redact a token for safe logging. Mirrors Go's `protocol.RedactToken`:
/// first 8 characters + `...`, or `***` if the token is 8 characters or fewer.
pub fn redact_token(token: &str) -> String {
    let prefix: String = token.chars().take(8).collect();
    if prefix.chars().count() < 8 || prefix.len() == token.len() {
        return "***".to_string();
    }
    format!("{prefix}...")
}

#[cfg(test)]
mod token_tests {
    use super::*;

    #[test]
    fn redacts_long_token() {
        assert_eq!(
            redact_token("supersecretrelaytoken1234567890"),
            "supersec..."
        );
    }

    #[test]
    fn masks_short_token() {
        assert_eq!(redact_token("short"), "***");
        assert_eq!(redact_token("exactly8"), "***");
        assert_eq!(redact_token(""), "***");
    }

    #[test]
    fn session_token_redacted_matches_helper() {
        let tok = SessionToken::generate();
        assert_eq!(tok.redacted(), redact_token(tok.as_str()));
        assert!(!tok.redacted().contains(&tok.as_str()[8..]));
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
