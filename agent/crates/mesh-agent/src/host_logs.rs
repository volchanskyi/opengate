//! Host system-log collection for the System Logs pane.
//!
//! Reads recent records from the platform host log source — the systemd journal
//! on Linux, the Windows Event Log on Windows — normalizes them to [`LogEntry`],
//! and enumerates the distinct emitting units for the UI unit dropdown. Level,
//! time, and unit are pushed down to the underlying tool to bound the read; the
//! caller still applies the shared severity/time/search filter for uniform
//! semantics across sources. Raw lines are secret-dense, so [`redact_entries`]
//! scrubs each message on the device (the first of two redaction layers) before
//! a response leaves it.

use crate::logs::LogFilter;
use mesh_agent_core::ml::redact::redact_log_line;
use mesh_protocol::LogEntry;
use std::io::{self, BufRead};

/// Hard cap on host log lines parsed per collection to bound memory/CPU.
const MAX_HOST_LINES: usize = 5_000;

/// Hard cap on distinct units returned for the dropdown (sorted, then capped).
/// An exact unit outside the capped set is still accepted by the unit filter.
const MAX_UNITS: usize = 200;

/// A platform host log source the agent can read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum LogSource {
    /// The systemd journal, read via `journalctl -o json`.
    Journald,
    /// The Windows Event Log, read via `Get-WinEvent`.
    WindowsEventLog,
}

/// The host log source for the current platform, or `None` where neither exists
/// (a minimal container or an unsupported OS). Resolving here keeps all
/// OS-specific logic on the agent — the browser only ever asks for `host`.
#[must_use]
pub fn resolve_host_source() -> Option<LogSource> {
    match std::env::consts::OS {
        "linux" => Some(LogSource::Journald),
        "windows" => Some(LogSource::WindowsEventLog),
        _ => None,
    }
}

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

/// The journald `-p` maximum priority for a normalized minimum severity, or
/// `None` when the filter matches every priority (no push-down needed). Because
/// journald `-p N` selects priorities `<= N` (more severe), the mapping mirrors
/// the shared severity ordering (min WARN ⊇ ERROR).
fn journald_priority_ceiling(min_level: &str) -> Option<&'static str> {
    match min_level {
        "ERROR" => Some("3"),
        "WARN" => Some("4"),
        "INFO" => Some("6"),
        _ => None, // DEBUG / TRACE / unknown: no ceiling
    }
}

/// The Windows Event Log levels to select for a normalized minimum severity, or
/// `None` when the filter matches every level. `0` (LogAlways) normalizes to
/// INFO, so it joins the INFO-and-above set.
fn windows_levels_for_min(min_level: &str) -> Option<Vec<i64>> {
    match min_level {
        "ERROR" => Some(vec![1, 2]),
        "WARN" => Some(vec![1, 2, 3]),
        "INFO" => Some(vec![0, 1, 2, 3, 4]),
        _ => None, // DEBUG / TRACE / unknown: every level
    }
}

/// Parses an RFC 3339 timestamp into whole Unix seconds, or `None` when it is
/// not a valid instant. Used to push a time bound down to the collector.
fn iso_to_epoch(ts: &str) -> Option<i64> {
    chrono::DateTime::parse_from_rfc3339(ts)
        .ok()
        .map(|dt| dt.timestamp())
}

/// Whether a unit token is safe to interpolate into the Windows PowerShell
/// `-Command` string. systemd units (`user@1000.service`) and Windows providers
/// (`Microsoft-Windows-Kernel-Power`, which contain spaces) fit this charset;
/// anything else (quotes, semicolons, `$`, backticks) is rejected so a hostile
/// unit can never break out of the `FilterHashtable` value.
fn windows_unit_allowed(unit: &str) -> bool {
    !unit.is_empty()
        && unit.chars().all(|c| {
            c.is_ascii_alphanumeric() || matches!(c, '.' | '_' | '@' | ':' | '/' | ' ' | '-')
        })
}

/// Builds the `journalctl -o json` argument vector for a bounded, filtered read.
/// Every value is a discrete argv token — no shell — so even a hostile `unit`
/// (e.g. `"; rm -rf /"`) is passed inertly as a single `_SYSTEMD_UNIT=` match
/// value and can never inject a command.
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

/// Builds the `Get-WinEvent -FilterHashtable` PowerShell command for a bounded,
/// filtered read, or `None` when `unit` is set but fails the allowlist (the
/// caller returns no entries rather than running an unsafe command). `LogName`
/// covers the common system channels.
fn build_windows_events_script(filter: &LogFilter, unit: &str) -> Option<String> {
    if !unit.is_empty() && !windows_unit_allowed(unit) {
        return None;
    }
    let mut terms = vec!["LogName='System','Application'".to_string()];
    if let Some(levels) = filter.level.as_deref().and_then(windows_levels_for_min) {
        let list = levels
            .iter()
            .map(i64::to_string)
            .collect::<Vec<_>>()
            .join(",");
        terms.push(format!("Level={list}"));
    }
    if let Some(from) = filter.time_from.as_deref().and_then(iso_to_epoch) {
        terms.push(format!(
            "StartTime=(Get-Date '1970-01-01Z').AddSeconds({from})"
        ));
    }
    if let Some(to) = filter.time_to.as_deref().and_then(iso_to_epoch) {
        terms.push(format!("EndTime=(Get-Date '1970-01-01Z').AddSeconds({to})"));
    }
    if !unit.is_empty() {
        terms.push(format!("ProviderName='{unit}'"));
    }
    let hashtable = terms.join("; ");
    Some(format!(
        "Get-WinEvent -FilterHashtable @{{{hashtable}}} -MaxEvents {MAX_HOST_LINES} -ErrorAction SilentlyContinue \
         | Select-Object TimeCreated,Level,ProviderName,Message | ConvertTo-Json -Compress"
    ))
}

/// Reads a JSON string field, returning `None` for a missing or non-string value.
fn json_str(value: &serde_json::Value, key: &str) -> Option<String> {
    value.get(key).and_then(|v| v.as_str()).map(str::to_owned)
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

/// Normalizes a raw list of unit/provider tokens into the dropdown set: distinct,
/// non-empty, sorted, and capped at [`MAX_UNITS`].
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

/// Reads the most recent host log records for `source`, applying the level/time
/// push-down and (when set) the unit filter. Returns an empty vector whenever the
/// source is unavailable — a non-matching platform, a missing tool, a read
/// failure, or a `unit` rejected by the Windows allowlist — so the same call is
/// safe on every fleet machine without platform branches at the call site.
pub fn collect_host_logs(source: LogSource, filter: &LogFilter, unit: &str) -> Vec<LogEntry> {
    match source {
        LogSource::Journald => collect_journald(filter, unit),
        LogSource::WindowsEventLog => collect_windows_events(filter, unit),
    }
}

/// Enumerates the distinct emitting units for `source` (systemd units / Windows
/// providers), normalized for the UI dropdown. Empty on any failure path.
pub fn list_units(source: LogSource) -> Vec<String> {
    match source {
        LogSource::Journald => list_journald_units(),
        LogSource::WindowsEventLog => list_windows_providers(),
    }
}

/// Runs `journalctl` for the most recent records under the pushed-down filter.
/// Empty on any failure path (non-Linux host, missing binary, non-zero exit).
fn collect_journald(filter: &LogFilter, unit: &str) -> Vec<LogEntry> {
    let output = std::process::Command::new("journalctl")
        .args(build_journald_args(filter, unit))
        .output();
    match output {
        Ok(output) if output.status.success() => {
            read_journald_lines(io::Cursor::new(output.stdout)).unwrap_or_default()
        }
        _ => Vec::new(),
    }
}

/// Runs `Get-WinEvent` for the most recent records under the pushed-down filter.
/// Empty on any failure path (non-Windows host, missing PowerShell, non-zero
/// exit, or a `unit` rejected by the allowlist).
fn collect_windows_events(filter: &LogFilter, unit: &str) -> Vec<LogEntry> {
    let Some(script) = build_windows_events_script(filter, unit) else {
        return Vec::new();
    };
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

/// Enumerates distinct systemd units via `journalctl -F _SYSTEMD_UNIT` (an
/// indexed field enumeration — cheap). Empty on any failure path.
fn list_journald_units() -> Vec<String> {
    let output = std::process::Command::new("journalctl")
        .args(["-F", "_SYSTEMD_UNIT", "--no-pager"])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            let text = String::from_utf8_lossy(&output.stdout);
            normalize_unit_list(text.lines().map(str::to_owned))
        }
        _ => Vec::new(),
    }
}

/// Enumerates Windows Event Log providers via `Get-WinEvent -ListProvider *`.
/// Empty on any failure path.
fn list_windows_providers() -> Vec<String> {
    let script = "Get-WinEvent -ListProvider * -ErrorAction SilentlyContinue \
                  | Select-Object -ExpandProperty Name";
    let output = std::process::Command::new("powershell")
        .args(["-NoProfile", "-NonInteractive", "-Command", script])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            let text = String::from_utf8_lossy(&output.stdout);
            normalize_unit_list(text.lines().map(str::to_owned))
        }
        _ => Vec::new(),
    }
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
    fn windows_levels_map_to_labels() {
        assert_eq!(windows_level_to_label(1), "ERROR"); // Critical
        assert_eq!(windows_level_to_label(2), "ERROR"); // Error
        assert_eq!(windows_level_to_label(3), "WARN"); // Warning
        assert_eq!(windows_level_to_label(4), "INFO"); // Information
        assert_eq!(windows_level_to_label(5), "DEBUG"); // Verbose
        assert_eq!(windows_level_to_label(0), "INFO"); // LogAlways
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
    fn windows_levels_for_min_widen_with_severity() {
        assert_eq!(windows_levels_for_min("ERROR"), Some(vec![1, 2]));
        assert_eq!(windows_levels_for_min("WARN"), Some(vec![1, 2, 3]));
        assert_eq!(windows_levels_for_min("INFO"), Some(vec![0, 1, 2, 3, 4]));
        assert_eq!(windows_levels_for_min("DEBUG"), None);
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

    /// A hostile unit is a single inert argv token on the journald path — no
    /// shell, so nothing executes; it simply matches no unit.
    #[test]
    fn build_journald_args_keeps_hostile_unit_inert() {
        let args = build_journald_args(&filter(None, None, None), "; rm -rf /");
        assert!(args.contains(&"_SYSTEMD_UNIT=; rm -rf /".to_string()));
        // No shell metacharacter ever becomes its own argument.
        assert!(!args.iter().any(|a| a == "rm" || a == "-rf"));
    }

    #[test]
    fn windows_unit_allowlist_permits_real_units_rejects_injection() {
        assert!(windows_unit_allowed("nginx.service"));
        assert!(windows_unit_allowed("user@1000.service"));
        assert!(windows_unit_allowed("Microsoft-Windows-Kernel-Power")); // has hyphens
        assert!(windows_unit_allowed("Some Provider With Spaces"));
        assert!(!windows_unit_allowed("")); // empty is "no filter", not a value
        assert!(!windows_unit_allowed("'; Remove-Item C:\\ -Recurse #"));
        assert!(!windows_unit_allowed("$(bad)"));
        assert!(!windows_unit_allowed("a`b"));
    }

    /// A hostile unit yields no Windows command at all — the collector returns
    /// empty rather than interpolating it into the `-Command` string.
    #[test]
    fn build_windows_script_refuses_hostile_unit() {
        assert!(
            build_windows_events_script(&filter(None, None, None), "'; Remove-Item C:\\ #")
                .is_none()
        );
        // A benign provider is embedded as a single quoted FilterHashtable value.
        let script = build_windows_events_script(
            &filter(None, None, None),
            "Microsoft-Windows-Kernel-Power",
        )
        .expect("benign provider builds a script");
        assert!(script.contains("ProviderName='Microsoft-Windows-Kernel-Power'"));
        assert!(!script.contains("Remove-Item"));
    }

    #[test]
    fn build_windows_script_pushes_level_and_time() {
        let script = build_windows_events_script(
            &filter(Some("WARN"), Some("1970-01-01T00:00:10Z"), None),
            "",
        )
        .unwrap();
        assert!(script.contains("Level=1,2,3"));
        assert!(script.contains("AddSeconds(10)"));
        assert!(
            !script.contains("ProviderName="),
            "no unit → no provider filter term"
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
        assert!(parse_windows_events(r#"{"Level":2,"ProviderName":"X"}"#).is_empty());
    }

    /// Every source degrades to an empty result where its tool is absent (the
    /// off-platform / minimal-host path), so a call is safe on any fleet machine
    /// without a platform branch at the call site.
    #[test]
    fn collectors_degrade_to_empty_without_their_tool() {
        for source in [LogSource::Journald, LogSource::WindowsEventLog] {
            // On a host lacking the tool (CI has no Windows Event Log / may lack
            // journald) the collector and enumerator return empty, never panic.
            let _ = collect_host_logs(source, &filter(None, None, None), "");
            let _ = list_units(source);
        }
        // Windows collection is refused outright for a hostile unit — no command.
        assert!(collect_host_logs(
            LogSource::WindowsEventLog,
            &filter(None, None, None),
            "$(evil)"
        )
        .is_empty());
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
}
