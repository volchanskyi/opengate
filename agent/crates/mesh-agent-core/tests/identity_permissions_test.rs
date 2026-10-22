//! The mTLS private key and its directory stay owner-only when an identity is saved.
//! These cases start from wide-open modes the agent repairs, so they live outside the source.

#![cfg(unix)]

use std::os::unix::fs::PermissionsExt;
use std::path::Path;

use mesh_agent_core::identity::{ensure_private_dir, AgentIdentity, KEY_FILE};
use mesh_agent_core::identity_store::{self, Commit};

fn mode_of(path: &Path) -> u32 {
    std::fs::metadata(path).unwrap().permissions().mode() & 0o777
}

fn issued_identity() -> AgentIdentity {
    let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
    let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
    params
        .distinguished_name
        .push(rcgen::DnType::CommonName, uuid::Uuid::new_v4().to_string());
    let cert = params.self_signed(&key).unwrap();
    AgentIdentity::from_issued(cert.der().to_vec(), key.serialize_der()).unwrap()
}

fn save(data_dir: &Path, identity: &AgentIdentity) {
    ensure_private_dir(data_dir).unwrap();
    identity_store::commit(data_dir, identity.staged(data_dir, Commit::new())).unwrap();
}

#[test]
fn a_saved_identity_is_an_owner_only_key_in_an_owner_only_directory() {
    let dir = tempfile::tempdir().unwrap();
    let data_dir = dir.path().join("data");

    save(&data_dir, &issued_identity());

    assert_eq!(mode_of(&data_dir), 0o700, "data directory");
    assert_eq!(mode_of(&data_dir.join(KEY_FILE)), 0o600, "agent.key");
}

#[test]
fn a_directory_that_already_exists_world_readable_is_repaired() {
    let dir = tempfile::tempdir().unwrap();
    let data_dir = dir.path().join("data");
    std::fs::create_dir_all(&data_dir).unwrap();
    std::fs::set_permissions(&data_dir, std::fs::Permissions::from_mode(0o755)).unwrap();

    save(&data_dir, &issued_identity());

    assert_eq!(mode_of(&data_dir), 0o700, "pre-created directory");
    assert_eq!(mode_of(&data_dir.join(KEY_FILE)), 0o600, "agent.key");
}

#[test]
fn a_stale_key_is_replaced_rather_than_refused() {
    let dir = tempfile::tempdir().unwrap();
    let data_dir = dir.path().join("data");
    std::fs::create_dir_all(&data_dir).unwrap();
    let key_path = data_dir.join(KEY_FILE);
    std::fs::write(&key_path, b"stale-key-from-a-partial-uninstall").unwrap();
    std::fs::set_permissions(&key_path, std::fs::Permissions::from_mode(0o644)).unwrap();

    let identity = issued_identity();
    save(&data_dir, &identity);

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
