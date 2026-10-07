use std::io::Write;
use std::path::Path;

use mesh_protocol::DeviceId;
use rcgen::{CertificateParams, KeyPair};

use crate::error::AgentError;

/// Filename for the persisted device UUID.
pub const DEVICE_ID_FILE: &str = "device_id.txt";
/// Filename for the DER-encoded agent certificate.
pub const CERT_FILE: &str = "agent.crt";
/// Filename for the DER-encoded agent private key.
pub const KEY_FILE: &str = "agent.key";

/// Mode for the agent's data directory: owner-only, so no other local account
/// can traverse it to reach the device's private key.
#[cfg(unix)]
const DATA_DIR_MODE: u32 = 0o700;

/// Mode for the agent's private key file: owner read/write only.
#[cfg(unix)]
const KEY_FILE_MODE: u32 = 0o600;

/// Ensures `data_dir` exists with owner-only mode, applied even to a pre-existing directory.
pub fn ensure_private_dir(data_dir: &Path) -> Result<(), AgentError> {
    std::fs::create_dir_all(data_dir)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(data_dir, std::fs::Permissions::from_mode(DATA_DIR_MODE))?;
    }
    Ok(())
}

/// Writes `contents` to `path` readable only by its owner; an existing file's mode is reset.
fn write_private_file(path: &Path, contents: &[u8]) -> Result<(), AgentError> {
    let mut options = std::fs::OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(KEY_FILE_MODE);
    }
    let mut file = options.open(path)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        file.set_permissions(std::fs::Permissions::from_mode(KEY_FILE_MODE))?;
    }
    file.write_all(contents)?;
    Ok(())
}

/// Generate an ECDSA P-256 key pair and certificate params with the device ID as CN.
fn generate_key_and_params(
    device_id: &DeviceId,
) -> Result<(KeyPair, CertificateParams), AgentError> {
    let key_pair = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256)
        .map_err(|e| AgentError::CertGen(format!("generate key: {e}")))?;

    let mut params = CertificateParams::new(Vec::<String>::new())
        .map_err(|e| AgentError::CertGen(format!("cert params: {e}")))?;
    params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String(device_id.0.to_string()),
    );

    Ok((key_pair, params))
}

/// Persistent agent identity: device ID and mTLS certificate.
pub struct AgentIdentity {
    /// The stable device UUID, persisted to disk.
    pub device_id: DeviceId,
    /// DER-encoded agent certificate.
    pub cert_der: Vec<u8>,
    /// DER-encoded private key.
    pub key_der: Vec<u8>,
}

impl AgentIdentity {
    /// Loads the identity from `data_dir`, or generates and persists a new one.
    pub fn load_or_create(data_dir: &Path) -> Result<Self, AgentError> {
        let id_path = data_dir.join(DEVICE_ID_FILE);
        let cert_path = data_dir.join(CERT_FILE);
        let key_path = data_dir.join(KEY_FILE);

        if id_path.exists() && cert_path.exists() && key_path.exists() {
            return Self::load(&id_path, &cert_path, &key_path);
        }

        Self::generate(data_dir)
    }

    fn load(id_path: &Path, cert_path: &Path, key_path: &Path) -> Result<Self, AgentError> {
        let id_str = std::fs::read_to_string(id_path).map_err(|e| {
            AgentError::Io(std::io::Error::new(
                e.kind(),
                format!("{}: {e}", id_path.display()),
            ))
        })?;
        let device_id = DeviceId(
            uuid::Uuid::parse_str(id_str.trim())
                .map_err(|e| AgentError::CertGen(format!("invalid device ID: {e}")))?,
        );
        let cert_der = std::fs::read(cert_path).map_err(|e| {
            AgentError::Io(std::io::Error::new(
                e.kind(),
                format!("{}: {e}", cert_path.display()),
            ))
        })?;
        let key_der = std::fs::read(key_path).map_err(|e| {
            AgentError::Io(std::io::Error::new(
                e.kind(),
                format!("{}: {e}", key_path.display()),
            ))
        })?;

        Ok(Self {
            device_id,
            cert_der,
            key_der,
        })
    }

    fn generate(data_dir: &Path) -> Result<Self, AgentError> {
        ensure_private_dir(data_dir)?;

        let device_id = DeviceId::new();
        let (key_pair, params) = generate_key_and_params(&device_id)?;

        let cert = params
            .self_signed(&key_pair)
            .map_err(|e| AgentError::CertGen(format!("self-sign: {e}")))?;

        let cert_der = cert.der().to_vec();
        let key_der = key_pair.serialize_der();

        std::fs::write(data_dir.join(DEVICE_ID_FILE), device_id.0.to_string())?;
        std::fs::write(data_dir.join(CERT_FILE), &cert_der)?;
        write_private_file(&data_dir.join(KEY_FILE), &key_der)?;

        Ok(Self {
            device_id,
            cert_der,
            key_der,
        })
    }

    /// Saves a CA-signed DER certificate over the self-signed one.
    pub fn save_signed_cert(data_dir: &Path, cert_der: &[u8]) -> Result<(), AgentError> {
        std::fs::write(data_dir.join(CERT_FILE), cert_der)?;
        Ok(())
    }
}

/// Pending enrollment identity: key pair and CSR generated but not yet signed.
pub struct PendingIdentity {
    /// The stable device UUID.
    pub device_id: DeviceId,
    /// DER-encoded private key (saved to disk).
    pub key_der: Vec<u8>,
    /// PEM-encoded PKCS#10 certificate signing request.
    pub csr_pem: String,
}

impl PendingIdentity {
    /// Generates a key pair and CSR, saving the device ID and key to `data_dir`.
    pub fn generate(data_dir: &Path) -> Result<Self, AgentError> {
        ensure_private_dir(data_dir)?;

        let device_id = DeviceId::new();
        let (key_pair, params) = generate_key_and_params(&device_id)?;

        let csr = params
            .serialize_request(&key_pair)
            .map_err(|e| AgentError::CertGen(format!("create CSR: {e}")))?;

        let key_der = key_pair.serialize_der();

        std::fs::write(data_dir.join(DEVICE_ID_FILE), device_id.0.to_string())?;
        write_private_file(&data_dir.join(KEY_FILE), &key_der)?;

        Ok(Self {
            device_id,
            key_der,
            csr_pem: csr
                .pem()
                .map_err(|e| AgentError::CertGen(format!("PEM encode CSR: {e}")))?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generates_new_identity() {
        let dir = tempfile::tempdir().unwrap();
        let identity = AgentIdentity::load_or_create(dir.path()).unwrap();

        assert_ne!(identity.device_id.0, uuid::Uuid::nil());

        assert!(dir.path().join("device_id.txt").exists());
        assert!(dir.path().join("agent.crt").exists());
        assert!(dir.path().join("agent.key").exists());

        assert!(!identity.cert_der.is_empty());
        assert!(!identity.key_der.is_empty());
    }

    #[test]
    fn test_reloads_existing() {
        let dir = tempfile::tempdir().unwrap();
        let id1 = AgentIdentity::load_or_create(dir.path()).unwrap();
        let id2 = AgentIdentity::load_or_create(dir.path()).unwrap();

        assert_eq!(id1.device_id.0, id2.device_id.0);
        assert_eq!(id1.cert_der, id2.cert_der);
        assert_eq!(id1.key_der, id2.key_der);
    }

    #[test]
    fn test_cert_cn_matches_device_id() {
        let dir = tempfile::tempdir().unwrap();
        let identity = AgentIdentity::load_or_create(dir.path()).unwrap();

        let stored_id = std::fs::read_to_string(dir.path().join("device_id.txt")).unwrap();
        assert_eq!(identity.device_id.0.to_string(), stored_id.trim());

        assert!(!identity.cert_der.is_empty());
        assert_eq!(
            identity.cert_der[0], 0x30,
            "DER should start with SEQUENCE tag"
        );
    }

    #[test]
    fn test_different_dirs_generate_different_ids() {
        let dir1 = tempfile::tempdir().unwrap();
        let dir2 = tempfile::tempdir().unwrap();
        let id1 = AgentIdentity::load_or_create(dir1.path()).unwrap();
        let id2 = AgentIdentity::load_or_create(dir2.path()).unwrap();

        assert_ne!(id1.device_id.0, id2.device_id.0);
    }

    #[test]
    fn test_pending_identity_generates_csr() {
        let dir = tempfile::tempdir().unwrap();
        let pending = PendingIdentity::generate(dir.path()).unwrap();

        assert!(pending.csr_pem.contains("BEGIN CERTIFICATE REQUEST"));
        assert!(pending.csr_pem.contains("END CERTIFICATE REQUEST"));

        assert!(dir.path().join("agent.key").exists());
        assert!(dir.path().join("device_id.txt").exists());
        assert!(!dir.path().join("agent.crt").exists());

        assert!(!pending.key_der.is_empty());
        assert_ne!(pending.device_id.0, uuid::Uuid::nil());
    }

    #[test]
    fn test_save_signed_cert_writes_file() {
        let dir = tempfile::tempdir().unwrap();
        let fake_cert = b"fake-cert-der-data";
        AgentIdentity::save_signed_cert(dir.path(), fake_cert).unwrap();

        let saved = std::fs::read(dir.path().join("agent.crt")).unwrap();
        assert_eq!(saved, fake_cert);
    }

    #[test]
    fn load_or_create_regenerates_when_any_file_is_missing() {
        for which_present in ["id", "cert", "key", "id+cert", "id+key", "cert+key"] {
            let dir = tempfile::tempdir().unwrap();
            if which_present.contains("id") {
                std::fs::write(
                    dir.path().join(DEVICE_ID_FILE),
                    uuid::Uuid::new_v4().to_string(),
                )
                .unwrap();
            }
            if which_present.contains("cert") {
                std::fs::write(dir.path().join(CERT_FILE), b"junk-not-a-real-cert").unwrap();
            }
            if which_present.contains("key") {
                std::fs::write(dir.path().join(KEY_FILE), b"junk-not-a-real-key").unwrap();
            }

            let identity = AgentIdentity::load_or_create(dir.path()).unwrap_or_else(|e| {
                panic!("load_or_create with only [{which_present}] present must regenerate: {e}")
            });
            assert!(dir.path().join(DEVICE_ID_FILE).exists());
            assert!(dir.path().join(CERT_FILE).exists());
            assert!(dir.path().join(KEY_FILE).exists());
            assert_eq!(
                identity.cert_der[0], 0x30,
                "regenerated cert must be valid DER (which={which_present})"
            );
        }
    }

    #[test]
    fn test_pending_then_load_after_signing() {
        let dir = tempfile::tempdir().unwrap();
        let pending = PendingIdentity::generate(dir.path()).unwrap();

        let fake_cert = vec![0x30, 0x82, 0x01, 0x00];
        AgentIdentity::save_signed_cert(dir.path(), &fake_cert).unwrap();

        let identity = AgentIdentity::load_or_create(dir.path()).unwrap();
        assert_eq!(identity.device_id.0, pending.device_id.0);
        assert_eq!(identity.cert_der, fake_cert);
        assert_eq!(identity.key_der, pending.key_der);
    }
}
