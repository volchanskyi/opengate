//! Intel AMT presence detection from the Management Engine Interface.
//!
//! Every case builds its own fixture tree under a temp dir, so the tests assert
//! the same behavior on a vPro workstation, a CI container, and a Mac.

use mesh_agent_core::amt_detect::{detect, detect_at, AmtPresence};
use std::fs;
use tempfile::TempDir;

/// Builds a fake MEI tree: an `mei0` device node and a `fw_ver` file holding
/// `contents` (skipped when `None`). Returns the temp dir plus both paths.
fn mei_fixture(contents: Option<&str>) -> (TempDir, std::path::PathBuf, std::path::PathBuf) {
    let dir = tempfile::tempdir().expect("temp dir");
    let device = dir.path().join("mei0");
    let version = dir.path().join("fw_ver");
    fs::write(&device, b"").expect("write device node");
    if let Some(text) = contents {
        fs::write(&version, text).expect("write fw_ver");
    }
    (dir, device, version)
}

#[test]
fn reports_available_with_version_when_mei_present() {
    let (_dir, device, version) = mei_fixture(Some("0:16.1.30.2260\n0:16.1.30.2260\n"));

    assert_eq!(
        detect_at(&device, &version),
        AmtPresence {
            available: true,
            version: "16.1.30.2260".to_string(),
        }
    );
}

#[test]
fn reports_available_without_version_when_version_file_missing() {
    let (_dir, device, version) = mei_fixture(None);

    assert_eq!(
        detect_at(&device, &version),
        AmtPresence {
            available: true,
            version: String::new(),
        }
    );
}

#[test]
fn reports_unavailable_when_mei_device_absent() {
    let dir = tempfile::tempdir().expect("temp dir");
    let device = dir.path().join("mei0");
    let version = dir.path().join("fw_ver");
    fs::write(&version, "0:16.1.30.2260\n").expect("write fw_ver");

    // No device node: the version file alone must not claim AMT support.
    assert_eq!(
        detect_at(&device, &version),
        AmtPresence {
            available: false,
            version: String::new(),
        }
    );
}

#[test]
fn strips_the_mei_client_index_prefix_from_the_version() {
    let (_dir, device, version) = mei_fixture(Some("2:11.8.50.3425\n"));

    assert_eq!(detect_at(&device, &version).version, "11.8.50.3425");
}

#[test]
fn keeps_a_version_line_that_carries_no_index_prefix() {
    let (_dir, device, version) = mei_fixture(Some("15.0.35.1951\n"));

    assert_eq!(detect_at(&device, &version).version, "15.0.35.1951");
}

#[test]
fn rejects_a_version_line_that_is_blank_or_whitespace() {
    let (_dir, device, version) = mei_fixture(Some("   \n\n"));

    assert_eq!(detect_at(&device, &version).version, "");
}

#[test]
fn caps_an_absurdly_long_version_line() {
    let (_dir, device, version) = mei_fixture(Some(&"9".repeat(4096)));

    let got = detect_at(&device, &version);
    assert!(got.available);
    assert!(
        got.version.len() <= 64,
        "version should be capped, got {} bytes",
        got.version.len()
    );
}

#[test]
fn detect_reads_the_host_and_never_panics() {
    // The host may or may not expose an MEI device; both answers are valid. The
    // contract under test is that the platform paths resolve and a version is
    // only ever reported alongside availability.
    let got = detect();
    if !got.available {
        assert_eq!(got.version, "");
    }
}
