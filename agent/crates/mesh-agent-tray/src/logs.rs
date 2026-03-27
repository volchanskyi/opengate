//! Log file viewing and tail operations.

use std::process::Command;

use tracing::{info, warn};

/// Default log file path matching the agent's tracing-appender output.
const DEFAULT_LOG_PATH: &str = "/var/log/mesh-agent";

/// Open the current log file in the system's default text editor.
pub fn open_log_file(log_path: &str) {
    // Find the most recent log file in the directory.
    // tracing-appender creates files like "agent.log.2026-03-26".
    let path = find_current_log(log_path);
    info!(path = %path, "opening log file");

    if let Err(e) = open::that(&path) {
        warn!(error = %e, path = %path, "failed to open log file");
        super::notifications::notify("Error", &format!("Cannot open log file: {e}"));
    }
}

/// Spawn a terminal running `tail -f` on the current log file.
pub fn tail_live_logs(log_path: &str) {
    let path = find_current_log(log_path);
    info!(path = %path, "tailing live logs");

    // Try common terminal emulators in order of preference.
    let terminals = [
        ("x-terminal-emulator", vec!["-e"]),
        ("gnome-terminal", vec!["--"]),
        ("konsole", vec!["-e"]),
        ("xfce4-terminal", vec!["-e"]),
        ("xterm", vec!["-e"]),
    ];

    for (terminal, args) in &terminals {
        let result = Command::new(terminal)
            .args(args)
            .args(["tail", "-f", &path])
            .spawn();

        match result {
            Ok(_) => {
                info!(terminal, "spawned tail in terminal");
                return;
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
            Err(e) => {
                warn!(terminal, error = %e, "failed to spawn terminal");
                continue;
            }
        }
    }

    warn!("no terminal emulator found, falling back to open::that");
    let _ = open::that(&path);
}

/// Spawn a terminal running `journalctl -fu mesh-agent`.
pub fn open_journalctl() {
    info!("opening journalctl for mesh-agent");

    let terminals = [
        ("x-terminal-emulator", vec!["-e"]),
        ("gnome-terminal", vec!["--"]),
        ("konsole", vec!["-e"]),
        ("xfce4-terminal", vec!["-e"]),
        ("xterm", vec!["-e"]),
    ];

    for (terminal, args) in &terminals {
        let result = Command::new(terminal)
            .args(args)
            .args(["journalctl", "-fu", "mesh-agent"])
            .spawn();

        match result {
            Ok(_) => {
                info!(terminal, "spawned journalctl in terminal");
                return;
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
            Err(e) => {
                warn!(terminal, error = %e, "failed to spawn terminal");
                continue;
            }
        }
    }

    warn!("no terminal emulator found for journalctl");
    super::notifications::notify("Error", "No terminal emulator found");
}

/// Find the most recent log file. tracing-appender creates dated files.
/// Returns the log_path as-is if it looks like a file, or finds the
/// newest file in the directory.
fn find_current_log(log_path: &str) -> String {
    let path = std::path::Path::new(log_path);

    // If log_path is a file that exists, use it directly.
    if path.is_file() {
        return log_path.to_string();
    }

    // If it's a directory, find the newest .log file.
    if path.is_dir() {
        if let Ok(entries) = std::fs::read_dir(path) {
            let mut files: Vec<_> = entries
                .flatten()
                .filter(|e| e.file_name().to_string_lossy().starts_with("agent.log"))
                .collect();

            files.sort_by_key(|e| {
                e.metadata()
                    .ok()
                    .and_then(|m| m.modified().ok())
                    .unwrap_or(std::time::SystemTime::UNIX_EPOCH)
            });

            if let Some(newest) = files.last() {
                return newest.path().to_string_lossy().to_string();
            }
        }
    }

    // Fallback: try the directory containing the path.
    if let Some(parent) = path.parent() {
        if parent.is_dir() {
            return find_current_log(&parent.to_string_lossy());
        }
    }

    // Last resort: return the default path.
    format!("{DEFAULT_LOG_PATH}/agent.log")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_find_current_log_with_file() {
        let dir = tempfile::tempdir().unwrap();
        let log_file = dir.path().join("agent.log");
        std::fs::write(&log_file, "test").unwrap();

        let result = find_current_log(&log_file.to_string_lossy());
        assert_eq!(result, log_file.to_string_lossy());
    }

    #[test]
    fn test_find_current_log_with_directory() {
        let dir = tempfile::tempdir().unwrap();
        let log1 = dir.path().join("agent.log.2026-03-25");
        let log2 = dir.path().join("agent.log.2026-03-26");
        std::fs::write(&log1, "old").unwrap();
        // Small delay to ensure different modification times
        std::fs::write(&log2, "new").unwrap();

        let result = find_current_log(&dir.path().to_string_lossy());
        assert!(result.contains("agent.log"));
    }

    #[test]
    fn test_find_current_log_nonexistent_returns_default() {
        let result = find_current_log("/nonexistent/path/agent.log");
        assert!(result.contains("agent.log"));
    }
}
