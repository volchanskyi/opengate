//! Edge-side redaction for raw host log responses.
//!
//! Raw log lines are secret-dense, so every entry's message is scrubbed on the
//! device before a raw-log response leaves it — the first of two independent
//! redaction layers (the server applies the second). Only the free-text message
//! body is touched; the level, timestamp, and unit target are bounded normalized
//! fields and are left intact.

use mesh_agent_core::ml::redact::redact_log_line;
use mesh_protocol::LogEntry;

/// Redacts secret material from each entry's message in place before a raw-log
/// response leaves the device. Raw log lines are secret-dense, so this edge-side
/// pass is the first of two independent redaction layers (the server applies the
/// second). Only the message body carries free text; the level, timestamp, and
/// unit target are bounded normalized fields and are left untouched.
pub fn redact_entries(entries: &mut [LogEntry]) {
    for entry in entries.iter_mut() {
        entry.message = redact_log_line(&entry.message);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// `redact_entries` strips secret material from each message while leaving
    /// the bounded normalized fields (level, timestamp, target) intact.
    #[test]
    fn redact_entries_scrubs_message_secrets() {
        let mut entries = vec![
            LogEntry {
                timestamp: "2026-04-01T12:00:00Z".into(),
                level: "ERROR".into(),
                target: "nginx.service".into(),
                message: "auth failed for password=hunter2secret retrying".into(),
            },
            LogEntry {
                timestamp: "2026-04-01T12:00:01Z".into(),
                level: "INFO".into(),
                target: "app".into(),
                message: "handled request in 4ms".into(),
            },
        ];
        redact_entries(&mut entries);
        assert!(
            !entries[0].message.contains("hunter2secret"),
            "secret must be stripped: {}",
            entries[0].message
        );
        // Bounded normalized fields are untouched.
        assert_eq!(entries[0].level, "ERROR");
        assert_eq!(entries[0].target, "nginx.service");
        // A benign message is returned unchanged.
        assert_eq!(entries[1].message, "handled request in 4ms");
    }
}
