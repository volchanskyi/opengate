//! Host system-log collection: reads the systemd journal into [`LogEntry`] values and lists
//! its units, with [`redact_entries`] scrubbing messages before they leave the device.

use crate::logs::{LogFilter, LogResult};
use mesh_agent_core::ml::redact::redact_log_line;
use mesh_protocol::LogEntry;
use std::io::{self, BufRead};
use std::sync::atomic::{AtomicU64, Ordering};
use tracing::warn;

/// Cap on host log lines parsed per collection.
const MAX_HOST_LINES: usize = 5_000;

/// Cap on distinct units returned, applied after sorting; the unit filter still accepts others.
const MAX_UNITS: usize = 200;

/// A platform host log source the agent can read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum LogSource {
    /// The systemd journal, read via `journalctl -o json`.
    Journald,
}

/// A host log source with no reader here, carrying the requested name for the refusal.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UnavailableSource(String);

impl std::fmt::Display for UnavailableSource {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "host log source {:?} is not available on this host",
            self.0
        )
    }
}

impl std::error::Error for UnavailableSource {}

/// Running total of host-log requests refused for naming a source with no reader.
static UNAVAILABLE_SOURCE_REQUESTS: AtomicU64 = AtomicU64::new(0);

/// Records one refusal and builds the answer naming the source; the log line carries the total.
fn refuse(name: &str) -> UnavailableSource {
    UNAVAILABLE_SOURCE_REQUESTS.fetch_add(1, Ordering::Relaxed);
    let refused_total = UNAVAILABLE_SOURCE_REQUESTS.load(Ordering::Relaxed);
    warn!(
        source = name,
        refused_total, "host log source is not available on this host"
    );
    UnavailableSource(name.to_string())
}

/// The host log source for the current platform, or `None` where it has none.
#[must_use]
pub fn resolve_host_source() -> Option<LogSource> {
    match std::env::consts::OS {
        "linux" => Some(LogSource::Journald),
        _ => None,
    }
}

/// Resolves the source a `RequestDeviceLogs` names: `host` picks the platform source, and an
/// unreadable name, an unknown name or `self` is refused by name, since an empty page misleads.
pub fn resolve_requested_source(name: &str) -> Result<LogSource, UnavailableSource> {
    match (name, resolve_host_source()) {
        ("host", Some(source)) => Ok(source),
        ("journald", Some(LogSource::Journald)) => Ok(LogSource::Journald),
        _ => Err(refuse(name)),
    }
}

/// Maps a syslog priority (0=emerg … 7=debug), journald's `PRIORITY` scale, to a level label.
fn journald_priority_to_level(priority: u8) -> &'static str {
    match priority {
        0..=3 => "ERROR", // emerg, alert, crit, err
        4 => "WARN",      // warning
        5 | 6 => "INFO",  // notice, info
        _ => "DEBUG",     // debug (7) and anything beyond
    }
}

/// The journald `-p` ceiling for a minimum severity, or `None` when every priority matches;
/// `-p N` selects priorities `<= N`, so min WARN includes ERROR.
fn journald_priority_ceiling(min_level: &str) -> Option<&'static str> {
    match min_level {
        "ERROR" => Some("3"),
        "WARN" => Some("4"),
        "INFO" => Some("6"),
        _ => None, // DEBUG / TRACE / unknown: no ceiling
    }
}

/// Parses an RFC 3339 timestamp into whole Unix seconds, or `None` when invalid.
fn iso_to_epoch(ts: &str) -> Option<i64> {
    chrono::DateTime::parse_from_rfc3339(ts)
        .ok()
        .map(|dt| dt.timestamp())
}

/// Builds the `journalctl -o json` argv; each value is one token, so a hostile `unit` matches
/// a single `_SYSTEMD_UNIT=` value and cannot inject a command.
fn build_journald_args(filter: &LogFilter, unit: &str) -> Vec<String> {
    let mut args = vec![
        "-o".to_string(),
        "json".to_string(),
        "--no-pager".to_string(),
        "-n".to_string(),
        MAX_HOST_LINES.to_string(),
    ];
    if let Some(ceiling) = filter.level.as_deref().and_then(journald_priority_ceiling) {
        args.push("-p".to_string());
        args.push(ceiling.to_string());
    }
    if let Some(from) = filter.time_from.as_deref().and_then(iso_to_epoch) {
        args.push("--since".to_string());
        args.push(format!("@{from}"));
    }
    if let Some(to) = filter.time_to.as_deref().and_then(iso_to_epoch) {
        args.push("--until".to_string());
        args.push(format!("@{to}"));
    }
    if !unit.is_empty() {
        args.push(format!("_SYSTEMD_UNIT={unit}"));
    }
    args
}

/// Reads a JSON string field, returning `None` for a missing or non-string value.
fn json_str(value: &serde_json::Value, key: &str) -> Option<String> {
    value.get(key).and_then(|v| v.as_str()).map(str::to_owned)
}

/// Converts a journald `__REALTIME_TIMESTAMP` (Unix microseconds) to an RFC 3339 UTC string,
/// empty when out of range.
fn realtime_micros_to_iso(micros: i64) -> String {
    use chrono::{SecondsFormat, TimeZone, Utc};
    let secs = micros.div_euclid(1_000_000);
    let nanos = (micros.rem_euclid(1_000_000) * 1_000) as u32;
    match Utc.timestamp_opt(secs, nanos).single() {
        Some(dt) => dt.to_rfc3339_opts(SecondsFormat::Micros, true),
        None => String::new(),
    }
}

/// Parses one `journalctl -o json` line into a [`LogEntry`]; a line without `MESSAGE` is `None`.
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

/// Parses journald JSON lines into entries up to [`MAX_HOST_LINES`], skipping malformed lines.
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

/// Reduces unit tokens to the distinct, non-empty, sorted set capped at [`MAX_UNITS`].
fn normalize_unit_list(raw: impl IntoIterator<Item = String>) -> Vec<String> {
    let mut units: Vec<String> = raw
        .into_iter()
        .map(|u| u.trim().to_string())
        .filter(|u| !u.is_empty())
        .collect();
    units.sort();
    units.dedup();
    units.truncate(MAX_UNITS);
    units
}

/// Reads the latest records for `source` with the level, time and unit filters; empty when the
/// tool is missing or fails. `source` comes from [`resolve_requested_source`].
pub fn collect_host_logs(source: LogSource, filter: &LogFilter, unit: &str) -> Vec<LogEntry> {
    match source {
        LogSource::Journald => collect_journald(filter, unit),
    }
}

/// Whether a batch reached the [`MAX_HOST_LINES`] cap, meaning an unknown number of older
/// records fell off the window.
#[must_use]
pub fn batch_saturated(entries: &[LogEntry]) -> bool {
    entries.len() >= MAX_HOST_LINES
}

/// Enumerates the distinct emitting units for `source` (systemd units),
/// normalized for the UI dropdown. Empty on any failure path.
pub fn list_units(source: LogSource) -> Vec<String> {
    match source {
        LogSource::Journald => list_journald_units(),
    }
}

/// Collects host logs for a source other than the agent's own files, applying the shared filter
/// and pagination; a source with no reader here is refused by name.
pub fn collect_system_logs(
    source: &str,
    filter: &LogFilter,
    unit: &str,
) -> Result<(LogResult, Vec<String>), UnavailableSource> {
    let source = resolve_requested_source(source)?;
    let raw = collect_host_logs(source, filter, unit);
    let filtered = crate::logs::filter_entries(raw, filter);
    let result = crate::logs::paginate(filtered, filter);
    Ok((result, list_units(source)))
}

/// The parsed stdout of a successful `journalctl` read; a non-zero exit discards partial output.
fn entries_from_exit(success: bool, stdout: &[u8]) -> Vec<LogEntry> {
    if !success {
        return Vec::new();
    }
    read_journald_lines(io::Cursor::new(stdout)).unwrap_or_default()
}

/// The units of a successful enumeration; a non-zero exit yields none.
fn units_from_exit(success: bool, stdout: &[u8]) -> Vec<String> {
    if !success {
        return Vec::new();
    }
    let text = String::from_utf8_lossy(stdout);
    normalize_unit_list(text.lines().map(str::to_owned))
}

/// Runs `journalctl` with `args` and hands the result to `parse`; a launch failure yields the
/// same empty answer as a failed run.
fn run_journalctl<T>(
    args: impl IntoIterator<Item = String>,
    parse: fn(bool, &[u8]) -> Vec<T>,
) -> Vec<T> {
    match std::process::Command::new("journalctl").args(args).output() {
        Ok(output) => parse(output.status.success(), &output.stdout),
        Err(_) => Vec::new(),
    }
}

/// Runs `journalctl` for the most recent records under the pushed-down filter.
/// Empty on any failure path (missing binary, non-zero exit).
fn collect_journald(filter: &LogFilter, unit: &str) -> Vec<LogEntry> {
    run_journalctl(build_journald_args(filter, unit), entries_from_exit)
}

/// Enumerates distinct systemd units via `journalctl -F _SYSTEMD_UNIT`; empty on failure.
fn list_journald_units() -> Vec<String> {
    run_journalctl(
        ["-F", "_SYSTEMD_UNIT", "--no-pager"].map(String::from),
        units_from_exit,
    )
}

/// Redacts secrets from each message in place, the device's layer of two (the server applies
/// the second); level, timestamp and unit are bounded fields and stay untouched.
pub fn redact_entries(entries: &mut [LogEntry]) {
    for entry in entries.iter_mut() {
        entry.message = redact_log_line(&entry.message);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn filter(level: Option<&str>, from: Option<&str>, to: Option<&str>) -> LogFilter {
        LogFilter {
            level: level.map(str::to_owned),
            time_from: from.map(str::to_owned),
            time_to: to.map(str::to_owned),
            search: None,
            offset: 0,
            limit: 0,
        }
    }

    #[test]
    fn host_source_matches_the_build_target() {
        #[cfg(target_os = "linux")]
        {
            assert_eq!(resolve_host_source(), Some(LogSource::Journald));
            assert_eq!(
                resolve_requested_source("host").ok(),
                Some(LogSource::Journald),
                "`host` is answered with this platform's own reader"
            );
            assert_eq!(
                resolve_requested_source("journald").ok(),
                Some(LogSource::Journald),
                "naming journald outright is answered where the agent reads it"
            );
        }
        // Bypasses `resolve_requested_source`, so only the refusal test moves the counter.
        #[cfg(not(target_os = "linux"))]
        assert_eq!(resolve_host_source(), None);
    }

    #[test]
    fn refused_sources_are_named_and_counted() {
        let before = UNAVAILABLE_SOURCE_REQUESTS.load(Ordering::Relaxed);

        let refused = collect_system_logs("windows", &filter(None, None, None), "")
            .expect_err("this agent reads no Windows Event Log");
        let message = refused.to_string();
        assert!(
            message.contains("windows"),
            "the refusal names the source asked for: {message}"
        );
        assert!(
            message.contains("not available on this host"),
            "the refusal says why: {message}"
        );

        // An unrecognized name and the agent-files name are refused the same
        // way: `self` is the agent's own rotated files, never a host source.
        for name in ["syslog", "self", ""] {
            assert!(
                collect_system_logs(name, &filter(None, None, None), name).is_err(),
                "{name:?} is not a host log source this agent reads"
            );
        }

        assert_eq!(
            UNAVAILABLE_SOURCE_REQUESTS.load(Ordering::Relaxed),
            before + 4,
            "every refusal is counted"
        );
    }

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

    #[test]
    fn journald_priority_ceiling_mirrors_min_severity() {
        assert_eq!(journald_priority_ceiling("ERROR"), Some("3"));
        assert_eq!(journald_priority_ceiling("WARN"), Some("4"));
        assert_eq!(journald_priority_ceiling("INFO"), Some("6"));
        assert_eq!(journald_priority_ceiling("DEBUG"), None);
        assert_eq!(journald_priority_ceiling("TRACE"), None);
    }

    #[test]
    fn iso_to_epoch_parses_rfc3339() {
        assert_eq!(iso_to_epoch("1970-01-01T00:00:00Z"), Some(0));
        assert_eq!(iso_to_epoch("2023-11-14T22:13:20Z"), Some(1_700_000_000));
        assert_eq!(iso_to_epoch("not a time"), None);
    }

    #[test]
    fn build_journald_args_pushes_level_time_and_unit() {
        let args = build_journald_args(
            &filter(
                Some("WARN"),
                Some("1970-01-01T00:00:10Z"),
                Some("1970-01-01T00:00:20Z"),
            ),
            "nginx.service",
        );
        // Level ceiling, both time bounds, and the unit match are all present.
        assert!(args.windows(2).any(|w| w == ["-p", "4"]));
        assert!(args.windows(2).any(|w| w == ["--since", "@10"]));
        assert!(args.windows(2).any(|w| w == ["--until", "@20"]));
        assert!(args.contains(&"_SYSTEMD_UNIT=nginx.service".to_string()));
    }

    #[test]
    fn build_journald_args_keeps_hostile_unit_inert() {
        let args = build_journald_args(&filter(None, None, None), "; rm -rf /");
        assert!(args.contains(&"_SYSTEMD_UNIT=; rm -rf /".to_string()));
        // No shell metacharacter ever becomes its own argument.
        assert!(!args.iter().any(|a| a == "rm" || a == "-rf"));
    }

    #[test]
    fn a_failed_invocation_contributes_nothing() {
        let record = r#"{"PRIORITY":"3","_SYSTEMD_UNIT":"a.service","MESSAGE":"err one"}"#;
        let entries = entries_from_exit(true, record.as_bytes());
        assert_eq!(entries.len(), 1, "a successful read is parsed");
        assert_eq!(entries[0].level, "ERROR");
        assert!(
            entries_from_exit(false, record.as_bytes()).is_empty(),
            "a non-zero exit must yield no records, not the ones it managed to emit"
        );

        let units = units_from_exit(true, b"sshd.service\nnginx.service\n");
        assert_eq!(units, vec!["nginx.service", "sshd.service"]);
        assert!(
            units_from_exit(false, b"nginx.service\n").is_empty(),
            "a non-zero exit must yield no units, not a partial dropdown"
        );
    }

    #[test]
    fn realtime_micros_convert_to_iso() {
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
        let line = r#"{"SYSLOG_IDENTIFIER":"sshd","MESSAGE":"accepted login"}"#;
        let entry = parse_journald_json(line).expect("record without priority");
        assert_eq!(entry.level, "INFO", "absent PRIORITY defaults to info");
        assert_eq!(entry.target, "sshd");
        assert_eq!(entry.timestamp, "", "absent timestamp is empty");
    }

    #[test]
    fn parse_journald_json_rejects_non_records() {
        assert!(parse_journald_json("not json").is_none());
        assert!(parse_journald_json(r#"{"PRIORITY":"3"}"#).is_none());
    }

    #[test]
    fn read_journald_lines_skips_blank_and_malformed() {
        let doc = concat!(
            r#"{"PRIORITY":"3","_SYSTEMD_UNIT":"a.service","MESSAGE":"err one"}"#,
            "\n\n",
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
    fn collectors_degrade_to_empty_without_their_tool() {
        let source = LogSource::Journald;
        let _ = collect_host_logs(source, &filter(None, None, None), "");
        let _ = list_units(source);
        let _ = collect_host_logs(source, &filter(None, None, None), "$(evil)");
    }

    #[test]
    fn an_unlaunchable_tool_yields_the_same_nothing_as_a_failed_one() {
        let absent: Vec<String> = run_journalctl(
            ["--a-flag-journalctl-does-not-have".to_string()],
            units_from_exit,
        );
        assert!(
            absent.is_empty(),
            "a rejected invocation contributes no units"
        );

        let entries: Vec<LogEntry> = run_journalctl(
            ["--a-flag-journalctl-does-not-have".to_string()],
            entries_from_exit,
        );
        assert!(
            entries.is_empty(),
            "a rejected invocation contributes no records"
        );
    }

    #[test]
    fn normalize_unit_list_dedups_sorts_and_caps() {
        let raw = vec![
            " nginx.service ".to_string(),
            "sshd.service".to_string(),
            "nginx.service".to_string(),
            "".to_string(),
            "  ".to_string(),
        ];
        let units = normalize_unit_list(raw);
        assert_eq!(units, vec!["nginx.service", "sshd.service"]);

        // Cap holds: 300 distinct units truncate to MAX_UNITS, sorted.
        let many: Vec<String> = (0..300).map(|i| format!("unit-{i:04}.service")).collect();
        let capped = normalize_unit_list(many);
        assert_eq!(capped.len(), MAX_UNITS);
        assert_eq!(capped[0], "unit-0000.service");
    }

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
        assert_eq!(entries[0].level, "ERROR");
        assert_eq!(entries[0].target, "nginx.service");
        assert_eq!(entries[1].message, "handled request in 4ms");
    }

    #[test]
    fn only_a_batch_at_the_readers_cap_has_lost_records() {
        let batch = |n: usize| -> Vec<LogEntry> {
            (0..n)
                .map(|i| LogEntry {
                    timestamp: "2026-09-04T09:00:00.000000Z".to_string(),
                    level: "INFO".to_string(),
                    target: "cron.service".to_string(),
                    message: format!("record {i}"),
                })
                .collect()
        };

        assert!(
            batch_saturated(&batch(MAX_HOST_LINES)),
            "a batch at the cap lost the older end of its window"
        );
        assert!(
            !batch_saturated(&batch(MAX_HOST_LINES - 1)),
            "a batch under the cap is the whole window"
        );
        assert!(!batch_saturated(&batch(0)), "a quiet window lost nothing");
    }
}
