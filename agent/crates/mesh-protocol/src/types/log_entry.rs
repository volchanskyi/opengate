//! Log entry types for on-demand device log retrieval.

use serde::{Deserialize, Serialize};

/// A single parsed log entry from the agent's log files.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct LogEntry {
    /// ISO 8601 timestamp string.
    pub timestamp: String,
    /// Log level: "TRACE", "DEBUG", "INFO", "WARN", "ERROR".
    pub level: String,
    /// Module path, e.g. "mesh_agent::connection".
    pub target: String,
    /// Log message text.
    pub message: String,
}
