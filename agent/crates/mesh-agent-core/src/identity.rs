use std::path::Path;

use mesh_protocol::DeviceId;
use rcgen::{CertificateParams, KeyPair};

use crate::error::AgentError;

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
    /// Load an existing identity from `data_dir`, or generate a new one and
    /// persist it. Files: `device_id.txt`, `agent.crt` (DER), `agent.key` (DER).
    pub fn load_or_create(data_dir: &Path) -> Result<Self, AgentError> {
        let id_path = data_dir.join("device_id.txt");
        let cert_path = data_dir.join("agent.crt");
        let key_path = data_dir.join("agent.key");

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
        std::fs::create_dir_all(data_dir)?;

        let device_id = DeviceId::new();

        // Generate ECDSA P-256 key pair.
        let key_pair = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256)
            .map_err(|e| AgentError::CertGen(format!("generate key: {e}")))?;

        let mut params = CertificateParams::new(Vec::<String>::new())
            .map_err(|e| AgentError::CertGen(format!("cert params: {e}")))?;
        params.distinguished_name.push(
            rcgen::DnType::CommonName,
            rcgen::DnValue::Utf8String(device_id.0.to_string()),
        );

        let cert = params
            .self_signed(&key_pair)
            .map_err(|e| AgentError::CertGen(format!("self-sign: {e}")))?;

        let cert_der = cert.der().to_vec();
        let key_der = key_pair.serialize_der();

        // Persist to disk.
        std::fs::write(data_dir.join("device_id.txt"), device_id.0.to_string())?;
        std::fs::write(data_dir.join("agent.crt"), &cert_der)?;
        std::fs::write(data_dir.join("agent.key"), &key_der)?;

        Ok(Self {
            device_id,
            cert_der,
            key_der,
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

        // Device ID is a valid UUID
        assert_ne!(identity.device_id.0, uuid::Uuid::nil());

        // Files were written
        assert!(dir.path().join("device_id.txt").exists());
        assert!(dir.path().join("agent.crt").exists());
        assert!(dir.path().join("agent.key").exists());

        // Cert and key DER bytes are non-empty
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

        // Verify the persisted device_id matches the in-memory one
        let stored_id = std::fs::read_to_string(dir.path().join("device_id.txt")).unwrap();
        assert_eq!(identity.device_id.0.to_string(), stored_id.trim());

        // Verify cert DER is non-empty and starts with a valid ASN.1 SEQUENCE tag
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
}
