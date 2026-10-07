//! Container discovery through the read-only `docker ps` and `podman ps` CLIs.

use mesh_protocol::DiscoveredContainer;

/// Collapses decorated statuses like `Up 3 hours` to a lowercase state label.
fn normalize_state(raw: &str) -> String {
    let lower = raw.trim().to_ascii_lowercase();
    if lower.starts_with("up") {
        "running".to_string()
    } else if lower.starts_with("exited") {
        "exited".to_string()
    } else if lower.starts_with("created") {
        "created".to_string()
    } else {
        lower.split_whitespace().next().unwrap_or("").to_string()
    }
}

/// Prefers the explicit `State` field and falls back to the decorated `Status` string.
fn state_of(value: &serde_json::Value) -> String {
    if let Some(state) = value.get("State").and_then(|v| v.as_str()) {
        return normalize_state(state);
    }
    let status = value.get("Status").and_then(|v| v.as_str()).unwrap_or("");
    normalize_state(status)
}

/// Accepts docker's scalar `Names` or the first entry of podman's `Names` array.
fn name_of(value: &serde_json::Value) -> String {
    match value.get("Names") {
        Some(serde_json::Value::String(s)) => s.trim_start_matches('/').to_string(),
        Some(serde_json::Value::Array(arr)) => arr
            .first()
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        _ => value
            .get("Name")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
    }
}

/// Builds a [`DiscoveredContainer`] from one record; a record without an image yields `None`.
fn parse_record(runtime: &str, value: &serde_json::Value) -> Option<DiscoveredContainer> {
    let image = value.get("Image").and_then(|v| v.as_str())?.to_string();
    Some(DiscoveredContainer {
        runtime: runtime.to_string(),
        image,
        name: name_of(value),
        state: state_of(value),
    })
}

/// Parses `docker ps` output of one JSON object per line, skipping blank and malformed lines.
pub(crate) fn parse_docker_ps(stdout: &str) -> Vec<DiscoveredContainer> {
    let mut out = Vec::new();
    for line in stdout.lines() {
        if line.trim().is_empty() {
            continue;
        }
        if let Ok(value) = serde_json::from_str::<serde_json::Value>(line) {
            if let Some(container) = parse_record("docker", &value) {
                out.push(container);
            }
        }
    }
    out
}

/// Parses `podman ps` output of one JSON array; malformed input yields nothing.
pub(crate) fn parse_podman_ps(stdout: &str) -> Vec<DiscoveredContainer> {
    let Ok(serde_json::Value::Array(records)) = serde_json::from_str::<serde_json::Value>(stdout)
    else {
        return Vec::new();
    };
    records
        .iter()
        .filter_map(|record| parse_record("podman", record))
        .collect()
}

/// Lists containers from whichever of docker and podman is present.
pub fn collect_containers() -> Vec<DiscoveredContainer> {
    let mut out = run_ps("docker", parse_docker_ps);
    out.extend(run_ps("podman", parse_podman_ps));
    out
}

/// Runs `<runtime> ps -a` and parses stdout; a missing binary or non-zero exit yields none.
fn run_ps(runtime: &str, parser: fn(&str) -> Vec<DiscoveredContainer>) -> Vec<DiscoveredContainer> {
    let format = if runtime == "docker" {
        "{{json .}}"
    } else {
        "json"
    };
    let output = std::process::Command::new(runtime)
        .args(["ps", "-a", "--format", format])
        .output();
    match output {
        Ok(output) if output.status.success() => parser(&String::from_utf8_lossy(&output.stdout)),
        _ => Vec::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_docker_ps_reads_json_lines() {
        let out = concat!(
            r#"{"Image":"redis:7","Names":"cache","State":"running","Status":"Up 2 hours"}"#,
            "\n",
            "\n",
            r#"{"Image":"nginx:1.25","Names":"web","Status":"Exited (0) 5 minutes ago"}"#,
            "\n",
        );
        let containers = parse_docker_ps(out);
        assert_eq!(containers.len(), 2);
        assert_eq!(containers[0].runtime, "docker");
        assert_eq!(containers[0].image, "redis:7");
        assert_eq!(containers[0].name, "cache");
        assert_eq!(containers[0].state, "running");
        assert_eq!(containers[1].state, "exited");
        assert_eq!(containers[1].name, "web");
    }

    #[test]
    fn parse_docker_ps_skips_records_without_image() {
        let out = concat!(
            "not json\n",
            r#"{"Names":"orphan","State":"running"}"#,
            "\n",
        );
        assert!(parse_docker_ps(out).is_empty());
    }

    #[test]
    fn parse_podman_ps_reads_json_array() {
        let out = r#"[
            {"Image":"docker.io/library/postgres:16","Names":["db"],"State":"running"},
            {"Image":"alpine:3","Names":["job"],"State":"exited"}
        ]"#;
        let containers = parse_podman_ps(out);
        assert_eq!(containers.len(), 2);
        assert_eq!(containers[0].runtime, "podman");
        assert_eq!(containers[0].image, "docker.io/library/postgres:16");
        assert_eq!(containers[0].name, "db");
        assert_eq!(containers[0].state, "running");
        assert_eq!(containers[1].state, "exited");
    }

    #[test]
    fn parse_podman_ps_rejects_bad_json() {
        assert!(parse_podman_ps("not an array").is_empty());
        assert!(parse_podman_ps("}{").is_empty());
    }

    #[test]
    fn normalize_state_collapses_decorated_status() {
        assert_eq!(normalize_state("Up 3 hours"), "running");
        assert_eq!(normalize_state("Exited (137) 1 hour ago"), "exited");
        assert_eq!(normalize_state("Created"), "created");
        assert_eq!(normalize_state("paused"), "paused");
    }
}
