//! The SMBIOS system UUID joins a managed device to its Intel AMT connection.

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

/// Normalizes a DMI reading to a lowercase hyphenated UUID; malformed and shared sentinels give "".
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
