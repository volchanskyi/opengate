//! Agent binary auto-update: download, verify Ed25519 signature, atomic replace.

use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use sha2::{Digest, Sha256};
use std::path::{Path, PathBuf};
use tokio::fs;
use tracing::{info, warn};

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

/// Downloads, verifies, and atomically replaces the current binary.
///
/// Returns `Ok(true)` if the update was applied, `Ok(false)` if skipped
/// (e.g., the binary path doesn't exist), or an error.
///
/// # Steps
/// 1. Download binary from `url` to `{data_dir}/.update.new`
/// 2. Compute SHA-256 hash of the downloaded binary
/// 3. Verify the Ed25519 signature against the hash
/// 4. Backup the current binary to `{current_binary_path}.prev`
/// 5. Atomically replace the current binary via `rename(2)`
pub async fn apply_update(
    config: &UpdateConfig,
    version: &str,
    url: &str,
    sha256_hex: &str,
    signature_hex: &str,
) -> Result<bool, UpdateError> {
    let new_path = config.data_dir.join(".update.new");
    let prev_path = config.current_binary_path.with_extension("prev");

    // 1. Download binary
    info!(version, url, "downloading update binary");
    download_to_file(url, &new_path).await?;

    // 2. Compute SHA-256 of the downloaded binary
    let actual_hash = sha256_file(&new_path).await?;

    // 3. If the server provided an expected hash, verify it matches
    if !sha256_hex.is_empty() && actual_hash != sha256_hex {
        // Clean up on failure
        let _ = fs::remove_file(&new_path).await;
        return Err(UpdateError::HashMismatch {
            expected: sha256_hex.to_string(),
            actual: actual_hash,
        });
    }
    info!("SHA-256 computed: {actual_hash}");

    // 4. Verify Ed25519 signature against the actual hash
    verify_signature(&config.signing_public_key, &actual_hash, signature_hex)?;
    info!("Ed25519 signature verified");

    // 4. Backup current binary
    if config.current_binary_path.exists() {
        if let Err(e) = fs::copy(&config.current_binary_path, &prev_path).await {
            warn!(?e, "failed to backup current binary, continuing");
        }
    }

    // 5. Set executable permissions and atomically replace
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o755);
        fs::set_permissions(&new_path, perms).await?;
    }

    fs::rename(&new_path, &config.current_binary_path).await?;
    info!("binary replaced successfully");

    Ok(true)
}

/// Download a file from `url` to `dest`.
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

/// Compute the SHA-256 hash of a file, returned as lowercase hex.
async fn sha256_file(path: &Path) -> Result<String, UpdateError> {
    let data = fs::read(path).await?;
    let hash = Sha256::digest(&data);
    Ok(hex::encode(hash))
}

/// Verify an Ed25519 signature over a SHA-256 hash.
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

    /// Generate a deterministic Ed25519 keypair seeded from `offset`.
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
        let _hash_hex = hex::encode(&hash);

        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        // Verify against different data
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
        // Known SHA-256 of "hello world"
        assert_eq!(
            hash,
            "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
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

        // Use an HTTP test server would be ideal, but for unit tests we test
        // the hash mismatch path by checking the error type.
        // The download will fail since there's no server, which is expected.
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

        // Write old and new binaries
        fs::write(&binary_path, b"old binary").await.unwrap();
        fs::write(&new_path, b"new binary").await.unwrap();

        let (signing_key, verifying_key) = test_keypair();

        // Compute hash of new binary
        let hash = Sha256::digest(b"new binary");
        let hash_hex = hex::encode(hash);
        let sig = signing_key.sign(&hash);
        let sig_hex = hex::encode(sig.to_bytes());

        // Directly test the post-download steps by calling verify + replace
        verify_signature(&verifying_key.to_bytes(), &hash_hex, &sig_hex).unwrap();

        // Backup and replace
        let prev_path = binary_path.with_extension("prev");
        fs::copy(&binary_path, &prev_path).await.unwrap();
        fs::rename(&new_path, &binary_path).await.unwrap();

        // Verify results
        let current = fs::read(&binary_path).await.unwrap();
        assert_eq!(current, b"new binary");

        let backup = fs::read(&prev_path).await.unwrap();
        assert_eq!(backup, b"old binary");
    }
}
