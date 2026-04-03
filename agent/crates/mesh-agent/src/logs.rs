//! On-demand log collection from agent log files.
//!
//! Reads daily-rotated log files produced by `tracing-appender::rolling::daily`,
//! parses tracing-subscriber format, and returns filtered/paginated results.

use mesh_protocol::LogEntry;
use std::fs;
use std::io::{self, BufRead};
use std::path::PathBuf;

/// Maximum number of log files to scan (1 week of dailies).
const MAX_LOG_FILES: usize = 7;

/// Maximum number of lines to scan per request to bound memory/CPU.
const MAX_SCAN_LINES: usize = 10_000;

/// Default page size when limit is 0 (omitted by server).
const DEFAULT_LIMIT: usize = 300;

/// Collects and filters log entries from the agent's log directory.
pub struct LogCollector {
    log_dir: PathBuf,
}

/// Filter criteria for log collection.
pub struct LogFilter {
    /// Minimum log level (severity-based: WARN includes WARN+ERROR).
    pub level: Option<String>,
    /// ISO 8601 lower bound (inclusive).
    pub time_from: Option<String>,
    /// ISO 8601 upper bound (inclusive).
    pub time_to: Option<String>,
    /// Substring search on message field.
    pub search: Option<String>,
    /// Pagination offset (number of matching entries to skip).
    pub offset: u32,
    /// Maximum entries to return.
    pub limit: u32,
}

/// Result of a log collection request.
pub struct LogResult {
    /// Matching log entries for the requested page.
    pub entries: Vec<LogEntry>,
    /// Total number of matching entries (for pagination UI).
    pub total_count: u32,
    /// Whether more entries exist beyond offset+limit.
    pub has_more: bool,
}

/// Log level severity ordering.
fn level_severity(level: &str) -> Option<u8> {
    match level {
        "TRACE" => Some(0),
        "DEBUG" => Some(1),
        "INFO" => Some(2),
        "WARN" => Some(3),
        "ERROR" => Some(4),
        _ => None,
    }
}

impl LogCollector {
    /// Creates a new collector targeting the given log directory.
    pub fn new(log_dir: PathBuf) -> Self {
        Self { log_dir }
    }

    /// Collects log entries matching the given filter.
    pub fn collect(&self, filter: &LogFilter) -> Result<LogResult, io::Error> {
        if !self.log_dir.exists() {
            return Err(io::Error::new(
                io::ErrorKind::NotFound,
                format!("log directory not found: {}", self.log_dir.display()),
            ));
        }

        let files = self.discover_log_files()?;
        let mut all_entries = Vec::new();
        let mut lines_scanned = 0usize;
        let mut current_entry: Option<LogEntry> = None;

        for file_path in &files {
            if lines_scanned >= MAX_SCAN_LINES {
                break;
            }
            let file = fs::File::open(file_path)?;
            let reader = io::BufReader::new(file);

            for line in reader.lines() {
                if lines_scanned >= MAX_SCAN_LINES {
                    break;
                }
                lines_scanned += 1;

                let line = line?;
                if line.is_empty() {
                    continue;
                }

                if let Some(entry) = parse_log_line(&line) {
                    // Flush previous entry
                    if let Some(prev) = current_entry.take() {
                        if matches_filter(&prev, filter) {
                            all_entries.push(prev);
                        }
                    }
                    current_entry = Some(entry);
                } else if let Some(ref mut entry) = current_entry {
                    // Continuation line — append to previous entry's message
                    entry.message.push('\n');
                    entry.message.push_str(&line);
                }
                // else: garbage line before any valid entry — skip
            }
        }

        // Flush last entry
        if let Some(prev) = current_entry.take() {
            if matches_filter(&prev, filter) {
                all_entries.push(prev);
            }
        }

        // Sort newest-first by timestamp. Files are scanned newest-first (for
        // scan-budget efficiency) but lines within each file are chronological,
        // so a simple reverse wouldn't produce correct ordering.
        all_entries.sort_by(|a, b| b.timestamp.cmp(&a.timestamp));

        let total_count = all_entries.len() as u32;
        let offset = filter.offset as usize;
        let limit = if filter.limit == 0 { DEFAULT_LIMIT } else { filter.limit as usize };
        let has_more = offset + limit < all_entries.len();

        let entries = all_entries.into_iter().skip(offset).take(limit).collect();

        Ok(LogResult {
            entries,
            total_count,
            has_more,
        })
    }

    /// Discovers log files sorted by name (newest first).
    /// tracing-appender daily rotation produces files like:
    ///   agent.log.2026-04-01
    ///   agent.log.2026-04-02
    fn discover_log_files(&self) -> Result<Vec<PathBuf>, io::Error> {
        let mut files: Vec<PathBuf> = fs::read_dir(&self.log_dir)?
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| {
                p.file_name()
                    .and_then(|n| n.to_str())
                    .is_some_and(|n| n.starts_with("agent.log"))
            })
            .collect();

        files.sort();

        // Keep only the most recent files
        if files.len() > MAX_LOG_FILES {
            files = files.split_off(files.len() - MAX_LOG_FILES);
        }

        // Reverse so newest files are scanned first (avoids wasting
        // MAX_SCAN_LINES budget on old entries).
        files.reverse();

        Ok(files)
    }
}

/// Parses a single log line in tracing-subscriber format:
/// `2026-04-01T12:34:56.789012Z  INFO mesh_agent::connection: connected to server`
fn parse_log_line(line: &str) -> Option<LogEntry> {
    // Timestamp must start with a digit (year)
    if !line.starts_with(|c: char| c.is_ascii_digit()) {
        return None;
    }

    // Split: timestamp <whitespace> level <whitespace> target: message
    let mut parts = line.splitn(2, |c: char| c.is_whitespace());
    let timestamp = parts.next()?.trim();

    // Validate timestamp looks like ISO 8601
    if timestamp.len() < 20 || !timestamp.contains('T') {
        return None;
    }

    let rest = parts.next()?.trim_start();

    // Level is the next non-whitespace token
    let mut parts = rest.splitn(2, |c: char| c.is_whitespace());
    let level = parts.next()?.trim();

    // Validate level
    level_severity(level)?;

    let rest = parts.next().unwrap_or("").trim_start();

    // Target and message split at first ": "
    let (target, message) = if let Some(pos) = rest.find(": ") {
        (&rest[..pos], rest[pos + 2..].to_string())
    } else {
        // No target separator — entire rest is the message
        ("", rest.to_string())
    };

    Some(LogEntry {
        timestamp: timestamp.to_string(),
        level: level.to_string(),
        target: target.to_string(),
        message,
    })
}

/// Checks if an entry matches the filter criteria.
fn matches_filter(entry: &LogEntry, filter: &LogFilter) -> bool {
    // Level filter (severity-based: WARN includes WARN+ERROR)
    if let Some(ref min_level) = filter.level {
        if let (Some(min_sev), Some(entry_sev)) =
            (level_severity(min_level), level_severity(&entry.level))
        {
            if entry_sev < min_sev {
                return false;
            }
        }
    }

    // Time range filter (ISO 8601 string comparison works correctly)
    if let Some(ref from) = filter.time_from {
        if entry.timestamp.as_str() < from.as_str() {
            return false;
        }
    }
    if let Some(ref to) = filter.time_to {
        if entry.timestamp.as_str() > to.as_str() {
            return false;
        }
    }

    // Keyword search (case-insensitive substring match on message)
    if let Some(ref search) = filter.search {
        if !entry
            .message
            .to_lowercase()
            .contains(&search.to_lowercase())
        {
            return false;
        }
    }

    true
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::path::Path;
    use tempfile::TempDir;

    fn write_log_file(dir: &Path, name: &str, content: &str) {
        fs::write(dir.join(name), content).unwrap();
    }

    fn default_filter() -> LogFilter {
        LogFilter {
            level: None,
            time_from: None,
            time_to: None,
            search: None,
            offset: 0,
            limit: 100,
        }
    }

    // --- Positive cases ---

    #[test]
    fn test_parse_standard_tracing_format() {
        let line = "2026-04-01T12:34:56.789012Z  INFO mesh_agent::connection: connected to server";
        let entry = parse_log_line(line).unwrap();
        assert_eq!(entry.timestamp, "2026-04-01T12:34:56.789012Z");
        assert_eq!(entry.level, "INFO");
        assert_eq!(entry.target, "mesh_agent::connection");
        assert_eq!(entry.message, "connected to server");
    }

    #[test]
    fn test_parse_multi_line_entry() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "2026-04-01T12:00:00.000000Z  ERROR mesh_agent::main: panic occurred\n  at src/main.rs:42\n  note: stack trace follows\n2026-04-01T12:00:01.000000Z  INFO mesh_agent::main: recovered\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let result = collector.collect(&default_filter()).unwrap();

        // Newest-first ordering
        assert_eq!(result.entries.len(), 2);
        assert_eq!(result.entries[0].level, "INFO");
        assert_eq!(result.entries[0].message, "recovered");
        assert_eq!(result.entries[1].level, "ERROR");
        assert!(result.entries[1].message.contains("panic occurred"));
        assert!(result.entries[1].message.contains("at src/main.rs:42"));
        assert!(result.entries[1].message.contains("stack trace follows"));
    }

    #[test]
    fn test_filter_by_level() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "2026-04-01T12:00:00.000000Z  DEBUG mesh_agent: debug msg\n\
             2026-04-01T12:00:01.000000Z  INFO mesh_agent: info msg\n\
             2026-04-01T12:00:02.000000Z  WARN mesh_agent: warn msg\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let filter = LogFilter {
            level: Some("INFO".to_string()),
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();

        // Newest-first: WARN before INFO
        assert_eq!(result.entries.len(), 2);
        assert_eq!(result.entries[0].level, "WARN");
        assert_eq!(result.entries[1].level, "INFO");
    }

    #[test]
    fn test_filter_by_level_severity() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "2026-04-01T12:00:00.000000Z  TRACE mesh_agent: trace\n\
             2026-04-01T12:00:01.000000Z  DEBUG mesh_agent: debug\n\
             2026-04-01T12:00:02.000000Z  INFO mesh_agent: info\n\
             2026-04-01T12:00:03.000000Z  WARN mesh_agent: warn\n\
             2026-04-01T12:00:04.000000Z  ERROR mesh_agent: error\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let filter = LogFilter {
            level: Some("WARN".to_string()),
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();

        // Newest-first: ERROR before WARN
        assert_eq!(result.entries.len(), 2);
        assert_eq!(result.entries[0].level, "ERROR");
        assert_eq!(result.entries[1].level, "WARN");
    }

    #[test]
    fn test_filter_by_time_range() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "2026-04-01T10:00:00.000000Z  INFO mesh_agent: early\n\
             2026-04-01T12:00:00.000000Z  INFO mesh_agent: midday\n\
             2026-04-01T14:00:00.000000Z  INFO mesh_agent: afternoon\n\
             2026-04-01T18:00:00.000000Z  INFO mesh_agent: evening\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let filter = LogFilter {
            time_from: Some("2026-04-01T11:00:00Z".to_string()),
            time_to: Some("2026-04-01T15:00:00Z".to_string()),
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();

        // Newest-first: afternoon before midday
        assert_eq!(result.entries.len(), 2);
        assert_eq!(result.entries[0].message, "afternoon");
        assert_eq!(result.entries[1].message, "midday");
    }

    #[test]
    fn test_filter_by_keyword() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "2026-04-01T12:00:00.000000Z  INFO mesh_agent: connected to server\n\
             2026-04-01T12:00:01.000000Z  WARN mesh_agent: slow heartbeat\n\
             2026-04-01T12:00:02.000000Z  ERROR mesh_agent: connection lost\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let filter = LogFilter {
            search: Some("connect".to_string()),
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();

        // Newest-first: "connection lost" before "connected to server"
        assert_eq!(result.entries.len(), 2);
        assert!(result.entries[0].message.contains("connection"));
        assert!(result.entries[1].message.contains("connected"));
    }

    #[test]
    fn test_pagination_offset_limit() {
        let dir = TempDir::new().unwrap();
        let mut content = String::new();
        // Use unique timestamps: hour 10-11, minutes 0-99 spread across
        for i in 0..100u32 {
            let hour = 10 + i / 60;
            let min = i % 60;
            content.push_str(&format!(
                "2026-04-01T{:02}:{:02}:00.000000Z  INFO mesh_agent: line {}\n",
                hour, min, i
            ));
        }
        write_log_file(dir.path(), "agent.log.2026-04-01", &content);

        let collector = LogCollector::new(dir.path().to_path_buf());
        let filter = LogFilter {
            offset: 50,
            limit: 25,
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();

        // Newest-first: entries are 99,98,...,0. offset=50 skips 99..50 → starts at 49.
        assert_eq!(result.entries.len(), 25);
        assert_eq!(result.entries[0].message, "line 49");
        assert_eq!(result.entries[24].message, "line 25");
        assert_eq!(result.total_count, 100);
    }

    #[test]
    fn test_pagination_has_more() {
        let dir = TempDir::new().unwrap();
        let mut content = String::new();
        for i in 0..50 {
            content.push_str(&format!(
                "2026-04-01T12:{:02}:00.000000Z  INFO mesh_agent: line {}\n",
                i % 60,
                i
            ));
        }
        write_log_file(dir.path(), "agent.log.2026-04-01", &content);

        let collector = LogCollector::new(dir.path().to_path_buf());

        // has_more = true
        let filter = LogFilter {
            offset: 0,
            limit: 25,
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();
        assert!(result.has_more);
        assert_eq!(result.total_count, 50);

        // has_more = false (exactly at boundary)
        let filter = LogFilter {
            offset: 25,
            limit: 25,
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();
        assert!(!result.has_more);

        // has_more = false (beyond)
        let filter = LogFilter {
            offset: 0,
            limit: 100,
            ..default_filter()
        };
        let result = collector.collect(&filter).unwrap();
        assert!(!result.has_more);
    }

    #[test]
    fn test_multiple_log_files() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "2026-04-01T12:00:00.000000Z  INFO mesh_agent: day1\n",
        );
        write_log_file(
            dir.path(),
            "agent.log.2026-04-02",
            "2026-04-02T12:00:00.000000Z  INFO mesh_agent: day2\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let result = collector.collect(&default_filter()).unwrap();

        // Newest-first: day2 before day1
        assert_eq!(result.entries.len(), 2);
        assert_eq!(result.entries[0].message, "day2");
        assert_eq!(result.entries[1].message, "day1");
    }

    // --- Negative cases ---

    #[test]
    fn test_empty_log_dir() {
        let dir = TempDir::new().unwrap();
        let collector = LogCollector::new(dir.path().to_path_buf());
        let result = collector.collect(&default_filter()).unwrap();
        assert!(result.entries.is_empty());
        assert_eq!(result.total_count, 0);
    }

    #[test]
    fn test_missing_log_dir() {
        let collector = LogCollector::new(PathBuf::from("/nonexistent/path/logs"));
        let result = collector.collect(&default_filter());
        assert!(result.is_err());
    }

    #[test]
    fn test_malformed_line_skipped() {
        let dir = TempDir::new().unwrap();
        write_log_file(
            dir.path(),
            "agent.log.2026-04-01",
            "garbage line without timestamp\n\
             another bad line\n\
             2026-04-01T12:00:00.000000Z  INFO mesh_agent: valid entry\n\
             not a timestamp but after valid\n",
        );

        let collector = LogCollector::new(dir.path().to_path_buf());
        let result = collector.collect(&default_filter()).unwrap();

        // Only the valid entry (with continuation line appended)
        assert_eq!(result.entries.len(), 1);
        assert_eq!(result.entries[0].level, "INFO");
        assert!(result.entries[0].message.contains("valid entry"));
    }

    #[test]
    fn test_empty_file() {
        let dir = TempDir::new().unwrap();
        write_log_file(dir.path(), "agent.log.2026-04-01", "");

        let collector = LogCollector::new(dir.path().to_path_buf());
        let result = collector.collect(&default_filter()).unwrap();
        assert!(result.entries.is_empty());
        assert_eq!(result.total_count, 0);
    }
}
