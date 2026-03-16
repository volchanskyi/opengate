//! Integration test: QUIC mTLS connect + handshake + AgentRegister roundtrip.
//!
//! Exercises the full transport stack: CA → certs → QUIC → handshake → control messages.

use std::sync::Arc;

use mesh_agent_core::{AgentConnection, AsyncControlStream};
use mesh_protocol::{AgentCapability, ControlMessage, HandshakeMessage};
use sha2::{Digest, Sha384};

/// Generate CA, server cert, and agent cert for mTLS testing.
struct TestCerts {
    #[allow(dead_code)] // Available for tests that need PEM format
    ca_pem: String,
    ca_cert_der: Vec<u8>,
    server_cert_der: Vec<u8>,
    server_key_der: Vec<u8>,
    agent_cert_der: Vec<u8>,
    agent_key_der: Vec<u8>,
}

fn generate_test_certs() -> TestCerts {
    // CA
    let ca_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
    let mut ca_params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
    ca_params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
    ca_params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String("Test CA".to_string()),
    );
    // Clone params before self_signed consumes them (needed for Issuer)
    let ca_params_for_issuer = ca_params.clone();
    let ca_cert = ca_params.self_signed(&ca_key).unwrap();
    let issuer = rcgen::Issuer::new(ca_params_for_issuer, &ca_key);

    // Server cert signed by CA (SAN: "server" for TLS verification)
    let server_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
    let mut server_params = rcgen::CertificateParams::new(vec!["server".to_string()]).unwrap();
    server_params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String("server".to_string()),
    );
    let server_cert = server_params.signed_by(&server_key, &issuer).unwrap();

    // Agent cert signed by CA
    let agent_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
    let mut agent_params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
    agent_params.distinguished_name.push(
        rcgen::DnType::CommonName,
        rcgen::DnValue::Utf8String("test-agent-id".to_string()),
    );
    let agent_cert = agent_params.signed_by(&agent_key, &issuer).unwrap();

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
fn build_server_endpoint(certs: &TestCerts) -> (quinn::Endpoint, std::net::SocketAddr) {
    let ca_cert_der = rustls::pki_types::CertificateDer::from(certs.ca_cert_der.clone());
    let mut root_store = rustls::RootCertStore::empty();
    root_store.add(ca_cert_der).unwrap();

    let client_verifier = rustls::server::WebPkiClientVerifier::builder(Arc::new(root_store))
        .build()
        .unwrap();

    let server_cert_der = rustls::pki_types::CertificateDer::from(certs.server_cert_der.clone());
    let server_key_der = rustls::pki_types::PrivateKeyDer::Pkcs8(
        rustls::pki_types::PrivatePkcs8KeyDer::from(certs.server_key_der.clone()),
    );

    let mut server_tls = rustls::ServerConfig::builder()
        .with_client_cert_verifier(client_verifier)
        .with_single_cert(vec![server_cert_der], server_key_der)
        .unwrap();
    server_tls.alpn_protocols = vec![b"opengate".to_vec()];

    let server_crypto = quinn::crypto::rustls::QuicServerConfig::try_from(server_tls).unwrap();
    let server_config = quinn::ServerConfig::with_crypto(Arc::new(server_crypto));

    let endpoint = quinn::Endpoint::server(server_config, "127.0.0.1:0".parse().unwrap()).unwrap();
    let addr = endpoint.local_addr().unwrap();
    (endpoint, addr)
}

/// Build quinn client config with mTLS (same logic as main.rs build_quic_config).
fn build_client_config(certs: &TestCerts) -> quinn::ClientConfig {
    let ca_cert_der = rustls::pki_types::CertificateDer::from(certs.ca_cert_der.clone());
    let mut root_store = rustls::RootCertStore::empty();
    root_store.add(ca_cert_der).unwrap();

    let client_cert = rustls::pki_types::CertificateDer::from(certs.agent_cert_der.clone());
    let client_key = rustls::pki_types::PrivateKeyDer::Pkcs8(
        rustls::pki_types::PrivatePkcs8KeyDer::from(certs.agent_key_der.clone()),
    );

    let mut tls_config = rustls::ClientConfig::builder()
        .with_root_certificates(root_store)
        .with_client_auth_cert(vec![client_cert], client_key)
        .unwrap();
    tls_config.alpn_protocols = vec![b"opengate".to_vec()];

    let quinn_crypto = quinn::crypto::rustls::QuicClientConfig::try_from(tls_config).unwrap();
    quinn::ClientConfig::new(Arc::new(quinn_crypto))
}

/// Helper: perform server-side handshake on opened streams.
async fn server_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    ca_cert_der: &[u8],
) {
    let ca_cert_hash: [u8; 48] = Sha384::digest(ca_cert_der).into();
    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).unwrap();
    let server_hello = HandshakeMessage::ServerHello {
        nonce,
        cert_hash: ca_cert_hash,
    };
    send.write_all(&server_hello.encode_binary()).await.unwrap();

    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf).await.unwrap();
    let agent_hello = HandshakeMessage::decode_binary(&hello_buf).unwrap();
    assert!(
        matches!(agent_hello, HandshakeMessage::AgentHello { .. }),
        "expected AgentHello, got {:?}",
        agent_hello
    );
}

/// Helper: perform client-side handshake on accepted streams.
async fn client_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    agent_cert_der: &[u8],
) {
    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf).await.unwrap();
    let server_hello = HandshakeMessage::decode_binary(&hello_buf).unwrap();
    assert!(
        matches!(server_hello, HandshakeMessage::ServerHello { .. }),
        "expected ServerHello, got {:?}",
        server_hello
    );

    let agent_cert_hash: [u8; 48] = Sha384::digest(agent_cert_der).into();
    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).unwrap();
    let agent_hello = HandshakeMessage::AgentHello {
        nonce,
        agent_cert_hash,
    };
    send.write_all(&agent_hello.encode_binary()).await.unwrap();
}

#[tokio::test]
async fn test_quic_agent_register_roundtrip() {
    let certs = generate_test_certs();
    let (server_endpoint, server_addr) = build_server_endpoint(&certs);
    let client_config = build_client_config(&certs);

    let ca_cert_der = certs.ca_cert_der.clone();
    let agent_cert_der = certs.agent_cert_der.clone();

    // Spawn mock server
    let server_handle = tokio::spawn(async move {
        let incoming = server_endpoint.accept().await.unwrap();
        let conn = incoming.await.unwrap();
        let (mut send, mut recv) = conn.open_bi().await.unwrap();

        server_handshake(&mut send, &mut recv, &ca_cert_der).await;

        let stream = AsyncControlStream::new(tokio::io::join(recv, send));
        let config = mesh_agent_core::AgentConfig {
            server_addr: server_addr.to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp"),
        };
        let mut agent_conn = AgentConnection::new(stream, config);

        let msg = agent_conn.receive_control().await.unwrap();
        match msg {
            ControlMessage::AgentRegister {
                hostname,
                os,
                arch,
                version,
                capabilities,
            } => {
                assert!(!hostname.is_empty());
                assert_eq!(os, std::env::consts::OS);
                assert_eq!(arch, std::env::consts::ARCH);
                assert_eq!(version, env!("CARGO_PKG_VERSION"));
                assert_eq!(capabilities.len(), 3);
                assert!(capabilities.contains(&AgentCapability::RemoteDesktop));
                assert!(capabilities.contains(&AgentCapability::Terminal));
                assert!(capabilities.contains(&AgentCapability::FileManager));
            }
            other => panic!("expected AgentRegister, got {:?}", other),
        }
    });

    // Client (agent) connects
    let client_endpoint = quinn::Endpoint::client("0.0.0.0:0".parse().unwrap()).unwrap();
    let conn = client_endpoint
        .connect_with(client_config, server_addr, "server")
        .unwrap()
        .await
        .unwrap();
    let (mut send, mut recv) = conn.accept_bi().await.unwrap();

    client_handshake(&mut send, &mut recv, &agent_cert_der).await;

    let stream = AsyncControlStream::new(tokio::io::join(recv, send));
    let config = mesh_agent_core::AgentConfig {
        server_addr: server_addr.to_string(),
        server_ca_pem: String::new(),
        data_dir: std::path::PathBuf::from("/tmp"),
    };
    let mut agent_conn = AgentConnection::new(stream, config);

    agent_conn
        .send_control(ControlMessage::AgentRegister {
            capabilities: vec![
                AgentCapability::RemoteDesktop,
                AgentCapability::Terminal,
                AgentCapability::FileManager,
            ],
            hostname: gethostname::gethostname().to_string_lossy().to_string(),
            os: std::env::consts::OS.to_string(),
            arch: std::env::consts::ARCH.to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
        })
        .await
        .unwrap();

    server_handle.await.unwrap();
}

#[tokio::test]
async fn test_quic_session_request_response() {
    let certs = generate_test_certs();
    let (server_endpoint, server_addr) = build_server_endpoint(&certs);
    let client_config = build_client_config(&certs);

    let ca_cert_der = certs.ca_cert_der.clone();
    let agent_cert_der = certs.agent_cert_der.clone();

    // Spawn mock server: handshake → read register → send SessionRequest → read SessionAccept
    let server_handle = tokio::spawn(async move {
        let incoming = server_endpoint.accept().await.unwrap();
        let conn = incoming.await.unwrap();
        let (mut send, mut recv) = conn.open_bi().await.unwrap();

        server_handshake(&mut send, &mut recv, &ca_cert_der).await;

        let stream = AsyncControlStream::new(tokio::io::join(recv, send));
        let config = mesh_agent_core::AgentConfig {
            server_addr: server_addr.to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp"),
        };
        let mut agent_conn = AgentConnection::new(stream, config);

        let _register = agent_conn.receive_control().await.unwrap();

        // Send SessionRequest
        let token = mesh_protocol::SessionToken::generate();
        agent_conn
            .send_control(ControlMessage::SessionRequest {
                token: token.clone(),
                relay_url: "wss://relay.test/session".to_string(),
                permissions: mesh_protocol::Permissions {
                    desktop: true,
                    terminal: true,
                    file_read: true,
                    file_write: false,
                    input: true,
                },
            })
            .await
            .unwrap();

        // Read SessionAccept
        let response = agent_conn.receive_control().await.unwrap();
        match response {
            ControlMessage::SessionAccept {
                token: t,
                relay_url,
            } => {
                assert_eq!(t.as_str(), token.as_str());
                assert_eq!(relay_url, "wss://relay.test/session");
            }
            other => panic!("expected SessionAccept, got {:?}", other),
        }
    });

    // Client connects
    let client_endpoint = quinn::Endpoint::client("0.0.0.0:0".parse().unwrap()).unwrap();
    let conn = client_endpoint
        .connect_with(client_config, server_addr, "server")
        .unwrap()
        .await
        .unwrap();
    let (mut send, mut recv) = conn.accept_bi().await.unwrap();

    client_handshake(&mut send, &mut recv, &agent_cert_der).await;

    let stream = AsyncControlStream::new(tokio::io::join(recv, send));
    let config = mesh_agent_core::AgentConfig {
        server_addr: server_addr.to_string(),
        server_ca_pem: String::new(),
        data_dir: std::path::PathBuf::from("/tmp"),
    };
    let mut conn = AgentConnection::new(stream, config);

    conn.send_control(ControlMessage::AgentRegister {
        capabilities: vec![AgentCapability::Terminal],
        hostname: "test".to_string(),
        os: "linux".to_string(),
        arch: "x86_64".to_string(),
        version: "0.1.0".to_string(),
    })
    .await
    .unwrap();

    // Receive SessionRequest
    let msg = conn.receive_control().await.unwrap();
    match &msg {
        ControlMessage::SessionRequest {
            token,
            relay_url,
            permissions,
        } => {
            assert_eq!(relay_url, "wss://relay.test/session");
            assert!(permissions.desktop);
            assert!(permissions.terminal);

            // Send SessionAccept back
            conn.send_control(ControlMessage::SessionAccept {
                token: token.clone(),
                relay_url: relay_url.clone(),
            })
            .await
            .unwrap();
        }
        other => panic!("expected SessionRequest, got {:?}", other),
    }

    server_handle.await.unwrap();
}

#[tokio::test]
async fn test_quic_disconnect_detection() {
    let certs = generate_test_certs();
    let (server_endpoint, server_addr) = build_server_endpoint(&certs);
    let client_config = build_client_config(&certs);

    let ca_cert_der = certs.ca_cert_der.clone();
    let agent_cert_der = certs.agent_cert_der.clone();

    // Server: handshake → read register → close connection
    let server_handle = tokio::spawn(async move {
        let incoming = server_endpoint.accept().await.unwrap();
        let conn = incoming.await.unwrap();
        let (mut send, mut recv) = conn.open_bi().await.unwrap();

        server_handshake(&mut send, &mut recv, &ca_cert_der).await;

        let stream = AsyncControlStream::new(tokio::io::join(recv, send));
        let config = mesh_agent_core::AgentConfig {
            server_addr: server_addr.to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp"),
        };
        let mut agent_conn = AgentConnection::new(stream, config);
        let _register = agent_conn.receive_control().await.unwrap();

        // Drop everything to simulate disconnect
        drop(agent_conn);
        conn.close(0u32.into(), b"test done");
    });

    // Client connects
    let client_endpoint = quinn::Endpoint::client("0.0.0.0:0".parse().unwrap()).unwrap();
    let quic_conn = client_endpoint
        .connect_with(client_config, server_addr, "server")
        .unwrap()
        .await
        .unwrap();
    let (mut send, mut recv) = quic_conn.accept_bi().await.unwrap();

    client_handshake(&mut send, &mut recv, &agent_cert_der).await;

    let stream = AsyncControlStream::new(tokio::io::join(recv, send));
    let config = mesh_agent_core::AgentConfig {
        server_addr: server_addr.to_string(),
        server_ca_pem: String::new(),
        data_dir: std::path::PathBuf::from("/tmp"),
    };
    let mut conn = AgentConnection::new(stream, config);

    conn.send_control(ControlMessage::AgentRegister {
        capabilities: vec![AgentCapability::Terminal],
        hostname: "test".to_string(),
        os: "linux".to_string(),
        arch: "x86_64".to_string(),
        version: "0.1.0".to_string(),
    })
    .await
    .unwrap();

    server_handle.await.unwrap();

    // After server drops, the next receive should return an error
    let result = conn.receive_control().await;
    assert!(result.is_err(), "expected error after server disconnect");
}
