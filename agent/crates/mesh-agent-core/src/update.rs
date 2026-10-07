//! Agent binary auto-update: download, verify Ed25519 signature, atomic replace.

use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use sha2::{Digest, Sha256};
use std::path::{Path, PathBuf};
use tokio::fs;
use tracing::{debug, info, warn};

/// Mode for the replaced agent binary: owner and group, nothing for anyone else.
#[cfg(unix)]
const BINARY_MODE: u32 = 0o750;

/// Errors that can occur during the update process.
#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum UpdateError {
    /// Failed to download the update binary.
    #[error("download failed: {0}")]
    Download(String),

    /// SHA-256 hash of the downloaded binary does not match the expected value.
    #[error("hash mismatch: expected {expected}, got {actual}")]
    HashMismatch {
        /// Expected hash from the manifest.
        expected: String,
        /// Actual hash computed from the downloaded binary.
        actual: String,
    },

    /// Ed25519 signature verification failed.
    #[error("invalid signature")]
    SignatureInvalid,

    /// I/O error during file operations.
    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    /// Hex decoding error.
    #[error("hex decode error: {0}")]
    Hex(String),
}

/// Configuration for the update process.
pub struct UpdateConfig {
    /// Ed25519 public key for verifying update signatures.
    pub signing_public_key: [u8; 32],
    /// Path to the currently running binary.
    pub current_binary_path: PathBuf,
    /// Data directory for temporary files.
    pub data_dir: PathBuf,
}

/// Downloads, verifies and atomically replaces the binary; `Ok(false)` means skipped.
pub async fn apply_update(
    config: &UpdateConfig,
    version: &str,
    url: &str,
    sha256_hex: &str,
    signature_hex: &str,
) -> Result<bool, UpdateError> {
    let new_path = config.data_dir.join(".update.new");
    let prev_path = config.current_binary_path.with_extension("prev");

    // Skips the whole update when the running binary already hashes to the manifest's sha256.
    if !sha256_hex.is_empty() && config.current_binary_path.exists() {
        let current_hash = sha256_file(&config.current_binary_path).await?;
        if current_hash == sha256_hex {
            info!(
                version,
                sha256 = sha256_hex,
                "update precheck: current binary already matches manifest hash, skipping"
            );
            return Ok(false);
        }
    }

    info!(version, url, "downloading update binary");
    download_to_file(url, &new_path).await?;

    let actual_hash = sha256_file(&new_path).await?;

    if !sha256_hex.is_empty() && actual_hash != sha256_hex {
        if let Err(e) = fs::remove_file(&new_path).await {
            warn!(path = %new_path.display(), error = %e, "failed to remove tampered binary on hash mismatch");
        }
        return Err(UpdateError::HashMismatch {
            expected: sha256_hex.to_string(),
            actual: actual_hash,
        });
    }
    info!("SHA-256 computed: {actual_hash}");

    verify_signature(&config.signing_public_key, &actual_hash, signature_hex)?;
    info!("Ed25519 signature verified");

    if config.current_binary_path.exists() {
        if let Err(e) = fs::copy(&config.current_binary_path, &prev_path).await {
            warn!(?e, "failed to backup current binary, continuing");
        }
    }

    // Owner and group only: the binary is owned by root and the installer writes the same mode.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(BINARY_MODE);
        fs::set_permissions(&new_path, perms).await?;
    }

    fs::rename(&new_path, &config.current_binary_path).await?;
    restore_selinux_context(&config.current_binary_path).await;
    info!("binary replaced successfully");

    // The sentinel lets the startup watchdog detect a pending update.
    write_update_pending(&config.data_dir).await;

    Ok(true)
}

/// Restores the binary from `{binary_path}.prev`; `Ok(false)` means no `.prev` exists.
pub async fn rollback(binary_path: &Path) -> Result<bool, UpdateError> {
    let prev = binary_path.with_extension("prev");
    if !prev.exists() {
        return Ok(false);
    }
    fs::rename(&prev, binary_path).await?;
    restore_selinux_context(binary_path).await;
    info!("rolled back to previous binary");
    Ok(true)
}

/// Returns `true` if the update-pending sentinel exists (post-update, pre-verify).
pub fn is_update_pending(data_dir: &Path) -> bool {
    data_dir.join(SENTINEL_UPDATE_PENDING).exists()
}

/// Removes the update-pending sentinel after successful verification.
pub async fn clear_update_pending(data_dir: &Path) {
    let path = data_dir.join(SENTINEL_UPDATE_PENDING);
    if let Err(e) = fs::remove_file(&path).await {
        if e.kind() != std::io::ErrorKind::NotFound {
            warn!(error = %e, "failed to clear update-pending sentinel");
        }
    }
}

/// Reads the rollback counter (number of consecutive rollbacks).
pub async fn rollback_count(data_dir: &Path) -> u32 {
    let path = data_dir.join(FILE_ROLLBACK_COUNT);
    match fs::read_to_string(&path).await {
        Ok(s) => s.trim().parse().unwrap_or(0),
        Err(_) => 0,
    }
}

/// Increments the rollback counter file.
pub async fn increment_rollback_count(data_dir: &Path) {
    let count = rollback_count(data_dir).await + 1;
    let path = data_dir.join(FILE_ROLLBACK_COUNT);
    if let Err(e) = fs::write(&path, count.to_string()).await {
        warn!(path = %path.display(), error = %e, "failed to persist rollback counter");
    }
}

/// Resets the rollback counter to zero (called after a healthy start).
pub async fn reset_rollback_count(data_dir: &Path) {
    let path = data_dir.join(FILE_ROLLBACK_COUNT);
    if let Err(e) = fs::remove_file(&path).await {
        if e.kind() != std::io::ErrorKind::NotFound {
            warn!(path = %path.display(), error = %e, "failed to clear rollback counter");
        }
    }
}

/// Maximum consecutive rollbacks before giving up.
pub const MAX_ROLLBACKS: u32 = 2;

/// Sentinel file written after a binary replacement, cleared after healthy start.
const SENTINEL_UPDATE_PENDING: &str = ".update-pending";

/// Counter file tracking consecutive rollback attempts.
const FILE_ROLLBACK_COUNT: &str = ".rollback-count";

/// Writes the update-pending sentinel after a successful binary replacement.
async fn write_update_pending(data_dir: &Path) {
    let path = data_dir.join(SENTINEL_UPDATE_PENDING);
    if let Err(e) = fs::write(&path, b"1").await {
        warn!(error = %e, "failed to write update-pending sentinel");
    }
}

/// Runs `restorecon` because `rename(2)` keeps the source SELinux context, which blocks execution.
async fn restore_selinux_context(path: &Path) {
    match tokio::process::Command::new("restorecon")
        .arg(path)
        .output()
        .await
    {
        Ok(output) if output.status.success() => {
            info!(path = %path.display(), "SELinux context restored");
        }
        Ok(output) => {
            let stderr = String::from_utf8_lossy(&output.stderr);
            debug!(path = %path.display(), stderr = %stderr.trim(), "restorecon exited non-zero (non-SELinux system?)");
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            debug!("restorecon not found, skipping SELinux context restore");
        }
        Err(e) => {
            debug!(error = %e, "restorecon failed, skipping SELinux context restore");
        }
    }
}

async fn download_to_file(url: &str, dest: &Path) -> Result<(), UpdateError> {
    let response = reqwest::get(url)
        .await
        .map_err(|e| UpdateError::Download(e.to_string()))?;

    if !response.status().is_success() {
        return Err(UpdateError::Download(format!("HTTP {}", response.status())));
    }

    let bytes = response
        .bytes()
        .await
        .map_err(|e| UpdateError::Download(e.to_string()))?;

    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent).await?;
    }

    fs::write(dest, &bytes).await?;
    Ok(())
}

async fn sha256_file(path: &Path) -> Result<String, UpdateError> {
    let data = fs::read(path).await?;
    let hash = Sha256::digest(&data);
    Ok(hex::encode(hash))
}

fn verify_signature(
    public_key_bytes: &[u8; 32],
    sha256_hex: &str,
    signature_hex: &str,
) -> Result<(), UpdateError> {
    let verifying_key =
        VerifyingKey::from_bytes(public_key_bytes).map_err(|_| UpdateError::SignatureInvalid)?;

    let hash_bytes = hex::decode(sha256_hex).map_err(|e| UpdateError::Hex(e.to_string()))?;

    let sig_bytes = hex::decode(signature_hex).map_err(|e| UpdateError::Hex(e.to_string()))?;

    let signature = Signature::from_slice(&sig_bytes).map_err(|_| UpdateError::SignatureInvalid)?;

    verifying_key
        .verify(&hash_bytes, &signature)
        .map_err(|_| UpdateError::SignatureInvalid)?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::{Signer, SigningKey};
    use sha2::{Digest, Sha256};

    fn test_keypair_with_offset(offset: u8) -> (SigningKey, VerifyingKey) {
        let secret: [u8; 32] =
            core::array::from_fn(|i| (i as u8).wrapping_add(offset).wrapping_add(1));
        let signing_key = SigningKey::from_bytes(&secret);
        let verifying_key = signing_key.verifying_key();
        (signing_key, verifying_key)
    }

    fn test_keypair() -> (SigningKey, VerifyingKey) {
        test_keypair_with_offset(0)
    }

    fn test_keypair_alt() -> (SigningKey, VerifyingKey) {
        test_keypair_with_offset(32)
    }

    #[test]
    fn test_verify_signature_valid() {
        let (signing_key, verifying_key) = test_keypair();

        let data = b"test binary data";
        let hash = Sha256::digest(data);
        let hash_hex = hex::encode(hash);

        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        let result = verify_signature(&verifying_key.to_bytes(), &hash_hex, &sig_hex);
        assert!(result.is_ok());
    }

    #[test]
    fn test_verify_signature_wrong_data() {
        let (signing_key, verifying_key) = test_keypair();

        let hash = Sha256::digest(b"original data");
        let _hash_hex = hex::encode(hash);

        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        let wrong_hash = Sha256::digest(b"tampered data");
        let wrong_hex = hex::encode(wrong_hash);

        let result = verify_signature(&verifying_key.to_bytes(), &wrong_hex, &sig_hex);
        assert!(matches!(result, Err(UpdateError::SignatureInvalid)));
    }

    #[test]
    fn test_verify_signature_wrong_key() {
        let (signing_key, _) = test_keypair();
        let (_, other_verifying_key) = test_keypair_alt();

        let hash = Sha256::digest(b"test data");
        let hash_hex = hex::encode(hash);

        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        let result = verify_signature(&other_verifying_key.to_bytes(), &hash_hex, &sig_hex);
        assert!(matches!(result, Err(UpdateError::SignatureInvalid)));
    }

    #[test]
    fn test_verify_signature_invalid_hex() {
        let (_, verifying_key) = test_keypair();

        let result = verify_signature(&verifying_key.to_bytes(), "not-hex!", "also-not-hex!");
        assert!(matches!(result, Err(UpdateError::Hex(_))));
    }

    #[tokio::test]
    async fn test_sha256_file() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("test.bin");
        fs::write(&path, b"hello world").await.unwrap();

        let hash = sha256_file(&path).await.unwrap();
        assert_eq!(
            hash,
            "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
        );
    }

    #[tokio::test]
    async fn test_rollback_restores_prev() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        let prev_path = binary_path.with_extension("prev");

        fs::write(&binary_path, b"bad binary").await.unwrap();
        fs::write(&prev_path, b"good binary").await.unwrap();

        let result = rollback(&binary_path).await.unwrap();
        assert!(result);

        let content = fs::read(&binary_path).await.unwrap();
        assert_eq!(content, b"good binary");
        assert!(!prev_path.exists());
    }

    #[tokio::test]
    async fn test_rollback_no_prev() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        fs::write(&binary_path, b"current").await.unwrap();

        let result = rollback(&binary_path).await.unwrap();
        assert!(!result);
    }

    #[tokio::test]
    async fn test_update_pending_sentinel() {
        let dir = tempfile::tempdir().unwrap();

        assert!(!is_update_pending(dir.path()));

        write_update_pending(dir.path()).await;
        assert!(is_update_pending(dir.path()));

        clear_update_pending(dir.path()).await;
        assert!(!is_update_pending(dir.path()));
    }

    #[tokio::test]
    async fn test_rollback_counter() {
        let dir = tempfile::tempdir().unwrap();

        assert_eq!(rollback_count(dir.path()).await, 0);

        increment_rollback_count(dir.path()).await;
        assert_eq!(rollback_count(dir.path()).await, 1);

        increment_rollback_count(dir.path()).await;
        assert_eq!(rollback_count(dir.path()).await, 2);

        reset_rollback_count(dir.path()).await;
        assert_eq!(rollback_count(dir.path()).await, 0);
    }

    #[tokio::test]
    async fn test_apply_update_full_pipeline() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        fs::write(&binary_path, b"old binary").await.unwrap();

        let (signing_key, verifying_key) = test_keypair();

        let fake_binary = b"new agent binary v2.0.0";
        let hash = Sha256::digest(fake_binary);
        let hash_hex = hex::encode(hash);
        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        let mut server = mockito::Server::new_async().await;
        let mock = server
            .mock("GET", "/agent-v2.0.0")
            .with_status(200)
            .with_body(fake_binary)
            .create_async()
            .await;

        let config = UpdateConfig {
            signing_public_key: verifying_key.to_bytes(),
            current_binary_path: binary_path.clone(),
            data_dir: dir.path().to_path_buf(),
        };

        let url = format!("{}/agent-v2.0.0", server.url());
        let result = apply_update(&config, "2.0.0", &url, &hash_hex, &sig_hex).await;
        assert!(result.is_ok(), "apply_update failed: {:?}", result.err());
        assert!(result.unwrap(), "apply_update should return true");

        mock.assert_async().await;

        let current = fs::read(&binary_path).await.unwrap();
        assert_eq!(current, fake_binary, "binary should be replaced");

        let prev_path = binary_path.with_extension("prev");
        let backup = fs::read(&prev_path).await.unwrap();
        assert_eq!(backup, b"old binary", "old binary should be backed up");

        assert!(
            is_update_pending(dir.path()),
            "update-pending sentinel should exist"
        );
    }

    #[tokio::test]
    async fn test_apply_update_hash_mismatch() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        fs::write(&binary_path, b"old binary").await.unwrap();

        let (_, verifying_key) = test_keypair();

        let config = UpdateConfig {
            signing_public_key: verifying_key.to_bytes(),
            current_binary_path: binary_path,
            data_dir: dir.path().to_path_buf(),
        };

        let result = apply_update(
            &config,
            "1.0.0",
            "http://127.0.0.1:1/nonexistent",
            "abc123",
            "def456",
        )
        .await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_atomic_replace_with_backup() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        let new_path = dir.path().join(".update.new");

        fs::write(&binary_path, b"old binary").await.unwrap();
        fs::write(&new_path, b"new binary").await.unwrap();

        let (signing_key, verifying_key) = test_keypair();

        let hash = Sha256::digest(b"new binary");
        let hash_hex = hex::encode(hash);
        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        verify_signature(&verifying_key.to_bytes(), &hash_hex, &sig_hex).unwrap();

        let prev_path = binary_path.with_extension("prev");
        fs::copy(&binary_path, &prev_path).await.unwrap();
        fs::rename(&new_path, &binary_path).await.unwrap();

        let current = fs::read(&binary_path).await.unwrap();
        assert_eq!(current, b"new binary");

        let backup = fs::read(&prev_path).await.unwrap();
        assert_eq!(backup, b"old binary");
    }

    #[tokio::test]
    async fn test_apply_update_precheck_skips_when_hash_matches() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        let body = b"identical binary contents";
        fs::write(&binary_path, body).await.unwrap();

        let (signing_key, verifying_key) = test_keypair();
        let hash = Sha256::digest(body);
        let hash_hex = hex::encode(hash);
        let sig_hex = hex::encode(signing_key.sign(&hash).to_bytes());

        let mut server = mockito::Server::new_async().await;
        let mock = server
            .mock("GET", "/agent-vNEW")
            .expect(0)
            .create_async()
            .await;

        let config = UpdateConfig {
            signing_public_key: verifying_key.to_bytes(),
            current_binary_path: binary_path.clone(),
            data_dir: dir.path().to_path_buf(),
        };

        let url = format!("{}/agent-vNEW", server.url());
        let result = apply_update(&config, "9.9.9", &url, &hash_hex, &sig_hex).await;
        assert!(result.is_ok(), "apply_update errored: {:?}", result.err());
        assert!(
            !result.unwrap(),
            "precheck must return Ok(false) when current binary already matches manifest sha256"
        );

        mock.assert_async().await;

        assert_eq!(fs::read(&binary_path).await.unwrap(), body);
        assert!(!binary_path.with_extension("prev").exists());
        assert!(!is_update_pending(dir.path()));
    }

    #[tokio::test]
    async fn test_apply_update_precheck_skipped_with_empty_hash() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        fs::write(&binary_path, b"current binary").await.unwrap();

        let (_, verifying_key) = test_keypair();
        let config = UpdateConfig {
            signing_public_key: verifying_key.to_bytes(),
            current_binary_path: binary_path,
            data_dir: dir.path().to_path_buf(),
        };

        let result = apply_update(
            &config,
            "1.0.0",
            "http://127.0.0.1:1/nonexistent",
            "", // An empty hash bypasses the precheck.
            "",
        )
        .await;
        assert!(
            result.is_err(),
            "empty hash must bypass precheck and attempt download"
        );
    }

    #[tokio::test]
    async fn test_apply_update_precheck_skipped_when_current_binary_missing() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("nonexistent-agent");

        let (_, verifying_key) = test_keypair();
        let config = UpdateConfig {
            signing_public_key: verifying_key.to_bytes(),
            current_binary_path: binary_path,
            data_dir: dir.path().to_path_buf(),
        };

        let result = apply_update(
            &config,
            "1.0.0",
            "http://127.0.0.1:1/nonexistent",
            "a".repeat(64).as_str(),
            "deadbeef",
        )
        .await;
        assert!(
            result.is_err(),
            "missing current binary must bypass precheck and attempt download"
        );
    }

    #[tokio::test]
    async fn test_apply_update_proceeds_when_hash_differs() {
        let dir = tempfile::tempdir().unwrap();
        let binary_path = dir.path().join("agent");
        fs::write(&binary_path, b"old binary").await.unwrap();

        let (signing_key, verifying_key) = test_keypair();
        let new_body = b"new agent binary";
        let new_hash = Sha256::digest(new_body);
        let new_hash_hex = hex::encode(new_hash);
        let new_sig_hex = hex::encode(signing_key.sign(&new_hash).to_bytes());

        let mut server = mockito::Server::new_async().await;
        let mock = server
            .mock("GET", "/agent-vNEW")
            .with_status(200)
            .with_body(new_body)
            .create_async()
            .await;

        let config = UpdateConfig {
            signing_public_key: verifying_key.to_bytes(),
            current_binary_path: binary_path.clone(),
            data_dir: dir.path().to_path_buf(),
        };

        let url = format!("{}/agent-vNEW", server.url());
        let result = apply_update(&config, "2.0.0", &url, &new_hash_hex, &new_sig_hex).await;
        assert!(result.is_ok(), "apply_update errored: {:?}", result.err());
        assert!(
            result.unwrap(),
            "hash differs → precheck must NOT skip → apply_update returns true"
        );
        mock.assert_async().await;
        assert_eq!(fs::read(&binary_path).await.unwrap(), new_body);
    }
}
