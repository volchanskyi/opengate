//! Host hardware identity that travels with the agent's inventory report.
//!
//! The SMBIOS system UUID is the join key between a managed device and its Intel
//! AMT connection: on vPro hardware the UUID the AMT firmware presents over CIRA
//! is the same value the operating system reads out of DMI. The server stores it
//! purely to resolve that link and never returns it over the API.

use std::fs;
use std::path::Path;
use uuid::Uuid;

/// Where Linux exposes the SMBIOS system UUID. Readable by root only, which is
/// how the agent runs on a managed host.
const LINUX_PRODUCT_UUID: &str = "/sys/class/dmi/id/product_uuid";

/// Reads this host's SMBIOS system UUID, or an empty string when the platform
/// does not expose one or the value is a placeholder.
pub fn system_uuid() -> String {
    if cfg!(target_os = "linux") {
        system_uuid_from(LINUX_PRODUCT_UUID)
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

/// Normalizes a raw DMI reading into a lowercase hyphenated UUID.
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

#[cfg(test)]
mod tests {
    use super::*;

    /// On Linux the identity comes from the DMI sysfs file and from nowhere
    /// else, so [`system_uuid`] and [`system_uuid_from`] over that path are the
    /// same reading. Anywhere else the platform exposes no SMBIOS UUID and the
    /// answer is empty — an unrelated host must never be handed an identity
    /// that would link it to another machine's AMT connection.
    #[test]
    fn system_uuid_reads_the_platform_source() {
        if cfg!(target_os = "linux") {
            assert_eq!(LINUX_PRODUCT_UUID, "/sys/class/dmi/id/product_uuid");
            assert_eq!(system_uuid(), system_uuid_from(LINUX_PRODUCT_UUID));
        } else {
            assert_eq!(system_uuid(), "");
        }
    }
}
