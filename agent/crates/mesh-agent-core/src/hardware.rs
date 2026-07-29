//! Host hardware identity that travels with the agent's inventory report.
//!
//! The SMBIOS system UUID is the join key between a managed device and its Intel
//! AMT connection: on vPro hardware the UUID the AMT firmware presents over CIRA
//! is the same value the operating system reads out of DMI. The server stores it
//! purely to resolve that link and never returns it over the API.

use std::fs;
use std::path::Path;
use std::process::Command;
use uuid::Uuid;

/// Where Linux exposes the SMBIOS system UUID. Readable by root only, which is
/// how the agent runs on a managed host.
const LINUX_PRODUCT_UUID: &str = "/sys/class/dmi/id/product_uuid";

/// Reads this host's SMBIOS system UUID, or an empty string when the platform
/// does not expose one or the value is a placeholder.
pub fn system_uuid() -> String {
    if cfg!(target_os = "linux") {
        system_uuid_from(LINUX_PRODUCT_UUID)
    } else if cfg!(target_os = "windows") {
        parse_system_uuid(&windows_product_uuid())
    } else {
        String::new()
    }
}

/// Reads and validates a system UUID out of a DMI-style file.
pub fn system_uuid_from(path: impl AsRef<Path>) -> String {
    match fs::read_to_string(path) {
        Ok(raw) => parse_system_uuid(&raw),
        Err(_) => String::new(),
    }
}

/// Normalizes a raw DMI or WMI reading into a lowercase hyphenated UUID.
///
/// Returns an empty string for anything unusable — malformed text, or the
/// all-zero / all-ones sentinels hypervisors and unconfigured firmware hand out.
/// Those sentinels are shared by every affected host, so treating one as an
/// identity would link unrelated machines to the same AMT connection.
pub fn parse_system_uuid(raw: &str) -> String {
    raw.lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .find_map(|line| Uuid::parse_str(line).ok())
        .filter(|id| !id.is_nil() && *id != Uuid::max())
        .map(|id| id.hyphenated().to_string())
        .unwrap_or_default()
}

/// Asks Windows for the SMBIOS system UUID. Returns empty output on any
/// failure, which `parse_system_uuid` turns into "no identity".
fn windows_product_uuid() -> String {
    let output = Command::new("powershell")
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            "(Get-CimInstance -ClassName Win32_ComputerSystemProduct).UUID",
        ])
        .output();
    match output {
        Ok(out) => String::from_utf8_lossy(&out.stdout).to_string(),
        Err(_) => String::new(),
    }
}
