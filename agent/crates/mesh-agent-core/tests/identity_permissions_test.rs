//! The mTLS private key and its directory stay owner-only on both identity paths.
//! These cases start from wide-open modes the agent repairs, so they live outside the source.

#![cfg(unix)]

use std::os::unix::fs::PermissionsExt;
use std::path::Path;

use mesh_agent_core::identity::{AgentIdentity, PendingIdentity, KEY_FILE};

fn mode_of(path: &Path) -> u32 {
    std::fs::metadata(path).unwrap().permissions().mode() & 0o777
}

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
