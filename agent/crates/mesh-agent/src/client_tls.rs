//! The agent's TLS client: its identity and trusted master keys, built anew whenever either changes
//! so the next connection presents the current certificate with a fresh session cache.

use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

use anyhow::{Context, Result};
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::client::WebPkiServerVerifier;
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{DigitallySignedStruct, SignatureScheme};

/// QUIC idle timeout, matching the server's transport configuration.
const IDLE_TIMEOUT_SECS: u64 = 90;
/// QUIC keep-alive period, matching the server's transport configuration.
const KEEP_ALIVE_SECS: u64 = 30;

/// A QUIC client configuration plus the verifier that remembers why it refused a server.
pub(crate) struct ClientTls {
    config: quinn::ClientConfig,
    rustls: Arc<rustls::ClientConfig>,
    verifier: Arc<RecordingVerifier>,
}

impl ClientTls {
    /// Builds the client from the agent's identity and the PEM bundle of master keys it trusts.
    pub(crate) fn load(identity: &mesh_agent_core::AgentIdentity, ca_pem: &str) -> Result<Self> {
        let roots = root_store(ca_pem)?;
        let verifier = Arc::new(RecordingVerifier {
            inner: WebPkiServerVerifier::builder(Arc::new(roots))
                .build()
                .context("build server verifier")?,
            refusal: Mutex::new(None),
        });

        let client_cert = CertificateDer::from(identity.cert_der.clone());
        let client_key = rustls::pki_types::PrivateKeyDer::Pkcs8(
            rustls::pki_types::PrivatePkcs8KeyDer::from(identity.key_der.clone()),
        );
        let mut tls = rustls::ClientConfig::builder()
            .dangerous()
            .with_custom_certificate_verifier(verifier.clone())
            .with_client_auth_cert(vec![client_cert], client_key)
            .context("build TLS config")?;
        tls.alpn_protocols = vec![b"opengate".to_vec()];
        let rustls = Arc::new(tls);

        let mut config = quinn::ClientConfig::new(Arc::new(
            quinn::crypto::rustls::QuicClientConfig::try_from(rustls.clone())?,
        ));
        let mut transport = quinn::TransportConfig::default();
        transport.max_idle_timeout(Some(
            std::time::Duration::from_secs(IDLE_TIMEOUT_SECS)
                .try_into()
                .context("idle timeout")?,
        ));
        transport.keep_alive_interval(Some(std::time::Duration::from_secs(KEEP_ALIVE_SECS)));
        config.transport_config(Arc::new(transport));

        Ok(Self {
            config,
            rustls,
            verifier,
        })
    }

    /// The certificate this client presents, as the server will see it.
    pub(crate) fn presented_certificate(&self) -> Option<Vec<u8>> {
        self.rustls
            .client_auth_cert_resolver
            .resolve(&[], &[SignatureScheme::ECDSA_NISTP256_SHA256])
            .and_then(|key| key.cert.first().map(|c| c.to_vec()))
    }

    /// Why the last connection refused the server's chain, cleared by reading it.
    pub(crate) fn take_chain_refusal(&self) -> Option<String> {
        self.verifier.refusal.lock().ok().and_then(|mut r| r.take())
    }
}

/// Every certificate in a PEM bundle; an empty bundle or a block that fails to parse is refused.
pub(crate) fn parse_bundle(pem: &str) -> Result<Vec<CertificateDer<'static>>> {
    let certs = rustls_pemfile::certs(&mut pem.as_bytes())
        .collect::<Result<Vec<_>, _>>()
        .context("parse master-key bundle")?;
    anyhow::ensure!(
        !certs.is_empty(),
        "the master-key bundle holds no certificate"
    );
    Ok(certs)
}

fn root_store(ca_pem: &str) -> Result<rustls::RootCertStore> {
    let mut roots = rustls::RootCertStore::empty();
    for cert in parse_bundle(ca_pem)? {
        roots.add(cert).context("add master key")?;
    }
    Ok(roots)
}

/// Whether `bundle_pem` verifies the chain the server presented on the current connection.
pub(crate) fn bundle_verifies_chain(
    bundle_pem: &str,
    chain: &[CertificateDer<'static>],
    server_name: &ServerName<'static>,
) -> bool {
    let Some((leaf, intermediates)) = chain.split_first() else {
        return false;
    };
    let Ok(roots) = root_store(bundle_pem) else {
        return false;
    };
    let Ok(verifier) = WebPkiServerVerifier::builder(Arc::new(roots)).build() else {
        return false;
    };
    verifier
        .verify_server_cert(leaf, intermediates, server_name, &[], UnixTime::now())
        .is_ok()
}

/// Whether `cert_der` names any certificate of `bundle_pem` as its issuer, by subject.
pub(crate) fn issued_by_bundle(cert_der: &[u8], bundle_pem: &str) -> bool {
    let Ok((_, cert)) = x509_parser::parse_x509_certificate(cert_der) else {
        return false;
    };
    let Ok(bundle) = parse_bundle(bundle_pem) else {
        return false;
    };
    bundle.iter().any(|ca| {
        x509_parser::parse_x509_certificate(ca)
            .map(|(_, ca)| ca.subject().as_raw() == cert.issuer().as_raw())
            .unwrap_or(false)
    })
}

/// Whether two PEM bundles share a master key.
pub(crate) fn bundles_overlap(a: &str, b: &str) -> bool {
    match (parse_bundle(a), parse_bundle(b)) {
        (Ok(a), Ok(b)) => a.iter().any(|cert| b.contains(cert)),
        _ => false,
    }
}

/// The server-certificate verifier the bundle defines, which keeps the reason for a refusal so the
/// agent can tell an unknown master key from an unreachable server.
#[derive(Debug)]
struct RecordingVerifier {
    inner: Arc<WebPkiServerVerifier>,
    refusal: Mutex<Option<String>>,
}

impl ServerCertVerifier for RecordingVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        intermediates: &[CertificateDer<'_>],
        server_name: &ServerName<'_>,
        ocsp_response: &[u8],
        now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        let verdict = self.inner.verify_server_cert(
            end_entity,
            intermediates,
            server_name,
            ocsp_response,
            now,
        );
        if let Err(e) = &verdict {
            if let Ok(mut refusal) = self.refusal.lock() {
                *refusal = Some(e.to_string());
            }
        }
        verdict
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        self.inner.verify_tls12_signature(message, cert, dss)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        self.inner.verify_tls13_signature(message, cert, dss)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.inner.supported_verify_schemes()
    }
}

/// An established connection and the control stream the agent opened on it.
pub(crate) struct Connected {
    pub(crate) link: Link,
    pub(crate) send: quinn::SendStream,
    pub(crate) recv: quinn::RecvStream,
}

/// An established connection and the name the agent verified the server under.
pub(crate) struct Link {
    pub(crate) conn: quinn::Connection,
    pub(crate) server_name: ServerName<'static>,
}

impl Link {
    /// The certificate chain the server presented.
    pub(crate) fn server_chain(&self) -> Vec<CertificateDer<'static>> {
        self.conn
            .peer_identity()
            .and_then(|any| any.downcast::<Vec<CertificateDer<'static>>>().ok())
            .map(|chain| *chain)
            .unwrap_or_default()
    }
}

/// Why a connection attempt failed, as far as the agent's next decision cares.
#[derive(Debug)]
pub(crate) enum ConnectFailure {
    /// The agent refused the server's chain: none of its master keys signed it.
    ChainRefused(String),
    /// The server refused the agent's certificate during the TLS handshake.
    CertificateRefused(String),
    /// Anything else: an unreachable server, a timeout, a name that does not resolve.
    Unreachable(String),
}

impl std::fmt::Display for ConnectFailure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::ChainRefused(why) => write!(f, "server chain refused: {why}"),
            Self::CertificateRefused(why) => write!(f, "certificate refused: {why}"),
            Self::Unreachable(why) => write!(f, "{why}"),
        }
    }
}

/// TLS alerts a server sends for a client certificate it will not accept.
const CERTIFICATE_ALERTS: [u8; 7] = [42, 43, 44, 45, 46, 48, 116];
/// QUIC carries a TLS alert as a transport error code of 0x100 plus the alert.
const CRYPTO_ERROR_BASE: u64 = 0x100;

/// Connects to `server_addr` and opens the control stream the agent writes first on.
pub(crate) async fn connect(
    endpoint: &quinn::Endpoint,
    tls: &ClientTls,
    server_addr: &str,
) -> Result<Connected, ConnectFailure> {
    // The host part of the address is the TLS server name.
    let sni_host = server_addr
        .rsplit_once(':')
        .map(|(h, _)| h)
        .unwrap_or(server_addr);
    let server_name = ServerName::try_from(sni_host.to_string())
        .map_err(|e| ConnectFailure::Unreachable(format!("server name {sni_host}: {e}")))?;

    let addr: SocketAddr = match server_addr.parse() {
        Ok(a) => a,
        Err(_) => tokio::net::lookup_host(server_addr)
            .await
            .map_err(|e| ConnectFailure::Unreachable(format!("resolve server addr: {e}")))?
            .next()
            .ok_or_else(|| {
                ConnectFailure::Unreachable(format!("no addresses found for {server_addr}"))
            })?,
    };
    let connecting = endpoint
        .connect_with(tls.config.clone(), addr, sni_host)
        .map_err(|e| ConnectFailure::Unreachable(format!("QUIC connect: {e}")))?;
    let conn = match connecting.await {
        Ok(conn) => conn,
        Err(e) => return Err(classify_connect_error(tls, e)),
    };
    let (send, recv) = conn
        .open_bi()
        .await
        .map_err(|e| ConnectFailure::Unreachable(format!("open bi stream: {e}")))?;
    Ok(Connected {
        link: Link { conn, server_name },
        send,
        recv,
    })
}

fn classify_connect_error(tls: &ClientTls, e: quinn::ConnectionError) -> ConnectFailure {
    if let Some(why) = tls.take_chain_refusal() {
        return ConnectFailure::ChainRefused(why);
    }
    if let quinn::ConnectionError::ConnectionClosed(close) = &e {
        let code = u64::from(close.error_code);
        let alert = code
            .checked_sub(CRYPTO_ERROR_BASE)
            .and_then(|a| u8::try_from(a).ok());
        if alert.is_some_and(|a| CERTIFICATE_ALERTS.contains(&a)) {
            return ConnectFailure::CertificateRefused(e.to_string());
        }
    }
    ConnectFailure::Unreachable(format!("QUIC establish: {e}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ca(name: &str) -> (rcgen::Issuer<'static, rcgen::KeyPair>, String, Vec<u8>) {
        let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, name.to_string());
        let cert = params.clone().self_signed(&key).unwrap();
        (
            rcgen::Issuer::new(params, key),
            cert.pem(),
            cert.der().to_vec(),
        )
    }

    fn leaf(issuer: &rcgen::Issuer<'static, rcgen::KeyPair>, sans: Vec<String>) -> Vec<u8> {
        let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let params = rcgen::CertificateParams::new(sans).unwrap();
        params.signed_by(&key, issuer).unwrap().der().to_vec()
    }

    #[test]
    fn a_bundle_with_one_bad_block_is_refused() {
        let (_, pem, _) = ca("good");
        let bad = format!("{pem}-----BEGIN CERTIFICATE-----\n!!!!\n-----END CERTIFICATE-----\n");
        assert!(parse_bundle(&pem).is_ok());
        assert!(parse_bundle(&bad).is_err());
        assert!(parse_bundle("").is_err());
    }

    #[test]
    fn only_the_signing_master_key_verifies_the_servers_chain() {
        let (issuer, pem, _) = ca("ours");
        let (_, other_pem, _) = ca("theirs");
        let chain = vec![CertificateDer::from(leaf(
            &issuer,
            vec!["127.0.0.1".into()],
        ))];
        let name = ServerName::try_from("127.0.0.1".to_string()).unwrap();

        assert!(bundle_verifies_chain(&pem, &chain, &name));
        assert!(!bundle_verifies_chain(&other_pem, &chain, &name));
        assert!(bundle_verifies_chain(
            &format!("{other_pem}{pem}"),
            &chain,
            &name
        ));
    }

    /// A changeover: A the old master key, B the new, B's own key certified by A, and a server
    /// certificate from B presented with that cross certificate.
    fn changeover(a_expired: bool) -> (String, String, Vec<CertificateDer<'static>>) {
        let a_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut a_params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        a_params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        a_params
            .distinguished_name
            .push(rcgen::DnType::CommonName, "A".to_string());
        if a_expired {
            a_params.not_before = rcgen::date_time_ymd(2020, 1, 1);
            a_params.not_after = rcgen::date_time_ymd(2021, 1, 1);
        }
        let a_cert = a_params.clone().self_signed(&a_key).unwrap();
        let a = rcgen::Issuer::new(a_params, a_key);

        let b_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut b_params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        b_params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        b_params
            .distinguished_name
            .push(rcgen::DnType::CommonName, "B".to_string());
        let b_cert = b_params.clone().self_signed(&b_key).unwrap();
        let cross = b_params.clone().signed_by(&b_key, &a).unwrap();
        let b = rcgen::Issuer::new(b_params, b_key);

        let chain = vec![
            CertificateDer::from(leaf(&b, vec!["127.0.0.1".into()])),
            CertificateDer::from(cross.der().to_vec()),
        ];
        (a_cert.pem(), b_cert.pem(), chain)
    }

    fn server() -> ServerName<'static> {
        ServerName::try_from("127.0.0.1".to_string()).unwrap()
    }

    #[test]
    fn trusting_only_old_ca_verifies_cross_chain() {
        let (a, _, chain) = changeover(false);
        assert!(bundle_verifies_chain(&a, &chain, &server()));
    }

    #[test]
    fn trusting_only_new_ca_verifies_cross_chain() {
        let (_, b, chain) = changeover(false);
        assert!(bundle_verifies_chain(&b, &chain, &server()));
    }

    #[test]
    fn expired_old_anchor_still_verifies_cross_chain() {
        let (a, _, chain) = changeover(true);
        assert!(
            bundle_verifies_chain(&a, &chain, &server()),
            "a trust anchor carries no validity period, so an expired one still anchors"
        );
    }

    #[test]
    fn issuer_and_overlap_are_read_from_the_bundle() {
        let (issuer, pem, _) = ca("ours");
        let (_, other_pem, _) = ca("theirs");
        let cert = leaf(&issuer, Vec::new());

        assert!(issued_by_bundle(&cert, &pem));
        assert!(!issued_by_bundle(&cert, &other_pem));
        assert!(bundles_overlap(&pem, &format!("{other_pem}{pem}")));
        assert!(!bundles_overlap(&pem, &other_pem));
    }
}
