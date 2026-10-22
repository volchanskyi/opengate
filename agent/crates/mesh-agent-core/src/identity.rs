use std::path::{Path, PathBuf};

use mesh_protocol::DeviceId;
use rcgen::{CertificateParams, KeyPair};

use crate::error::AgentError;
use crate::identity_store::Commit;

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

/// Persistent agent identity: device ID and mTLS certificate.
pub struct AgentIdentity {
    /// The device number the certificate carries, chosen by the server at enrollment.
    pub device_id: DeviceId,
    /// DER-encoded agent certificate.
    pub cert_der: Vec<u8>,
    /// DER-encoded PKCS#8 private key.
    pub key_der: Vec<u8>,
}

impl AgentIdentity {
    /// The three files an identity occupies in `data_dir`.
    pub fn files(data_dir: &Path) -> [PathBuf; 3] {
        [
            data_dir.join(DEVICE_ID_FILE),
            data_dir.join(CERT_FILE),
            data_dir.join(KEY_FILE),
        ]
    }

    /// Whether every identity file exists in `data_dir`.
    pub fn exists(data_dir: &Path) -> bool {
        Self::files(data_dir).iter().all(|p| p.exists())
    }

    /// Loads the identity saved in `data_dir`; a missing file is an error, never a new identity.
    pub fn load(data_dir: &Path) -> Result<Self, AgentError> {
        let [id_path, cert_path, key_path] = Self::files(data_dir);
        let id_str = read_named(&id_path).map(|b| String::from_utf8_lossy(&b).into_owned())?;
        let device_id = DeviceId(
            uuid::Uuid::parse_str(id_str.trim())
                .map_err(|e| AgentError::CertGen(format!("invalid device ID: {e}")))?,
        );
        Ok(Self {
            device_id,
            cert_der: read_named(&cert_path)?,
            key_der: read_named(&key_path)?,
        })
    }

    /// Builds an identity from an issued certificate and its key, taking the device number from
    /// the certificate; a certificate for another key or without a UUID number is refused.
    pub fn from_issued(cert_der: Vec<u8>, key_der: Vec<u8>) -> Result<Self, AgentError> {
        let device_id = certificate_device_id(&cert_der)?;
        let key = KeyPair::try_from(key_der.as_slice())
            .map_err(|e| AgentError::CertGen(format!("parse key: {e}")))?;
        if certificate_public_key(&cert_der)? != key.public_key_raw() {
            return Err(AgentError::CertGen(
                "the certificate was issued for another key".into(),
            ));
        }
        Ok(Self {
            device_id,
            cert_der,
            key_der,
        })
    }

    /// Adds this identity's three files to a crash-safe save into `data_dir`.
    pub fn staged(&self, data_dir: &Path, commit: Commit) -> Commit {
        let [id_path, cert_path, key_path] = Self::files(data_dir);
        commit
            .write(id_path, self.device_id.0.to_string())
            .write(cert_path, self.cert_der.clone())
            .write_private(key_path, self.key_der.clone())
    }
}

fn read_named(path: &Path) -> Result<Vec<u8>, AgentError> {
    std::fs::read(path).map_err(|e| {
        AgentError::Io(std::io::Error::new(
            e.kind(),
            format!("{}: {e}", path.display()),
        ))
    })
}

/// The device number in a certificate's CommonName.
pub fn certificate_device_id(cert_der: &[u8]) -> Result<DeviceId, AgentError> {
    let (_, cert) = x509_parser::parse_x509_certificate(cert_der)
        .map_err(|e| AgentError::CertGen(format!("parse certificate: {e}")))?;
    let cn = cert
        .subject()
        .iter_common_name()
        .next()
        .and_then(|cn| cn.as_str().ok())
        .ok_or_else(|| AgentError::CertGen("certificate names no device".into()))?;
    uuid::Uuid::parse_str(cn)
        .map(DeviceId)
        .map_err(|e| AgentError::CertGen(format!("certificate device number {cn:?}: {e}")))
}

fn certificate_public_key(cert_der: &[u8]) -> Result<Vec<u8>, AgentError> {
    let (_, cert) = x509_parser::parse_x509_certificate(cert_der)
        .map_err(|e| AgentError::CertGen(format!("parse certificate: {e}")))?;
    Ok(cert.public_key().subject_public_key.data.to_vec())
}

/// A key and a certificate request for it, held in memory until the issued certificate is saved.
pub struct PendingIdentity {
    /// DER-encoded PKCS#8 private key.
    pub key_der: Vec<u8>,
    /// DER-encoded PKCS#10 certificate signing request.
    pub csr_der: Vec<u8>,
    /// PEM-encoded PKCS#10 certificate signing request.
    pub csr_pem: String,
}

impl PendingIdentity {
    /// Generates a P-256 key and a request for it; `device_id` names the machine in a renewal and
    /// is absent at enrollment, where the server chooses the number.
    pub fn generate(device_id: Option<&DeviceId>) -> Result<Self, AgentError> {
        let key_pair = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256)
            .map_err(|e| AgentError::CertGen(format!("generate key: {e}")))?;
        let mut params = CertificateParams::new(Vec::<String>::new())
            .map_err(|e| AgentError::CertGen(format!("cert params: {e}")))?;
        params.distinguished_name = rcgen::DistinguishedName::new();
        if let Some(id) = device_id {
            params.distinguished_name.push(
                rcgen::DnType::CommonName,
                rcgen::DnValue::Utf8String(id.0.to_string()),
            );
        }
        let csr = params
            .serialize_request(&key_pair)
            .map_err(|e| AgentError::CertGen(format!("create CSR: {e}")))?;
        Ok(Self {
            key_der: key_pair.serialize_der(),
            csr_der: csr.der().to_vec(),
            csr_pem: csr
                .pem()
                .map_err(|e| AgentError::CertGen(format!("PEM encode CSR: {e}")))?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::identity_store;
    use x509_parser::prelude::FromDer;

    fn issued(cn: &str, key: &KeyPair) -> Vec<u8> {
        let mut params = CertificateParams::new(Vec::<String>::new()).unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, cn.to_string());
        params.self_signed(key).unwrap().der().to_vec()
    }

    #[test]
    fn load_refuses_a_missing_file_and_creates_nothing() {
        for missing in [DEVICE_ID_FILE, CERT_FILE, KEY_FILE] {
            let dir = tempfile::tempdir().unwrap();
            for name in [DEVICE_ID_FILE, CERT_FILE, KEY_FILE] {
                if name != missing {
                    std::fs::write(dir.path().join(name), uuid::Uuid::new_v4().to_string())
                        .unwrap();
                }
            }
            assert!(AgentIdentity::load(dir.path()).is_err(), "{missing} absent");
            assert!(!AgentIdentity::exists(dir.path()));
            assert!(!dir.path().join(missing).exists(), "{missing} was minted");
        }
    }

    #[test]
    fn issued_identity_takes_the_certificates_device_number() {
        let key = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let number = uuid::Uuid::new_v4();
        let identity =
            AgentIdentity::from_issued(issued(&number.to_string(), &key), key.serialize_der())
                .unwrap();
        assert_eq!(identity.device_id.0, number);
    }

    #[test]
    fn issued_identity_refuses_another_keys_certificate() {
        let ours = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let theirs = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let cert = issued(&uuid::Uuid::new_v4().to_string(), &theirs);
        assert!(AgentIdentity::from_issued(cert, ours.serialize_der()).is_err());
    }

    #[test]
    fn issued_identity_refuses_a_certificate_without_a_device_number() {
        let key = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        assert!(
            AgentIdentity::from_issued(issued("not-a-uuid", &key), key.serialize_der()).is_err()
        );
    }

    #[test]
    fn saved_identity_loads_back() {
        let dir = tempfile::tempdir().unwrap();
        let key = KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let cert = issued(&uuid::Uuid::new_v4().to_string(), &key);
        let identity = AgentIdentity::from_issued(cert, key.serialize_der()).unwrap();
        identity_store::commit(dir.path(), identity.staged(dir.path(), Commit::new())).unwrap();

        let loaded = AgentIdentity::load(dir.path()).unwrap();
        assert_eq!(loaded.device_id.0, identity.device_id.0);
        assert_eq!(loaded.cert_der, identity.cert_der);
        assert_eq!(loaded.key_der, identity.key_der);
    }

    #[test]
    fn enrollment_request_names_no_device_and_renewal_request_names_its_own() {
        let enroll = PendingIdentity::generate(None).unwrap();
        assert!(enroll.csr_pem.contains("BEGIN CERTIFICATE REQUEST"));
        let (_, csr) =
            x509_parser::certification_request::X509CertificationRequest::from_der(&enroll.csr_der)
                .unwrap();
        assert_eq!(
            csr.certification_request_info
                .subject
                .iter_common_name()
                .count(),
            0
        );

        let id = DeviceId(uuid::Uuid::new_v4());
        let renew = PendingIdentity::generate(Some(&id)).unwrap();
        let (_, csr) =
            x509_parser::certification_request::X509CertificationRequest::from_der(&renew.csr_der)
                .unwrap();
        let cn = csr
            .certification_request_info
            .subject
            .iter_common_name()
            .next()
            .unwrap()
            .as_str()
            .unwrap()
            .to_string();
        assert_eq!(cn, id.0.to_string());
    }
}
