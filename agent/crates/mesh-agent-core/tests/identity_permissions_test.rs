//! The device's private key and the directory holding it are the endpoint's
//! mTLS credential. Every other local account on the machine must be unable to
//! read either, on both identity paths and whatever state the directory was
//! left in.
//!
//! These cases put a directory or a key into the wide-open state the agent has
//! to repair, so they are the one place in the crate that deliberately widens a
//! mode. They live out here rather than beside the code for that reason: a
//! permissive mode in a source file is a finding, and a permissive mode in a
//! test is the fixture the finding is about.

#![cfg(unix)]

use std::os::unix::fs::PermissionsExt;
use std::path::Path;

use mesh_agent_core::identity::{AgentIdentity, PendingIdentity, KEY_FILE};

fn mode_of(path: &Path) -> u32 {
    std::fs::metadata(path).unwrap().permissions().mode() & 0o777
}

/// A path the agent has to create itself — a container or an
/// `OPENGATE_DATA_DIR` override, where the installer never ran.
fn uncreated_data_dir(dir: &tempfile::TempDir) -> std::path::PathBuf {
    dir.path().join("data")
}

#[test]
fn generate_writes_an_owner_only_key_in_an_owner_only_directory() {
    let dir = tempfile::tempdir().unwrap();
    let data_dir = uncreated_data_dir(&dir);

    AgentIdentity::load_or_create(&data_dir).unwrap();

    assert_eq!(mode_of(&data_dir), 0o700, "data directory");
    assert_eq!(mode_of(&data_dir.join(KEY_FILE)), 0o600, "agent.key");
}

#[test]
fn pending_generate_writes_an_owner_only_key_in_an_owner_only_directory() {
    let dir = tempfile::tempdir().unwrap();
    let data_dir = uncreated_data_dir(&dir);

    PendingIdentity::generate(&data_dir).unwrap();

    assert_eq!(mode_of(&data_dir), 0o700, "data directory");
    assert_eq!(mode_of(&data_dir.join(KEY_FILE)), 0o600, "agent.key");
}

/// The installer creates the data directory first with a bare `mkdir -p`, so on
/// a real endpoint the agent always arrives at a directory that already exists —
/// and `create_dir_all` leaves an existing directory's mode alone. The mode has
/// to be set, not merely requested at creation.
#[test]
fn a_directory_that_already_exists_world_readable_is_repaired() {
    for path in ["agent", "pending"] {
        let dir = tempfile::tempdir().unwrap();
        let data_dir = dir.path().join(path);
        std::fs::create_dir_all(&data_dir).unwrap();
        std::fs::set_permissions(&data_dir, std::fs::Permissions::from_mode(0o755)).unwrap();

        if path == "agent" {
            AgentIdentity::load_or_create(&data_dir).unwrap();
        } else {
            PendingIdentity::generate(&data_dir).unwrap();
        }

        assert_eq!(mode_of(&data_dir), 0o700, "pre-created directory ({path})");
        assert_eq!(
            mode_of(&data_dir.join(KEY_FILE)),
            0o600,
            "agent.key ({path})"
        );
    }
}

/// A partial uninstall or an interrupted enrolment leaves a key behind without
/// the other two files, and `load_or_create` then regenerates over it. Refusing
/// to overwrite would stop the agent from starting, so the stale key is replaced
/// — and ends owner-only however it was left.
#[test]
fn a_stale_key_is_replaced_rather_than_refused() {
    let dir = tempfile::tempdir().unwrap();
    let data_dir = dir.path().join("data");
    std::fs::create_dir_all(&data_dir).unwrap();
    let key_path = data_dir.join(KEY_FILE);
    std::fs::write(&key_path, b"stale-key-from-a-partial-uninstall").unwrap();
    std::fs::set_permissions(&key_path, std::fs::Permissions::from_mode(0o644)).unwrap();

    let identity = AgentIdentity::load_or_create(&data_dir).unwrap();

    assert_eq!(
        mode_of(&key_path),
        0o600,
        "the stale key is left owner-only"
    );
    assert_eq!(
        std::fs::read(&key_path).unwrap(),
        identity.key_der,
        "the stale bytes are gone, not appended to"
    );
}
