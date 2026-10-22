//! The agent's side of renewal over the live connection: a request from a key held in memory, and
//! an issued certificate checked against that key and the current server before it is saved.

use std::time::{Duration, Instant};

use anyhow::Context;
use mesh_agent_core::identity_store::{self, Commit};
use mesh_agent_core::{AgentIdentity, PendingIdentity};
use mesh_protocol::{ControlMessage, DeviceId};
use rustls::pki_types::{CertificateDer, ServerName};

use crate::client_tls::{self, ClientTls};
use crate::lifecycle::Paths;

/// How long a request waits for the server's answer.
pub(crate) const ANSWER_WAIT: Duration = Duration::from_secs(60);

/// A renewal in progress: the key the request was made for, and when.
pub(crate) struct Renewal {
    pending: Option<Pending>,
}

struct Pending {
    key_der: Vec<u8>,
    asked_at: Instant,
}

/// Why an issued certificate was not installed.
#[derive(Debug, PartialEq, Eq)]
pub(crate) enum Rejected {
    /// No request was outstanding.
    NotAsked,
    /// The answer came after the request's wait.
    TooLate,
    /// The certificate is for another key or another device.
    WrongIdentity,
    /// A block of the trust bundle does not parse.
    BadBundle,
    /// The bundle does not verify the server of the current connection.
    ServerNotVerified,
    /// The new identity and bundle do not build a TLS client.
    Tls,
}

/// A renewal ready to save and use: the new identity, its trust bundle, and the TLS client built
/// from both.
pub(crate) struct Installed {
    pub(crate) identity: AgentIdentity,
    pub(crate) bundle: String,
    pub(crate) tls: ClientTls,
}

/// What the current connection proved about the server, which a new bundle must also verify.
pub(crate) struct ServerSeen<'a> {
    pub(crate) chain: &'a [CertificateDer<'static>],
    pub(crate) name: &'a ServerName<'static>,
}

impl Renewal {
    pub(crate) fn new() -> Self {
        Self { pending: None }
    }

    /// Generates a P-256 key held in memory and the request naming this device that asks for it.
    pub(crate) fn request(
        &mut self,
        device: &DeviceId,
        now: Instant,
    ) -> anyhow::Result<ControlMessage> {
        let pending = PendingIdentity::generate(Some(device)).context("generate renewal key")?;
        self.pending = Some(Pending {
            key_der: pending.key_der,
            asked_at: now,
        });
        Ok(ControlMessage::CertificateSigningRequest {
            csr: pending.csr_der,
        })
    }

    /// Checks an issued certificate in memory: it matches the pending key and this device, every
    /// bundle block parses, the bundle verifies the current server, and a TLS client builds.
    pub(crate) fn issued(
        &mut self,
        device: &DeviceId,
        cert: Vec<u8>,
        bundle: &str,
        server: ServerSeen<'_>,
        now: Instant,
    ) -> Result<Installed, Rejected> {
        let pending = self.pending.take().ok_or(Rejected::NotAsked)?;
        if now.duration_since(pending.asked_at) > ANSWER_WAIT {
            return Err(Rejected::TooLate);
        }
        let identity = AgentIdentity::from_issued(cert, pending.key_der)
            .map_err(|_| Rejected::WrongIdentity)?;
        if identity.device_id != *device {
            return Err(Rejected::WrongIdentity);
        }
        client_tls::parse_bundle(bundle).map_err(|_| Rejected::BadBundle)?;
        if !client_tls::bundle_verifies_chain(bundle, server.chain, server.name) {
            return Err(Rejected::ServerNotVerified);
        }
        let tls = ClientTls::load(&identity, bundle).map_err(|_| Rejected::Tls)?;
        Ok(Installed {
            identity,
            bundle: bundle.to_string(),
            tls,
        })
    }

    /// Forgets the outstanding request, which the server refused.
    pub(crate) fn refused(&mut self) {
        self.pending = None;
    }
}

/// Saves a renewed identity and its trust bundle in one crash-safe commit.
pub(crate) fn save(installed: &Installed, paths: &Paths) -> anyhow::Result<()> {
    let commit = installed.identity.staged(
        &paths.data_dir,
        Commit::new().write(&paths.server_ca, installed.bundle.clone()),
    );
    identity_store::commit(&paths.data_dir, commit).context("save the renewed identity")
}

#[cfg(test)]
mod tests {
    use super::*;
    use x509_parser::prelude::FromDer;

    struct Pki {
        issuer: rcgen::Issuer<'static, rcgen::KeyPair>,
        pem: String,
        server_chain: Vec<CertificateDer<'static>>,
    }

    fn pki(name: &str) -> Pki {
        let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, name.to_string());
        let cert = params.clone().self_signed(&key).unwrap();
        let issuer = rcgen::Issuer::new(params, key);
        let server_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let server = rcgen::CertificateParams::new(vec!["127.0.0.1".into()])
            .unwrap()
            .signed_by(&server_key, &issuer)
            .unwrap();
        Pki {
            issuer,
            pem: cert.pem(),
            server_chain: vec![CertificateDer::from(server.der().to_vec())],
        }
    }

    /// The P-256 public key a request carries, as rcgen signs for it.
    struct RequestedKey(Vec<u8>);

    impl rcgen::PublicKeyData for RequestedKey {
        fn der_bytes(&self) -> &[u8] {
            &self.0
        }

        fn algorithm(&self) -> &'static rcgen::SignatureAlgorithm {
            &rcgen::PKCS_ECDSA_P256_SHA256
        }
    }

    fn sign(pki: &Pki, csr: &[u8]) -> Vec<u8> {
        let (_, parsed) =
            x509_parser::certification_request::X509CertificationRequest::from_der(csr).unwrap();
        let info = &parsed.certification_request_info;
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        let cn = info.subject.iter_common_name().next().unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, cn.as_str().unwrap().to_string());
        let key = RequestedKey(info.subject_pki.subject_public_key.data.to_vec());
        params.signed_by(&key, &pki.issuer).unwrap().der().to_vec()
    }

    fn requested(renewal: &mut Renewal, device: &DeviceId, now: Instant) -> Vec<u8> {
        match renewal.request(device, now).unwrap() {
            ControlMessage::CertificateSigningRequest { csr } => csr,
            other => panic!("expected a request, got {other:?}"),
        }
    }

    fn name() -> ServerName<'static> {
        ServerName::try_from("127.0.0.1".to_string()).unwrap()
    }

    #[test]
    fn renew_request_yields_p256_csr_with_device_cn() {
        let device = DeviceId(uuid::Uuid::new_v4());
        let csr = requested(&mut Renewal::new(), &device, Instant::now());
        let (_, parsed) =
            x509_parser::certification_request::X509CertificationRequest::from_der(&csr).unwrap();
        let cn = parsed
            .certification_request_info
            .subject
            .iter_common_name()
            .next()
            .unwrap()
            .as_str()
            .unwrap()
            .to_string();
        assert_eq!(cn, device.0.to_string());
        assert_eq!(
            parsed
                .certification_request_info
                .subject_pki
                .algorithm
                .parameters
                .as_ref()
                .and_then(|p| p.as_oid().ok())
                .map(|o| o.to_id_string()),
            Some("1.2.840.10045.3.1.7".to_string()),
            "the key is P-256"
        );
    }

    #[test]
    fn issued_for_other_key_refused() {
        let ca = pki("ours");
        let device = DeviceId(uuid::Uuid::new_v4());
        let mut renewal = Renewal::new();
        requested(&mut renewal, &device, Instant::now());
        let elsewhere = requested(&mut Renewal::new(), &device, Instant::now());
        let cert = sign(&ca, &elsewhere);
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        assert_eq!(
            renewal
                .issued(&device, cert, &ca.pem, server, Instant::now())
                .err(),
            Some(Rejected::WrongIdentity)
        );
    }

    #[test]
    fn issued_without_pending_ignored() {
        let ca = pki("ours");
        let device = DeviceId(uuid::Uuid::new_v4());
        let cert = sign(
            &ca,
            &requested(&mut Renewal::new(), &device, Instant::now()),
        );
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        assert_eq!(
            Renewal::new()
                .issued(&device, cert, &ca.pem, server, Instant::now())
                .err(),
            Some(Rejected::NotAsked)
        );
    }

    #[test]
    fn issued_after_deadline_ignored() {
        let ca = pki("ours");
        let device = DeviceId(uuid::Uuid::new_v4());
        let mut renewal = Renewal::new();
        let asked = Instant::now();
        let cert = sign(&ca, &requested(&mut renewal, &device, asked));
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        let late = asked + ANSWER_WAIT + Duration::from_secs(1);
        assert_eq!(
            renewal.issued(&device, cert, &ca.pem, server, late).err(),
            Some(Rejected::TooLate)
        );
    }

    #[test]
    fn bundle_not_verifying_current_server_refused() {
        let ca = pki("ours");
        let other = pki("theirs");
        let device = DeviceId(uuid::Uuid::new_v4());
        let mut renewal = Renewal::new();
        let cert = sign(&ca, &requested(&mut renewal, &device, Instant::now()));
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        assert_eq!(
            renewal
                .issued(&device, cert, &other.pem, server, Instant::now())
                .err(),
            Some(Rejected::ServerNotVerified)
        );
    }

    #[test]
    fn unparsable_bundle_block_refused() {
        let ca = pki("ours");
        let device = DeviceId(uuid::Uuid::new_v4());
        let mut renewal = Renewal::new();
        let cert = sign(&ca, &requested(&mut renewal, &device, Instant::now()));
        let bundle = format!(
            "{}-----BEGIN CERTIFICATE-----\n!!\n-----END CERTIFICATE-----\n",
            ca.pem
        );
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        assert_eq!(
            renewal
                .issued(&device, cert, &bundle, server, Instant::now())
                .err(),
            Some(Rejected::BadBundle)
        );
    }

    #[test]
    fn install_rebuilds_tls_and_session_cache() {
        let ca = pki("ours");
        let device = DeviceId(uuid::Uuid::new_v4());
        let mut renewal = Renewal::new();
        let cert = sign(&ca, &requested(&mut renewal, &device, Instant::now()));
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        let installed = renewal
            .issued(&device, cert.clone(), &ca.pem, server, Instant::now())
            .unwrap();
        assert_eq!(installed.tls.presented_certificate(), Some(cert));
    }

    #[test]
    fn refused_clears_pending() {
        let ca = pki("ours");
        let device = DeviceId(uuid::Uuid::new_v4());
        let mut renewal = Renewal::new();
        let cert = sign(&ca, &requested(&mut renewal, &device, Instant::now()));
        renewal.refused();
        let server = ServerSeen {
            chain: &ca.server_chain,
            name: &name(),
        };
        assert_eq!(
            renewal
                .issued(&device, cert, &ca.pem, server, Instant::now())
                .err(),
            Some(Rejected::NotAsked)
        );
    }
}
