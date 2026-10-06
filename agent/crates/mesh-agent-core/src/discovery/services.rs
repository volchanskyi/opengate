//! Read-only enumeration of systemd service units with their run states.

use mesh_protocol::DiscoveredService;

/// Parses `systemctl list-units` rows (`UNIT LOAD ACTIVE SUB DESCRIPTION`) into name and SUB state.
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

/// Reads the host's services, bounded and normalized. Empty on any platform
/// where the source is absent.
pub fn collect_services() -> Vec<DiscoveredService> {
    #[cfg(target_os = "linux")]
    {
        collect_services_linux()
    }
    #[cfg(not(target_os = "linux"))]
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

#[cfg(test)]
mod tests {
    use super::*;

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

    #[test]
    fn parse_systemctl_keeps_the_shortest_complete_row() {
        let out = concat!(
            "dbus.service loaded active running\n",
            "cups.service loaded active\n",
        );
        let services = parse_systemctl(out);
        assert_eq!(services.len(), 1, "the three-field row has no SUB column");
        assert_eq!(services[0].name, "dbus.service");
        assert_eq!(services[0].state, "running");
    }

    #[test]
    fn collect_services_reads_the_platform_or_reports_nothing() {
        let services = collect_services();
        #[cfg(not(target_os = "linux"))]
        assert!(services.is_empty(), "no service manager to read");
        for service in &services {
            assert!(
                service.name.ends_with(".service"),
                "not a service unit: {}",
                service.name
            );
            assert!(
                !service.state.is_empty(),
                "unit {} carries no run state",
                service.name
            );
        }
    }
}
