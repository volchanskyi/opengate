//! Intel AMT presence detection over the Management Engine Interface (MEI).
//!
//! The agent reports *presence* — whether the host exposes a Management Engine
//! at all, and which ME/AMT firmware it runs. That is a property of the machine
//! the agent already manages, so it travels with the hardware inventory. The
//! richer per-machine detail (model, AMT firmware build) arrives separately over
//! the server's CIRA/WSMAN connection.
//!
//! Detection is deliberately file-based: the MEI device node on Linux, and the
//! equivalent file a future platform exposes. That keeps one code path, adds no
//! platform crates, and lets the whole thing be exercised against a fixture
//! tree.

use std::fs;
use std::path::{Path, PathBuf};

/// Longest ME/AMT version string kept. Real values are ~15 bytes
/// (`16.1.30.2260`); anything longer is a malformed sysfs read.
const MAX_VERSION_LEN: usize = 64;

/// Local Intel AMT/ME presence as read from the host's MEI interface.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct AmtPresence {
    /// True when the host exposes a Management Engine interface. This means the
    /// hardware supports AMT; it does not mean AMT is provisioned. A linked AMT
    /// connection record is the proof of actual activation.
    pub available: bool,
    /// ME/AMT firmware version, empty when the host exposes no version file.
    pub version: String,
}

/// Reads AMT presence from this platform's MEI paths.
pub fn detect() -> AmtPresence {
    detect_at(mei_device_path(), mei_version_path())
}

/// Reads AMT presence from an explicit device node and version file.
///
/// `device` decides availability; `version` is best-effort and only consulted
/// when the device node exists, so a stale version file can never claim AMT
/// support on a machine that has no Management Engine.
pub fn detect_at(device: impl AsRef<Path>, version: impl AsRef<Path>) -> AmtPresence {
    if !device.as_ref().exists() {
        return AmtPresence::default();
    }
    AmtPresence {
        available: true,
        version: read_version(version.as_ref()),
    }
}

/// Extracts the firmware version from an MEI `fw_ver` file.
///
/// The file holds one line per ME client, each optionally prefixed with the
/// client index (`0:16.1.30.2260`). Every line reports the same firmware, so the
/// first usable one wins.
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

/// The MEI device node whose presence proves a Management Engine exists.
fn mei_device_path() -> PathBuf {
    if cfg!(target_os = "linux") {
        PathBuf::from("/dev/mei0")
    } else {
        // No Management Engine interface to speak of on other platforms.
        PathBuf::new()
    }
}

/// The file carrying the ME/AMT firmware version, where the platform exposes one.
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

    /// The MEI paths are the ones this platform actually exposes: on Linux the
    /// `mei0` character device the driver creates, and its sysfs `fw_ver`
    /// sibling. Anywhere else there is no Management Engine interface to read,
    /// and both paths are empty so [`detect_at`] reports "no AMT" instead of
    /// probing an arbitrary file that happens to exist.
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

    /// An empty path never exists, so a platform with no MEI interface reports
    /// "no AMT" rather than claiming presence off a stray file.
    #[test]
    fn an_empty_device_path_reports_no_management_engine() {
        assert_eq!(
            detect_at(PathBuf::new(), PathBuf::new()),
            AmtPresence::default()
        );
    }

    /// The version cap is a boundary, not an approximation. A line at exactly
    /// the cap is still a readable version and must be kept; one byte more is a
    /// malformed sysfs read and must be dropped. Rejecting at the cap would
    /// blank the firmware version on a machine reporting an unusually long but
    /// valid one — and a blank version reads as "no version file", which is a
    /// different fact about the hardware.
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
