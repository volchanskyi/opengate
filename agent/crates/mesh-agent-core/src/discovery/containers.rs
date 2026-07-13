//! Container discovery (WS-16).
//!
//! Enumerates running/stopped containers through a read-only local runtime CLI
//! when present: `docker ps -a --format '{{json .}}'` (newline-delimited JSON
//! objects) or `podman ps -a --format json` (a JSON array). Nothing is created,
//! started, or stopped. Absent runtimes contribute nothing, so the same call is
//! safe on a host with neither.

use mesh_protocol::DiscoveredContainer;

/// Normalizes a runtime's status/state text to a lowercase state label,
/// collapsing decorated statuses like `Up 3 hours` or `Exited (0) 2m ago`.
fn normalize_state(raw: &str) -> String {
    let lower = raw.trim().to_ascii_lowercase();
    if lower.starts_with("up") {
        "running".to_string()
    } else if lower.starts_with("exited") {
        "exited".to_string()
    } else if lower.starts_with("created") {
        "created".to_string()
    } else {
        // Docker/podman `State` fields (running/exited/paused/…) pass through as
        // their first token.
        lower.split_whitespace().next().unwrap_or("").to_string()
    }
}

/// Reads a container's state, preferring the explicit `State` field and falling
/// back to the decorated `Status` string.
fn state_of(value: &serde_json::Value) -> String {
    if let Some(state) = value.get("State").and_then(|v| v.as_str()) {
        return normalize_state(state);
    }
    let status = value.get("Status").and_then(|v| v.as_str()).unwrap_or("");
    normalize_state(status)
}

/// Reads the container name, accepting docker's scalar `Names` or podman's
/// `Names` array (first entry).
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

/// Builds a [`DiscoveredContainer`] from one runtime record. A record without an
/// image reference is not a container listing and yields `None`.
fn parse_record(runtime: &str, value: &serde_json::Value) -> Option<DiscoveredContainer> {
    let image = value.get("Image").and_then(|v| v.as_str())?.to_string();
    Some(DiscoveredContainer {
        runtime: runtime.to_string(),
        image,
        name: name_of(value),
        state: state_of(value),
    })
}

/// Parses `docker ps --format '{{json .}}'` output: one JSON object per line.
/// Blank and malformed lines are skipped.
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

/// Parses `podman ps --format json` output: a single JSON array of container
/// objects. Empty on malformed input.
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

/// Discovers containers via docker and podman if either CLI is present. Empty
/// when no runtime is available.
pub fn collect_containers() -> Vec<DiscoveredContainer> {
    let mut out = run_ps("docker", parse_docker_ps);
    out.extend(run_ps("podman", parse_podman_ps));
    out
}

/// Runs `<runtime> ps -a --format <fmt>` and parses stdout with `parser`. Empty
/// on any failure path (missing binary, non-zero exit).
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

    /// Docker's newline-delimited JSON parses each container with its image,
    /// name (leading slash stripped), and normalized state.
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
        // No State field → derived from the decorated Status string.
        assert_eq!(containers[1].state, "exited");
        assert_eq!(containers[1].name, "web");
    }

    /// A docker line without an image is not a container listing.
    #[test]
    fn parse_docker_ps_skips_records_without_image() {
        let out = concat!(
            "not json\n",
            r#"{"Names":"orphan","State":"running"}"#,
            "\n",
        );
        assert!(parse_docker_ps(out).is_empty());
    }

    /// Podman's JSON array parses each record; `Names` is an array there.
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

    /// Malformed podman output yields nothing rather than erroring.
    #[test]
    fn parse_podman_ps_rejects_bad_json() {
        assert!(parse_podman_ps("not an array").is_empty());
        assert!(parse_podman_ps("}{").is_empty());
    }

    /// Decorated status strings collapse to bare state labels.
    #[test]
    fn normalize_state_collapses_decorated_status() {
        assert_eq!(normalize_state("Up 3 hours"), "running");
        assert_eq!(normalize_state("Exited (137) 1 hour ago"), "exited");
        assert_eq!(normalize_state("Created"), "created");
        assert_eq!(normalize_state("paused"), "paused");
    }
}
