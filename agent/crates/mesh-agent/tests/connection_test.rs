//! Integration test: QUIC mTLS connect + handshake + AgentRegister roundtrip.
//!
//! Exercises the full transport stack: CA → certs → QUIC → handshake → control messages.

mod test_helpers;

use mesh_agent_core::{AgentConnection, AsyncControlStream};
use mesh_protocol::{AgentCapability, ControlMessage};
use test_helpers::*;

#[tokio::test]
async fn test_quic_agent_register_roundtrip() {
    let certs = generate_test_certs();
    let (server_endpoint, server_addr) = build_server_endpoint(&certs);
    let client_config = build_client_config(&certs);

    let ca_cert_der = certs.ca_cert_der.clone();
    let agent_cert_der = certs.agent_cert_der.clone();

    // Spawn mock server
    let server_handle = tokio::spawn(async move {
        let incoming = server_endpoint
            .accept()
            .await
            .expect("accept incoming connection");
        let conn = incoming.await.expect("complete server connection");
        let (mut send, mut recv) = conn.open_bi().await.expect("open bidirectional stream");

        server_handshake(&mut send, &mut recv, &ca_cert_der).await;

        let stream = AsyncControlStream::new(tokio::io::join(recv, send));
        let config = mesh_agent_core::AgentConfig {
            server_addr: server_addr.to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp"),
        };
        let mut agent_conn = AgentConnection::new(stream, config);

        let msg = agent_conn
            .receive_control()
            .await
            .expect("receive AgentRegister");
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
                assert_eq!(capabilities.len(), 2);
                assert!(capabilities.contains(&AgentCapability::Terminal));
                assert!(capabilities.contains(&AgentCapability::FileManager));
            }
            other => panic!("expected AgentRegister, got {:?}", other),
        }
    });

    // Client (agent) connects
    let client_endpoint =
        quinn::Endpoint::client("0.0.0.0:0".parse().expect("parse client bind address"))
            .expect("create client endpoint");
    let conn = client_endpoint
        .connect_with(client_config, server_addr, "server")
        .expect("initiate client connection")
        .await
        .expect("complete client connection");
    let (mut send, mut recv) = conn.accept_bi().await.expect("accept bidirectional stream");

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
            capabilities: vec![AgentCapability::Terminal, AgentCapability::FileManager],
            hostname: gethostname::gethostname().to_string_lossy().to_string(),
            os: std::env::consts::OS.to_string(),
            arch: std::env::consts::ARCH.to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
        })
        .await
        .expect("send AgentRegister");

    server_handle.await.expect("server task completed");
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
        let incoming = server_endpoint
            .accept()
            .await
            .expect("accept incoming connection");
        let conn = incoming.await.expect("complete server connection");
        let (mut send, mut recv) = conn.open_bi().await.expect("open bidirectional stream");

        server_handshake(&mut send, &mut recv, &ca_cert_der).await;

        let stream = AsyncControlStream::new(tokio::io::join(recv, send));
        let config = mesh_agent_core::AgentConfig {
            server_addr: server_addr.to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp"),
        };
        let mut agent_conn = AgentConnection::new(stream, config);

        let _register = agent_conn
            .receive_control()
            .await
            .expect("receive AgentRegister");

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
            .expect("send SessionRequest");

        // Read SessionAccept
        let response = agent_conn
            .receive_control()
            .await
            .expect("receive SessionAccept");
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
    let client_endpoint =
        quinn::Endpoint::client("0.0.0.0:0".parse().expect("parse client bind address"))
            .expect("create client endpoint");
    let conn = client_endpoint
        .connect_with(client_config, server_addr, "server")
        .expect("initiate client connection")
        .await
        .expect("complete client connection");
    let (mut send, mut recv) = conn.accept_bi().await.expect("accept bidirectional stream");

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
    .expect("send AgentRegister");

    // Receive SessionRequest
    let msg = conn
        .receive_control()
        .await
        .expect("receive SessionRequest");
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
            .expect("send SessionAccept");
        }
        other => panic!("expected SessionRequest, got {:?}", other),
    }

    server_handle.await.expect("server task completed");
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
        let incoming = server_endpoint
            .accept()
            .await
            .expect("accept incoming connection");
        let conn = incoming.await.expect("complete server connection");
        let (mut send, mut recv) = conn.open_bi().await.expect("open bidirectional stream");

        server_handshake(&mut send, &mut recv, &ca_cert_der).await;

        let stream = AsyncControlStream::new(tokio::io::join(recv, send));
        let config = mesh_agent_core::AgentConfig {
            server_addr: server_addr.to_string(),
            server_ca_pem: String::new(),
            data_dir: std::path::PathBuf::from("/tmp"),
        };
        let mut agent_conn = AgentConnection::new(stream, config);
        let _register = agent_conn
            .receive_control()
            .await
            .expect("receive AgentRegister");

        // Drop everything to simulate disconnect
        drop(agent_conn);
        conn.close(0u32.into(), b"test done");
    });

    // Client connects
    let client_endpoint =
        quinn::Endpoint::client("0.0.0.0:0".parse().expect("parse client bind address"))
            .expect("create client endpoint");
    let quic_conn = client_endpoint
        .connect_with(client_config, server_addr, "server")
        .expect("initiate client connection")
        .await
        .expect("complete client connection");
    let (mut send, mut recv) = quic_conn
        .accept_bi()
        .await
        .expect("accept bidirectional stream");

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
    .expect("send AgentRegister");

    server_handle.await.expect("server task completed");

    // After server drops, the next receive should return an error
    let result = conn.receive_control().await;
    assert!(result.is_err(), "expected error after server disconnect");
}
