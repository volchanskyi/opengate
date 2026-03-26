use std::process::Command;

fn main() {
    // Allow CI to override the agent version via OPENGATE_VERSION env var.
    // Falls back to git describe, then CARGO_PKG_VERSION as last resort.
    println!("cargo:rerun-if-env-changed=OPENGATE_VERSION");

    let version = if let Ok(ver) = std::env::var("OPENGATE_VERSION") {
        ver
    } else if let Some(ver) = git_version() {
        ver
    } else {
        std::env::var("CARGO_PKG_VERSION").unwrap()
    };

    println!("cargo:rustc-env=AGENT_VERSION={version}");
}

/// Try to get the version from the latest git tag (e.g. "v0.15.4" → "0.15.4").
fn git_version() -> Option<String> {
    let output = Command::new("git")
        .args(["describe", "--tags", "--abbrev=0"])
        .output()
        .ok()?;

    if !output.status.success() {
        return None;
    }

    let tag = String::from_utf8(output.stdout).ok()?;
    let tag = tag.trim();

    // Strip leading "v" prefix if present.
    Some(tag.strip_prefix('v').unwrap_or(tag).to_string())
}
