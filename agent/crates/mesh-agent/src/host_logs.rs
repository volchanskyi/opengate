//! Host log-rate readers for Edge-Sentinel (WS-9).
//!
//! Reads recent records from host log sources — the systemd journal, the Windows
//! Event Log, and the agent's own files — normalizes them to [`LogEntry`], and
//! folds a window into a fixed numeric log-rate feature vector for the WS-2
//! anomaly ensemble. Raw lines stay local; only level counts, per-unit ranks,
//! and volume leave the device. No message text ever becomes a feature or label.

use crate::logs::{LogCollector, LogFilter};
use mesh_agent_core::ml::log_rate::{LogRateExtractor, LOG_RATE_DIMS};
use mesh_agent_core::ml::redact::redact_log_line;
use mesh_protocol::{ControlMessage, LogEntry, MetricDim};
use std::io::{self, BufRead};
use std::path::Path;

/// A host log source the agent can read in addition to its own files.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum LogSource {
    /// The agent's own `tracing-appender` files (always available).
    AgentSelf,
    /// The systemd journal, read via `journalctl -o json`.
    Journald,
    /// The Windows Event Log, read via `Get-WinEvent`.
    WindowsEventLog,
}

/// Hard cap on host log lines parsed per collection to bound memory/CPU.
const MAX_HOST_LINES: usize = 5_000;

/// Maps a syslog priority (0=emerg … 7=debug) to a normalized level label.
/// journald's `PRIORITY` field uses this scale.
fn journald_priority_to_level(priority: u8) -> &'static str {
    match priority {
        0..=3 => "ERROR", // emerg, alert, crit, err
        4 => "WARN",      // warning
        5 | 6 => "INFO",  // notice, info
        _ => "DEBUG",     // debug (7) and anything beyond
    }
}

/// Maps a Windows Event Log level (1=Critical … 5=Verbose; 0=LogAlways) to a
/// normalized level label.
fn windows_level_to_label(level: i64) -> &'static str {
    match level {
        1 | 2 => "ERROR", // Critical, Error
        3 => "WARN",      // Warning
        5 => "DEBUG",     // Verbose
        _ => "INFO",      // Information (4), LogAlways (0), unknown
    }
}

/// Converts a journald `__REALTIME_TIMESTAMP` (microseconds since the Unix
/// epoch) into an ISO 8601 / RFC 3339 UTC string. Returns an empty string for a
/// value outside the representable range.
fn realtime_micros_to_iso(micros: i64) -> String {
    use chrono::{SecondsFormat, TimeZone, Utc};
    let secs = micros.div_euclid(1_000_000);
    let nanos = (micros.rem_euclid(1_000_000) * 1_000) as u32;
    match Utc.timestamp_opt(secs, nanos).single() {
        Some(dt) => dt.to_rfc3339_opts(SecondsFormat::Micros, true),
        None => String::new(),
    }
}

/// Reads a JSON string field, returning `None` for a missing or non-string value.
fn json_str(value: &serde_json::Value, key: &str) -> Option<String> {
    value.get(key).and_then(|v| v.as_str()).map(str::to_owned)
}

/// Parses one `journalctl -o json` line into a normalized [`LogEntry`]. A line
/// without a `MESSAGE` field is not a journal record and yields `None`.
fn parse_journald_json(line: &str) -> Option<LogEntry> {
    let value: serde_json::Value = serde_json::from_str(line).ok()?;
    let message = json_str(&value, "MESSAGE")?;
    // PRIORITY defaults to 6 (info) when absent or unparseable.
    let priority = json_str(&value, "PRIORITY")
        .and_then(|s| s.parse::<u8>().ok())
        .unwrap_or(6);
    let level = journald_priority_to_level(priority).to_string();
    let target = json_str(&value, "_SYSTEMD_UNIT")
        .or_else(|| json_str(&value, "SYSLOG_IDENTIFIER"))
        .unwrap_or_default();
    let timestamp = json_str(&value, "__REALTIME_TIMESTAMP")
        .and_then(|s| s.parse::<i64>().ok())
        .map(realtime_micros_to_iso)
        .unwrap_or_default();
    Some(LogEntry {
        timestamp,
        level,
        target,
        message,
    })
}

/// Parses one Windows Event Log record (a `Get-WinEvent | ConvertTo-Json`
/// object) into a normalized [`LogEntry`]. A record without a `Message` yields
/// `None`.
fn parse_windows_event_json(value: &serde_json::Value) -> Option<LogEntry> {
    let message = json_str(value, "Message")?;
    let level = value
        .get("Level")
        .and_then(serde_json::Value::as_i64)
        .map(windows_level_to_label)
        .unwrap_or("INFO")
        .to_string();
    let target = json_str(value, "ProviderName").unwrap_or_default();
    let timestamp = json_str(value, "TimeCreated").unwrap_or_default();
    Some(LogEntry {
        timestamp,
        level,
        target,
        message,
    })
}

/// Parses journald JSON-lines from a reader into normalized entries, stopping at
/// [`MAX_HOST_LINES`]. Malformed lines are skipped so one bad record never
/// aborts the scan.
fn read_journald_lines(reader: impl BufRead) -> Result<Vec<LogEntry>, io::Error> {
    let mut out = Vec::new();
    for line in reader.lines() {
        if out.len() >= MAX_HOST_LINES {
            break;
        }
        let line = line?;
        if line.trim().is_empty() {
            continue;
        }
        if let Some(entry) = parse_journald_json(&line) {
            out.push(entry);
        }
    }
    Ok(out)
}

/// Parses a Windows Event Log JSON document into normalized entries, capped at
/// [`MAX_HOST_LINES`]. `Get-WinEvent | ConvertTo-Json` emits a top-level array
/// for many records, or a bare object for exactly one; both are accepted.
fn parse_windows_events(json: &str) -> Vec<LogEntry> {
    let value: serde_json::Value = match serde_json::from_str(json) {
        Ok(value) => value,
        Err(_) => return Vec::new(),
    };
    let mut out = Vec::new();
    match value {
        serde_json::Value::Array(records) => {
            for record in records.iter().take(MAX_HOST_LINES) {
                if let Some(entry) = parse_windows_event_json(record) {
                    out.push(entry);
                }
            }
        }
        other => {
            if let Some(entry) = parse_windows_event_json(&other) {
                out.push(entry);
            }
        }
    }
    out
}

/// Folds a slice of normalized entries into the fixed-width log-rate feature
/// vector consumed by the WS-2 anomaly ensemble. Only level, unit rank, and
/// counts leave this function — never message content.
pub fn log_rate_vector(entries: &[LogEntry]) -> [f32; LOG_RATE_DIMS] {
    let mut extractor = LogRateExtractor::new();
    for entry in entries {
        extractor.observe_label(&entry.level, &entry.target);
    }
    extractor.finish()
}

/// Stable, bounded label for a host log source, embedded in metric dim names so
/// central series never grow with host shape.
fn source_label(source: LogSource) -> &'static str {
    match source {
        LogSource::AgentSelf => "self",
        LogSource::Journald => "journald",
        LogSource::WindowsEventLog => "windows",
    }
}

/// Field labels for the nine log-rate dims, in feature-vector slot order: five
/// severity levels, three top-emitting-unit ranks, then total volume.
const LOG_RATE_FIELD_LABELS: [&str; LOG_RATE_DIMS] = [
    "error",
    "warn",
    "info",
    "debug",
    "trace",
    "unit_rank1",
    "unit_rank2",
    "unit_rank3",
    "volume",
];

/// Names a log-rate feature vector as metric dims `log.rate.<source>.<field>`.
/// The names carry only level counts, top-unit ranks, and volume — never a unit
/// name or message text — so central cardinality stays bounded.
pub fn log_rate_dims(source: LogSource, vector: &[f32; LOG_RATE_DIMS]) -> Vec<MetricDim> {
    let label = source_label(source);
    LOG_RATE_FIELD_LABELS
        .iter()
        .zip(vector.iter())
        .map(|(field, &value)| MetricDim {
            name: format!("log.rate.{label}.{field}"),
            avg: f64::from(value),
        })
        .collect()
}

/// Builds the log-rate telemetry window for one source, or `None` when the
/// window has no records to report. The window rides the shared metric-window
/// message; the server assigns the authoritative org, so `org_id` is left empty.
pub fn build_log_rate_window(
    source: LogSource,
    entries: &[LogEntry],
    ts: i64,
) -> Option<ControlMessage> {
    if entries.is_empty() {
        return None;
    }
    let vector = log_rate_vector(entries);
    Some(ControlMessage::AgentMetricWindow {
        ts,
        org_id: String::new(),
        dims: log_rate_dims(source, &vector),
    })
}

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

/// Reads the most recent host log records for `source`, normalized and bounded
/// by [`MAX_HOST_LINES`]. Returns an empty vector whenever the source is
/// unavailable — a non-matching platform, a missing tool, or a read failure — so
/// the same call is safe on every fleet machine without platform branches at the
/// call site. `AgentSelf` reads the agent's own files under `self_dir`.
pub fn collect_host_logs(source: LogSource, self_dir: &Path) -> Vec<LogEntry> {
    match source {
        LogSource::AgentSelf => LogCollector::new(self_dir.to_path_buf())
            .collect(&LogFilter {
                level: None,
                time_from: None,
                time_to: None,
                search: None,
                offset: 0,
                limit: MAX_HOST_LINES as u32,
            })
            .map(|result| result.entries)
            .unwrap_or_default(),
        LogSource::Journald => collect_journald(MAX_HOST_LINES),
        LogSource::WindowsEventLog => collect_windows_events(MAX_HOST_LINES),
    }
}

/// Runs `journalctl -o json` for the most recent `max_entries` records. Empty on
/// any failure path (non-Linux host, missing binary, non-zero exit).
fn collect_journald(max_entries: usize) -> Vec<LogEntry> {
    let cap = max_entries.min(MAX_HOST_LINES).to_string();
    let output = std::process::Command::new("journalctl")
        .args(["-o", "json", "--no-pager", "-n", &cap])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            read_journald_lines(io::Cursor::new(output.stdout)).unwrap_or_default()
        }
        _ => Vec::new(),
    }
}

/// Runs `Get-WinEvent` for the most recent `max_entries` records. Empty on any
/// failure path (non-Windows host, missing PowerShell, non-zero exit).
fn collect_windows_events(max_entries: usize) -> Vec<LogEntry> {
    let cap = max_entries.min(MAX_HOST_LINES);
    let script = format!(
        "Get-WinEvent -MaxEvents {cap} | Select-Object TimeCreated,Level,ProviderName,Message | ConvertTo-Json -Compress"
    );
    let output = std::process::Command::new("powershell")
        .args(["-NoProfile", "-NonInteractive", "-Command", &script])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            parse_windows_events(&String::from_utf8_lossy(&output.stdout))
        }
        _ => Vec::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use tempfile::TempDir;

    /// Every journald priority band maps to the expected normalized level, and
    /// the band edges (3→ERROR, 4→WARN, 6→INFO, 7→DEBUG) are pinned.
    #[test]
    fn journald_priority_bands_map_to_levels() {
        assert_eq!(journald_priority_to_level(0), "ERROR");
        assert_eq!(journald_priority_to_level(3), "ERROR");
        assert_eq!(journald_priority_to_level(4), "WARN");
        assert_eq!(journald_priority_to_level(5), "INFO");
        assert_eq!(journald_priority_to_level(6), "INFO");
        assert_eq!(journald_priority_to_level(7), "DEBUG");
        assert_eq!(journald_priority_to_level(9), "DEBUG");
    }

    /// Every Windows Event Log level maps to the expected normalized level.
    #[test]
    fn windows_levels_map_to_labels() {
        assert_eq!(windows_level_to_label(1), "ERROR"); // Critical
        assert_eq!(windows_level_to_label(2), "ERROR"); // Error
        assert_eq!(windows_level_to_label(3), "WARN"); // Warning
        assert_eq!(windows_level_to_label(4), "INFO"); // Information
        assert_eq!(windows_level_to_label(5), "DEBUG"); // Verbose
        assert_eq!(windows_level_to_label(0), "INFO"); // LogAlways
    }

    /// `__REALTIME_TIMESTAMP` microseconds convert to an RFC 3339 UTC string,
    /// including the sub-second microseconds (which pins the `µs → ns` scaling).
    #[test]
    fn realtime_micros_convert_to_iso() {
        // 1_700_000_000_123_456 µs = 2023-11-14T22:13:20.123456Z.
        let iso = realtime_micros_to_iso(1_700_000_000_123_456);
        assert!(iso.starts_with("2023-11-14T22:13:20"), "got {iso}");
        assert!(iso.contains(".123456"), "sub-second µs must survive: {iso}");
        assert!(iso.ends_with('Z'), "must be UTC: {iso}");
    }

    #[test]
    fn parse_journald_json_normalizes_fields() {
        let line = r#"{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","_SYSTEMD_UNIT":"nginx.service","MESSAGE":"connection reset"}"#;
        let entry = parse_journald_json(line).expect("valid journald record");
        assert_eq!(entry.level, "ERROR");
        assert_eq!(entry.target, "nginx.service");
        assert_eq!(entry.message, "connection reset");
        assert!(entry.timestamp.starts_with("2023-11-14T"));
    }

    #[test]
    fn parse_journald_json_defaults_and_fallbacks() {
        // No PRIORITY → INFO; no _SYSTEMD_UNIT → SYSLOG_IDENTIFIER used as unit.
        let line = r#"{"SYSLOG_IDENTIFIER":"sshd","MESSAGE":"accepted login"}"#;
        let entry = parse_journald_json(line).expect("record without priority");
        assert_eq!(entry.level, "INFO", "absent PRIORITY defaults to info");
        assert_eq!(entry.target, "sshd");
        assert_eq!(entry.timestamp, "", "absent timestamp is empty");
    }

    #[test]
    fn parse_journald_json_rejects_non_records() {
        assert!(parse_journald_json("not json").is_none());
        // A JSON object without MESSAGE is not a log record.
        assert!(parse_journald_json(r#"{"PRIORITY":"3"}"#).is_none());
    }

    #[test]
    fn read_journald_lines_skips_blank_and_malformed() {
        let doc = concat!(
            r#"{"PRIORITY":"3","_SYSTEMD_UNIT":"a.service","MESSAGE":"err one"}"#,
            "\n",
            "\n", // blank line
            "garbage without json\n",
            r#"{"PRIORITY":"6","_SYSTEMD_UNIT":"b.service","MESSAGE":"info two"}"#,
            "\n",
        );
        let entries = read_journald_lines(io::Cursor::new(doc)).unwrap();
        assert_eq!(entries.len(), 2, "only the two valid records survive");
        assert_eq!(entries[0].level, "ERROR");
        assert_eq!(entries[1].level, "INFO");
    }

    #[test]
    fn parse_windows_events_accepts_array_and_object() {
        let array = r#"[
            {"TimeCreated":"2026-07-02T12:00:00","Level":2,"ProviderName":"App","Message":"boom"},
            {"TimeCreated":"2026-07-02T12:00:01","Level":3,"ProviderName":"Svc","Message":"warn"}
        ]"#;
        let entries = parse_windows_events(array);
        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].level, "ERROR");
        assert_eq!(entries[0].target, "App");
        assert_eq!(entries[1].level, "WARN");

        let single = r#"{"TimeCreated":"2026-07-02T12:00:02","Level":5,"ProviderName":"V","Message":"trace-ish"}"#;
        let one = parse_windows_events(single);
        assert_eq!(one.len(), 1);
        assert_eq!(one[0].level, "DEBUG");
    }

    #[test]
    fn parse_windows_events_rejects_bad_json_and_missing_message() {
        assert!(parse_windows_events("}{").is_empty());
        // Object without Message yields no entry.
        assert!(parse_windows_events(r#"{"Level":2,"ProviderName":"X"}"#).is_empty());
    }

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

    /// `collect_host_logs` with the `AgentSelf` source reads the agent's own
    /// rotated files under the given directory (the platform-independent path).
    #[test]
    fn collect_host_logs_reads_agent_self_files() {
        let dir = TempDir::new().unwrap();
        fs::write(
            dir.path().join("agent.log.2026-04-01"),
            "2026-04-01T12:00:00.000000Z  ERROR mesh_agent: boom\n\
             2026-04-01T12:00:01.000000Z  INFO mesh_agent: ok\n",
        )
        .unwrap();
        let entries = collect_host_logs(LogSource::AgentSelf, dir.path());
        assert_eq!(entries.len(), 2, "both self-log entries must be read");
        // An empty/missing directory yields no entries rather than erroring.
        let empty = TempDir::new().unwrap();
        assert!(collect_host_logs(LogSource::AgentSelf, empty.path()).is_empty());
    }

    /// The nine log-rate values map to metric dims named
    /// `log.rate.<source>.<field>`, in feature-vector slot order, carrying only
    /// counts — never a unit name or message text.
    #[test]
    fn log_rate_dims_name_by_source_and_field() {
        let mut vector = [0.0f32; LOG_RATE_DIMS];
        for (slot, value) in vector.iter_mut().enumerate() {
            *value = slot as f32; // 0,1,2,…,8 → one distinct value per slot
        }
        let dims = log_rate_dims(LogSource::Journald, &vector);
        assert_eq!(dims.len(), LOG_RATE_DIMS);
        assert_eq!(dims[0].name, "log.rate.journald.error");
        assert_eq!(dims[0].avg, 0.0);
        assert_eq!(dims[4].name, "log.rate.journald.trace");
        assert_eq!(dims[4].avg, 4.0);
        assert_eq!(dims[5].name, "log.rate.journald.unit_rank1");
        assert_eq!(dims[5].avg, 5.0);
        assert_eq!(dims[LOG_RATE_DIMS - 1].name, "log.rate.journald.volume");
        assert_eq!(dims[LOG_RATE_DIMS - 1].avg, 8.0);
    }

    /// Each source maps to a stable, bounded label so the metric name never
    /// grows with host shape.
    #[test]
    fn log_rate_dims_source_labels_are_bounded() {
        let vector = [0.0f32; LOG_RATE_DIMS];
        for (source, label) in [
            (LogSource::AgentSelf, "self"),
            (LogSource::Journald, "journald"),
            (LogSource::WindowsEventLog, "windows"),
        ] {
            let dims = log_rate_dims(source, &vector);
            assert_eq!(dims[0].name, format!("log.rate.{label}.error"));
        }
    }

    /// A non-empty window builds an `AgentMetricWindow` carrying the source's
    /// log-rate dims at the given timestamp; the server assigns the authoritative
    /// org, so the agent leaves `org_id` empty.
    #[test]
    fn build_log_rate_window_wraps_dims_with_empty_org() {
        let entries = vec![
            LogEntry {
                timestamp: "t".into(),
                level: "ERROR".into(),
                target: "busy.service".into(),
                message: "secret".into(),
            },
            LogEntry {
                timestamp: "t".into(),
                level: "INFO".into(),
                target: "busy.service".into(),
                message: "secret".into(),
            },
        ];
        let win = build_log_rate_window(LogSource::Journald, &entries, 1_700_000_000)
            .expect("non-empty window emits a metric window");
        match win {
            mesh_protocol::ControlMessage::AgentMetricWindow { ts, org_id, dims } => {
                assert_eq!(ts, 1_700_000_000);
                assert!(org_id.is_empty(), "agent must not assert an org");
                assert_eq!(dims.len(), LOG_RATE_DIMS);
                assert_eq!(dims[0].name, "log.rate.journald.error");
                assert_eq!(dims[0].avg, 1.0, "one ERROR entry");
                assert_eq!(dims[2].avg, 1.0, "one INFO entry");
                assert_eq!(dims[LOG_RATE_DIMS - 1].avg, 2.0, "two total");
            }
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        }
    }

    /// An empty window carries nothing to report, so no frame is produced.
    #[test]
    fn build_log_rate_window_is_none_when_empty() {
        assert!(build_log_rate_window(LogSource::Journald, &[], 1).is_none());
    }

    /// The log-rate vector counts levels and ranks units without carrying any
    /// message text — the core WS-9 privacy invariant.
    #[test]
    fn log_rate_vector_counts_levels_and_ranks_units() {
        let entries = vec![
            LogEntry {
                timestamp: "t".into(),
                level: "ERROR".into(),
                target: "busy.service".into(),
                message: "secret payload one".into(),
            },
            LogEntry {
                timestamp: "t".into(),
                level: "ERROR".into(),
                target: "busy.service".into(),
                message: "secret payload two".into(),
            },
            LogEntry {
                timestamp: "t".into(),
                level: "INFO".into(),
                target: "quiet.service".into(),
                message: "secret payload three".into(),
            },
        ];
        let v = log_rate_vector(&entries);
        assert_eq!(v[0], 2.0, "two ERROR entries");
        assert_eq!(v[2], 1.0, "one INFO entry");
        assert_eq!(v[5], 2.0, "busiest unit rank-1 count");
        assert_eq!(v[6], 1.0, "second unit rank-2 count");
        assert_eq!(v[LOG_RATE_DIMS - 1], 3.0, "total volume");
    }
}
