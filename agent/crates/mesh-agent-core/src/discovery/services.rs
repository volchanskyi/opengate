//! Host-service discovery (WS-16).
//!
//! On Linux the collector lists systemd service units via
//! `systemctl list-units --type=service --all --no-legend --no-pager --plain`
//! and reports each unit's name and normalized run state. On Windows it parses
//! `Get-Service | ConvertTo-Json`. Both are read-only enumerations bounded by
//! [`super::MAX_SERVICES`]; no service is started, stopped, or altered.

use mesh_protocol::DiscoveredService;

/// Parses `systemctl list-units --type=service --all --no-legend --plain`
/// output. Columns are `UNIT LOAD ACTIVE SUB DESCRIPTION`; the SUB column is the
/// most specific run state (`running`, `exited`, `dead`, `failed`, …). Rows
/// whose unit is not a `.service` are ignored.
pub(crate) fn parse_systemctl(content: &str) -> Vec<DiscoveredService> {
    let mut out = Vec::new();
    for line in content.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 4 {
            continue;
        }
        let name = fields[0];
        if !name.ends_with(".service") {
            continue;
        }
        out.push(DiscoveredService {
            name: name.to_string(),
            state: fields[3].to_ascii_lowercase(),
        });
    }
    out
}

/// Maps a Windows `ServiceControllerStatus` value to a normalized state label.
/// `Get-Service | ConvertTo-Json` serializes the status as its integer value.
#[cfg(any(target_os = "windows", test))]
fn windows_status_to_state(status: i64) -> &'static str {
    match status {
        1 => "stopped",
        2 => "start_pending",
        3 => "stop_pending",
        4 => "running",
        5 => "continue_pending",
        6 => "pause_pending",
        7 => "paused",
        _ => "unknown",
    }
}

/// Parses `Get-Service | Select Name,Status | ConvertTo-Json` output. The
/// `Status` field is accepted either as its integer enum value or as a string
/// label. A top-level array (many services) or a bare object (exactly one) are
/// both handled.
#[cfg(any(target_os = "windows", test))]
pub(crate) fn parse_windows_services(json: &str) -> Vec<DiscoveredService> {
    let value: serde_json::Value = match serde_json::from_str(json) {
        Ok(value) => value,
        Err(_) => return Vec::new(),
    };
    let mut out = Vec::new();
    match value {
        serde_json::Value::Array(records) => {
            for record in &records {
                if let Some(service) = parse_windows_service(record) {
                    out.push(service);
                }
            }
        }
        other => {
            if let Some(service) = parse_windows_service(&other) {
                out.push(service);
            }
        }
    }
    out
}

/// Parses one Windows service record. A record without a `Name` yields `None`.
#[cfg(any(target_os = "windows", test))]
fn parse_windows_service(value: &serde_json::Value) -> Option<DiscoveredService> {
    let name = value.get("Name").and_then(|v| v.as_str())?.to_string();
    let state = match value.get("Status") {
        Some(serde_json::Value::Number(n)) => n
            .as_i64()
            .map(windows_status_to_state)
            .unwrap_or("unknown")
            .to_string(),
        Some(serde_json::Value::String(s)) => s.to_ascii_lowercase(),
        _ => "unknown".to_string(),
    };
    Some(DiscoveredService { name, state })
}

/// Reads the host's services, bounded and normalized. Empty on any platform
/// where the source is absent.
pub fn collect_services() -> Vec<DiscoveredService> {
    #[cfg(target_os = "linux")]
    {
        collect_services_linux()
    }
    #[cfg(target_os = "windows")]
    {
        collect_services_windows()
    }
    #[cfg(not(any(target_os = "linux", target_os = "windows")))]
    {
        Vec::new()
    }
}

/// Runs `systemctl list-units` for all service units. Empty on any failure path
/// (missing binary, non-zero exit).
#[cfg(target_os = "linux")]
fn collect_services_linux() -> Vec<DiscoveredService> {
    let output = std::process::Command::new("systemctl")
        .args([
            "list-units",
            "--type=service",
            "--all",
            "--no-legend",
            "--no-pager",
            "--plain",
        ])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            parse_systemctl(&String::from_utf8_lossy(&output.stdout))
        }
        _ => Vec::new(),
    }
}

/// Runs `Get-Service` for all services. Empty on any failure path (missing
/// PowerShell, non-zero exit).
#[cfg(target_os = "windows")]
fn collect_services_windows() -> Vec<DiscoveredService> {
    let script = "Get-Service | Select-Object Name,Status | ConvertTo-Json -Compress";
    let output = std::process::Command::new("powershell")
        .args(["-NoProfile", "-NonInteractive", "-Command", script])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            parse_windows_services(&String::from_utf8_lossy(&output.stdout))
        }
        _ => Vec::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// systemctl rows parse to unit name + SUB run state; non-service units are
    /// dropped and short rows are skipped.
    #[test]
    fn parse_systemctl_extracts_name_and_state() {
        let out = concat!(
            "nginx.service           loaded active running   A high performance web server\n",
            "ssh.service             loaded active running   OpenBSD Secure Shell server\n",
            "cron.service            loaded active exited     Regular background program\n",
            "graphical.target        loaded active active     Graphical Interface\n",
            "short row\n",
        );
        let services = parse_systemctl(out);
        assert_eq!(services.len(), 3, "only .service units, no .target");
        assert_eq!(services[0].name, "nginx.service");
        assert_eq!(services[0].state, "running");
        assert_eq!(services[2].name, "cron.service");
        assert_eq!(services[2].state, "exited");
    }

    /// Windows status codes map to normalized labels.
    #[test]
    fn windows_status_codes_map_to_labels() {
        assert_eq!(windows_status_to_state(4), "running");
        assert_eq!(windows_status_to_state(1), "stopped");
        assert_eq!(windows_status_to_state(7), "paused");
        assert_eq!(windows_status_to_state(99), "unknown");
    }

    /// A `Get-Service` JSON array parses each record; the status is accepted as
    /// an integer enum value.
    #[test]
    fn parse_windows_services_array_integer_status() {
        let json = r#"[
            {"Name":"Spooler","Status":4},
            {"Name":"wuauserv","Status":1}
        ]"#;
        let services = parse_windows_services(json);
        assert_eq!(services.len(), 2);
        assert_eq!(services[0].name, "Spooler");
        assert_eq!(services[0].state, "running");
        assert_eq!(services[1].state, "stopped");
    }

    /// A bare object (single service) and a string status are both handled.
    #[test]
    fn parse_windows_services_object_string_status() {
        let json = r#"{"Name":"Dhcp","Status":"Running"}"#;
        let services = parse_windows_services(json);
        assert_eq!(services.len(), 1);
        assert_eq!(services[0].name, "Dhcp");
        assert_eq!(services[0].state, "running");
    }

    /// Bad JSON and records without a name yield nothing.
    #[test]
    fn parse_windows_services_rejects_bad_input() {
        assert!(parse_windows_services("}{").is_empty());
        assert!(parse_windows_services(r#"{"Status":4}"#).is_empty());
    }
}
