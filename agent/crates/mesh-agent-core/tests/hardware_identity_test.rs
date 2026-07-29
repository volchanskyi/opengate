//! SMBIOS system-UUID collection — the join key that ties a managed device to
//! its Intel AMT CIRA connection.

use mesh_agent_core::hardware::{parse_system_uuid, system_uuid, system_uuid_from};
use std::fs;

#[test]
fn normalizes_a_valid_uuid_to_lowercase() {
    assert_eq!(
        parse_system_uuid("4C4C4544-0037-5A10-8054-B4C04F335432\n"),
        "4c4c4544-0037-5a10-8054-b4c04f335432"
    );
}

#[test]
fn accepts_an_already_lowercase_uuid() {
    assert_eq!(
        parse_system_uuid("  8f3e1b2c-5d4a-4e6f-9a0b-1c2d3e4f5a6b  "),
        "8f3e1b2c-5d4a-4e6f-9a0b-1c2d3e4f5a6b"
    );
}

#[test]
fn rejects_the_all_zero_placeholder() {
    // Hypervisors and unconfigured firmware hand out this sentinel. Accepting it
    // would collapse every such host onto one AMT link.
    assert_eq!(
        parse_system_uuid("00000000-0000-0000-0000-000000000000"),
        ""
    );
}

#[test]
fn rejects_the_all_ones_placeholder() {
    assert_eq!(
        parse_system_uuid("FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF"),
        ""
    );
}

#[test]
fn rejects_malformed_input() {
    for raw in ["", "not-a-uuid", "4C4C4544-0037-5A10", "�\u{1}\u{2}"] {
        assert_eq!(parse_system_uuid(raw), "", "input {raw:?} must be rejected");
    }
}

#[test]
fn reads_the_uuid_from_a_sysfs_style_file() {
    let dir = tempfile::tempdir().expect("temp dir");
    let path = dir.path().join("product_uuid");
    fs::write(&path, "4C4C4544-0037-5A10-8054-B4C04F335432\n").expect("write fixture");

    assert_eq!(
        system_uuid_from(&path),
        "4c4c4544-0037-5a10-8054-b4c04f335432"
    );
}

#[test]
fn returns_empty_when_the_uuid_file_is_unreadable() {
    let dir = tempfile::tempdir().expect("temp dir");
    assert_eq!(system_uuid_from(dir.path().join("absent")), "");
}

#[test]
fn system_uuid_is_either_empty_or_well_formed() {
    // Reading DMI needs privileges the test runner may not have, so an empty
    // answer is legitimate; a non-empty one must be a normalized UUID.
    let got = system_uuid();
    if !got.is_empty() {
        assert_eq!(got, parse_system_uuid(&got));
    }
}
