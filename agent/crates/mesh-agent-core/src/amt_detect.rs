//! Intel AMT presence detection from the Management Engine Interface (MEI) device node and sysfs.

use std::fs;
use std::path::{Path, PathBuf};

/// Longest ME/AMT version string kept; a longer value is a malformed sysfs read.
const MAX_VERSION_LEN: usize = 64;

/// Local Intel AMT/ME presence as read from the host's MEI interface.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct AmtPresence {
    /// True when the host exposes a Management Engine interface, which says nothing about provisioning.
    pub available: bool,
    /// ME/AMT firmware version, empty when the host exposes no version file.
    pub version: String,
}

/// Reads AMT presence from this platform's MEI paths.
pub fn detect() -> AmtPresence {
    detect_at(mei_device_path(), mei_version_path())
}

/// Reads AMT presence from an explicit device node; `version` is read only when the device exists.
pub fn detect_at(device: impl AsRef<Path>, version: impl AsRef<Path>) -> AmtPresence {
    if !device.as_ref().exists() {
        return AmtPresence::default();
    }
    AmtPresence {
        available: true,
        version: read_version(version.as_ref()),
    }
}

/// Extracts the firmware version from the first `fw_ver` line, dropping a `0:` client prefix.
fn read_version(path: &Path) -> String {
    let Ok(raw) = fs::read_to_string(path) else {
        return String::new();
    };
    let line = raw.lines().next().unwrap_or_default().trim();
    let value = line.rsplit(':').next().unwrap_or_default().trim();
    if value.is_empty() || value.len() > MAX_VERSION_LEN {
        return String::new();
    }
    value.to_string()
}

fn mei_device_path() -> PathBuf {
    if cfg!(target_os = "linux") {
        PathBuf::from("/dev/mei0")
    } else {
        PathBuf::new()
    }
}

fn mei_version_path() -> PathBuf {
    if cfg!(target_os = "linux") {
        PathBuf::from("/sys/class/mei/mei0/fw_ver")
    } else {
        PathBuf::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mei_paths_match_the_build_target() {
        let device = mei_device_path();
        let version = mei_version_path();
        if cfg!(target_os = "linux") {
            assert_eq!(device, PathBuf::from("/dev/mei0"));
            assert_eq!(version, PathBuf::from("/sys/class/mei/mei0/fw_ver"));
        } else {
            assert_eq!(device, PathBuf::new());
            assert_eq!(version, PathBuf::new());
        }
    }

    #[test]
    fn an_empty_device_path_reports_no_management_engine() {
        assert_eq!(
            detect_at(PathBuf::new(), PathBuf::new()),
            AmtPresence::default()
        );
    }

    #[test]
    fn the_version_cap_keeps_the_longest_readable_line() {
        let dir = tempfile::tempdir().expect("temp dir");
        let at_cap = dir.path().join("at_cap");
        let over_cap = dir.path().join("over_cap");
        fs::write(&at_cap, "9".repeat(MAX_VERSION_LEN)).expect("write at-cap fixture");
        fs::write(&over_cap, "9".repeat(MAX_VERSION_LEN + 1)).expect("write over-cap fixture");

        assert_eq!(read_version(&at_cap).len(), MAX_VERSION_LEN);
        assert_eq!(read_version(&over_cap), "");
    }
}
