use std::process::Command;

fn main() {
    // The version is OPENGATE_VERSION when CI sets it, else the latest git tag, else the crate's.
    println!("cargo:rerun-if-env-changed=OPENGATE_VERSION");

    let version = if let Ok(ver) = std::env::var("OPENGATE_VERSION") {
        ver
    } else if let Some(ver) = git_version() {
        ver
    } else {
        env!("CARGO_PKG_VERSION").to_string()
    };

    println!("cargo:rustc-env=AGENT_VERSION={version}");
}

/// Reads the latest git tag without its leading `v`.
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

    Some(tag.strip_prefix('v').unwrap_or(tag).to_string())
}
