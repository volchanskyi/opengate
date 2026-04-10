//! Shared test helpers for mesh-agent integration tests.

use std::sync::Arc;

use mesh_protocol::HandshakeMessage;
use sha2::{Digest, Sha384};

/// Certificate set for mTLS testing.
#[allow(dead_code)] // Rust integration tests compile each file as a separate crate — fields unused by some binaries trigger dead_code
pub struct TestCerts {
    pub ca_pem: String,
    pub ca_cert_der: Vec<u8>,
    pub server_cert_der: Vec<u8>,
    pub server_key_der: Vec<u8>,
    pub agent_cert_der: Vec<u8>,
    pub agent_key_der: Vec<u8>,
}

/// Generate CA, server cert, and agent cert for mTLS testing.
pub fn generate_test_certs() -> TestCerts {
    // CA
    let ca_key =
        rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).expect("generate CA keypair");
    let mut ca_params =
        rcgen::CertificateParams::new(Vec::<String>::new()).expect("create CA cert params");
    ca_params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
    ca_params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String("Test CA".to_string()),
    );
    let ca_params_for_issuer = ca_params.clone();
    let ca_cert = ca_params.self_signed(&ca_key).expect("self-sign CA cert");
    let issuer = rcgen::Issuer::new(ca_params_for_issuer, &ca_key);

    // Server cert signed by CA (SAN: "server" for TLS verification)
    let server_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256)
        .expect("generate server keypair");
    let mut server_params = rcgen::CertificateParams::new(vec!["server".to_string()])
        .expect("create server cert params");
    server_params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String("server".to_string()),
    );
    let server_cert = server_params
        .signed_by(&server_key, &issuer)
        .expect("sign server cert with CA");

    // Agent cert signed by CA
    let agent_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256)
        .expect("generate agent keypair");
    let mut agent_params =
        rcgen::CertificateParams::new(Vec::<String>::new()).expect("create agent cert params");
    agent_params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String("test-agent-id".to_string()),
    );
    let agent_cert = agent_params
        .signed_by(&agent_key, &issuer)
        .expect("sign agent cert with CA");

    TestCerts {
        ca_pem: ca_cert.pem(),
        ca_cert_der: ca_cert.der().to_vec(),
        server_cert_der: server_cert.der().to_vec(),
        server_key_der: server_key.serialize_der(),
        agent_cert_der: agent_cert.der().to_vec(),
        agent_key_der: agent_key.serialize_der(),
    }
}

/// Build quinn server endpoint with mTLS.
pub fn build_server_endpoint(certs: &TestCerts) -> (quinn::Endpoint, std::net::SocketAddr) {
    let ca_cert_der = rustls::pki_types::CertificateDer::from(certs.ca_cert_der.clone());
    let mut root_store = rustls::RootCertStore::empty();
    root_store.add(ca_cert_der).expect("add CA to root store");

    let client_verifier = rustls::server::WebPkiClientVerifier::builder(Arc::new(root_store))
        .build()
        .expect("build client cert verifier");

    let server_cert_der = rustls::pki_types::CertificateDer::from(certs.server_cert_der.clone());
    let server_key_der = rustls::pki_types::PrivateKeyDer::Pkcs8(
        rustls::pki_types::PrivatePkcs8KeyDer::from(certs.server_key_der.clone()),
    );

    let mut server_tls = rustls::ServerConfig::builder()
        .with_client_cert_verifier(client_verifier)
        .with_single_cert(vec![server_cert_der], server_key_der)
        .expect("build server TLS config");
    server_tls.alpn_protocols = vec![b"opengate".to_vec()];

    let server_crypto = quinn::crypto::rustls::QuicServerConfig::try_from(server_tls)
        .expect("create QUIC server crypto config");
    let server_config = quinn::ServerConfig::with_crypto(Arc::new(server_crypto));

    let endpoint = quinn::Endpoint::server(
        server_config,
        "127.0.0.1:0".parse().expect("parse server bind address"),
    )
    .expect("bind server endpoint");
    let addr = endpoint.local_addr().expect("get server local address");
    (endpoint, addr)
}

/// Build quinn client config with mTLS.
pub fn build_client_config(certs: &TestCerts) -> quinn::ClientConfig {
    let ca_cert_der = rustls::pki_types::CertificateDer::from(certs.ca_cert_der.clone());
    let mut root_store = rustls::RootCertStore::empty();
    root_store.add(ca_cert_der).expect("add CA to root store");

    let client_cert = rustls::pki_types::CertificateDer::from(certs.agent_cert_der.clone());
    let client_key = rustls::pki_types::PrivateKeyDer::Pkcs8(
        rustls::pki_types::PrivatePkcs8KeyDer::from(certs.agent_key_der.clone()),
    );

    let mut tls_config = rustls::ClientConfig::builder()
        .with_root_certificates(root_store)
        .with_client_auth_cert(vec![client_cert], client_key)
        .expect("build client TLS config with cert auth");
    tls_config.alpn_protocols = vec![b"opengate".to_vec()];

    let quinn_crypto = quinn::crypto::rustls::QuicClientConfig::try_from(tls_config)
        .expect("create QUIC client crypto config");
    quinn::ClientConfig::new(Arc::new(quinn_crypto))
}

/// Perform server-side handshake on opened streams.
pub async fn server_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    ca_cert_der: &[u8],
) {
    let ca_cert_hash: [u8; 48] = Sha384::digest(ca_cert_der).into();
    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).expect("fill nonce with random bytes");
    let server_hello = HandshakeMessage::ServerHello {
        nonce,
        cert_hash: ca_cert_hash,
    };
    send.write_all(&server_hello.encode_binary())
        .await
        .expect("send ServerHello");

    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf)
        .await
        .expect("receive AgentHello");
    let agent_hello =
        HandshakeMessage::decode_binary(&hello_buf).expect("decode AgentHello message");
    assert!(
        matches!(agent_hello, HandshakeMessage::AgentHello { .. }),
        "expected AgentHello, got {:?}",
        agent_hello
    );
}

/// Perform client-side handshake on accepted streams.
pub async fn client_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    agent_cert_der: &[u8],
) {
    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf)
        .await
        .expect("receive ServerHello");
    let server_hello =
        HandshakeMessage::decode_binary(&hello_buf).expect("decode ServerHello message");
    assert!(
        matches!(server_hello, HandshakeMessage::ServerHello { .. }),
        "expected ServerHello, got {:?}",
        server_hello
    );

    let agent_cert_hash: [u8; 48] = Sha384::digest(agent_cert_der).into();
    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).expect("fill nonce with random bytes");
    let agent_hello = HandshakeMessage::AgentHello {
        nonce,
        agent_cert_hash,
    };
    send.write_all(&agent_hello.encode_binary())
        .await
        .expect("send AgentHello");
}
