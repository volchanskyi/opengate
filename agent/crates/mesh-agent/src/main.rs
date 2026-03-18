//! OpenGate mesh-agent binary.
//!
//! Connects to the server via QUIC, registers capabilities,
//! handles session requests, and applies binary updates.
//! Exit code 42 signals the service manager to restart after an update.

use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;

use anyhow::{Context, Result};
use clap::Parser;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha384};
use tracing::{error, info, warn};

/// OpenGate mesh agent.
#[derive(Parser, Debug)]
#[command(version, about = "OpenGate mesh agent")]
struct Args {
    /// Server address (host:port) for QUIC connection.
    #[arg(long, env = "OPENGATE_SERVER_ADDR")]
    server_addr: String,

    /// Path to the server CA certificate PEM file.
    #[arg(long, env = "OPENGATE_SERVER_CA")]
    server_ca: PathBuf,

    /// Data directory for identity, keys, and temporary files.
    #[arg(long, default_value = "/var/lib/mesh-agent", env = "OPENGATE_DATA_DIR")]
    data_dir: PathBuf,

    /// Ed25519 public key hex for verifying update signatures (optional).
    #[arg(long, env = "OPENGATE_UPDATE_PUBLIC_KEY")]
    update_public_key: Option<String>,

    /// Enrollment URL (e.g. https://opengate.example.com). Used on first boot
    /// to obtain a CA-signed certificate via CSR enrollment.
    #[arg(long, env = "OPENGATE_ENROLL_URL")]
    enroll_url: Option<String>,

    /// Enrollment token for first-boot CSR enrollment.
    #[arg(long, env = "OPENGATE_ENROLL_TOKEN")]
    enroll_token: Option<String>,
}

/// Exit code that tells systemd (RestartForceExitStatus=42) to restart the agent
/// after a successful binary update.
const EXIT_CODE_RESTART: i32 = 42;

/// Request body for the enrollment endpoint.
#[derive(Serialize)]
struct EnrollRequestBody {
    csr_pem: String,
}

/// Response from the enrollment endpoint.
#[derive(Deserialize)]
struct EnrollResponse {
    ca_pem: String,
    cert_pem: Option<String>,
    server_addr: String,
}

/// Perform first-boot enrollment: generate CSR, POST to server, save signed cert + CA.
async fn enroll(
    enroll_url: &str,
    enroll_token: &str,
    data_dir: &std::path::Path,
    server_ca_path: &std::path::Path,
) -> Result<mesh_agent_core::AgentIdentity> {
    info!("first boot detected, starting CSR enrollment");

    let pending =
        mesh_agent_core::PendingIdentity::generate(data_dir).context("generate enrollment CSR")?;

    info!(device_id = %pending.device_id.0, "generated enrollment CSR");

    let url = format!(
        "{}/api/v1/enroll/{}",
        enroll_url.trim_end_matches('/'),
        enroll_token
    );

    let client = reqwest::Client::builder()
        .danger_accept_invalid_certs(true) // first boot — no CA yet
        .build()
        .context("build HTTP client")?;

    let resp = client
        .post(&url)
        .json(&EnrollRequestBody {
            csr_pem: pending.csr_pem,
        })
        .send()
        .await
        .context("enrollment HTTP request")?;

    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        anyhow::bail!("enrollment failed: HTTP {status}: {body}");
    }

    let enroll_resp: EnrollResponse = resp.json().await.context("parse enrollment response")?;

    let cert_pem = enroll_resp
        .cert_pem
        .ok_or_else(|| anyhow::anyhow!("server did not return a signed certificate"))?;

    // Decode the PEM certificate to DER.
    let cert_der = pem::parse(cert_pem.as_bytes()).context("decode cert PEM from server")?;

    // Save the CA-signed cert.
    mesh_agent_core::AgentIdentity::save_signed_cert(data_dir, cert_der.contents())
        .context("save signed certificate")?;

    // Save the CA PEM for future connections.
    tokio::fs::write(server_ca_path, &enroll_resp.ca_pem)
        .await
        .context("save server CA certificate")?;

    info!(server_addr = %enroll_resp.server_addr, "enrollment complete");

    // Now load the full identity (device_id + signed cert + key).
    let identity = mesh_agent_core::AgentIdentity::load_or_create(data_dir)
        .context("load identity after enrollment")?;

    Ok(identity)
}

/// Check if enrollment is needed (no complete identity on disk).
fn needs_enrollment(data_dir: &std::path::Path) -> bool {
    let id_exists = data_dir.join(mesh_agent_core::DEVICE_ID_FILE).exists();
    let cert_exists = data_dir.join(mesh_agent_core::CERT_FILE).exists();
    let key_exists = data_dir.join(mesh_agent_core::KEY_FILE).exists();
    !(id_exists && cert_exists && key_exists)
}

/// Build the quinn QUIC client config with mTLS.
fn build_quic_config(
    ca_pem: &str,
    identity: &mesh_agent_core::AgentIdentity,
) -> Result<quinn::ClientConfig> {
    let server_certs = rustls_pemfile::certs(&mut ca_pem.as_bytes())
        .collect::<Result<Vec<_>, _>>()
        .context("parse CA PEM")?;

    let mut root_store = rustls::RootCertStore::empty();
    for cert in server_certs {
        root_store.add(cert).context("add CA cert")?;
    }

    let client_cert = rustls::pki_types::CertificateDer::from(identity.cert_der.clone());
    let client_key = rustls::pki_types::PrivateKeyDer::Pkcs8(
        rustls::pki_types::PrivatePkcs8KeyDer::from(identity.key_der.clone()),
    );

    let mut tls_config = rustls::ClientConfig::builder()
        .with_root_certificates(root_store)
        .with_client_auth_cert(vec![client_cert], client_key)
        .context("build TLS config")?;

    tls_config.alpn_protocols = vec![b"opengate".to_vec()];

    let quinn_config = quinn::ClientConfig::new(Arc::new(
        quinn::crypto::rustls::QuicClientConfig::try_from(tls_config)?,
    ));

    Ok(quinn_config)
}

/// Perform the binary handshake: read ServerHello, send AgentHello.
async fn perform_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    cert_der: &[u8],
) -> Result<()> {
    // Read ServerHello (81 bytes: 1 type + 32 nonce + 48 cert_hash)
    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf)
        .await
        .context("read ServerHello")?;

    let server_hello =
        mesh_protocol::HandshakeMessage::decode_binary(&hello_buf).context("decode ServerHello")?;

    match &server_hello {
        mesh_protocol::HandshakeMessage::ServerHello { .. } => {
            info!("received ServerHello");
        }
        other => anyhow::bail!("expected ServerHello, got {:?}", other),
    }

    // Compute agent cert SHA-384 hash
    let agent_cert_hash: [u8; 48] = Sha384::digest(cert_der).into();

    // Generate random nonce
    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).context("generate nonce")?;

    // Build and send AgentHello
    let agent_hello = mesh_protocol::HandshakeMessage::AgentHello {
        nonce,
        agent_cert_hash,
    };
    send.write_all(&agent_hello.encode_binary())
        .await
        .context("write AgentHello")?;

    info!("handshake complete");
    Ok(())
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let args = Args::parse();

    info!(
        version = env!("CARGO_PKG_VERSION"),
        server_addr = %args.server_addr,
        data_dir = %args.data_dir.display(),
        "mesh-agent starting"
    );

    // Ensure data directory exists
    tokio::fs::create_dir_all(&args.data_dir)
        .await
        .context("create data directory")?;

    // Parse update public key if provided
    let update_public_key: Option<[u8; 32]> = match &args.update_public_key {
        Some(hex_str) => {
            let bytes = hex::decode(hex_str).context("decode update public key hex")?;
            let key: [u8; 32] = bytes.try_into().map_err(|v: Vec<u8>| {
                anyhow::anyhow!("update public key must be 32 bytes, got {}", v.len())
            })?;
            Some(key)
        }
        None => {
            warn!("no update public key configured, auto-updates disabled");
            None
        }
    };

    // Load existing identity, or enroll to get a CA-signed certificate.
    // Enrollment also writes the CA PEM to --server-ca, so it must happen
    // before we read the CA file.
    let identity = if needs_enrollment(&args.data_dir) {
        match (&args.enroll_url, &args.enroll_token) {
            (Some(url), Some(token)) => enroll(url, token, &args.data_dir, &args.server_ca).await?,
            _ => {
                anyhow::bail!(
                    "no agent identity found at {} and --enroll-url / --enroll-token not set; \
                     cannot connect without enrollment",
                    args.data_dir.display()
                );
            }
        }
    } else {
        mesh_agent_core::AgentIdentity::load_or_create(&args.data_dir)
            .context("load agent identity")?
    };

    info!(device_id = %identity.device_id.0, "agent identity loaded");

    // Read server CA (written by enrollment on first boot, or pre-existing).
    let ca_pem = tokio::fs::read_to_string(&args.server_ca)
        .await
        .context("read server CA certificate")?;

    // Build QUIC client config (needs ca_pem reference before it moves into AgentConfig)
    let quinn_config = build_quic_config(&ca_pem, &identity)?;

    let config = mesh_agent_core::AgentConfig {
        server_addr: args.server_addr,
        server_ca_pem: ca_pem,
        data_dir: args.data_dir.clone(),
    };

    // Build update config
    let update_config = update_public_key.map(|key| mesh_agent_core::UpdateConfig {
        signing_public_key: key,
        current_binary_path: std::env::current_exe()
            .unwrap_or_else(|_| PathBuf::from("mesh-agent")),
        data_dir: args.data_dir.clone(),
    });

    // Platform lifecycle
    let lifecycle = platform_linux::create_service_lifecycle();

    // QUIC endpoint
    let endpoint = quinn::Endpoint::client("0.0.0.0:0".parse::<SocketAddr>()?)?;

    // Notify systemd we're ready
    lifecycle.notify_ready();
    info!("agent ready, connecting to server");

    // Shutdown signal handler
    let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?;

    // Main reconnect loop
    'outer: loop {
        // Connect with exponential backoff
        let connect_result = mesh_agent_core::reconnect_with_backoff(
            || {
                let addr_str = config.server_addr.clone();
                let qc = quinn_config.clone();
                let ep = endpoint.clone();
                async move {
                    // Extract hostname for TLS SNI verification.
                    let sni_host = addr_str
                        .rsplit_once(':')
                        .map(|(h, _)| h)
                        .unwrap_or(&addr_str);

                    let addr: SocketAddr = match addr_str.parse() {
                        Ok(a) => a,
                        Err(_) => tokio::net::lookup_host(&addr_str)
                            .await
                            .map_err(|e| format!("resolve server addr: {e}"))?
                            .next()
                            .ok_or_else(|| format!("no addresses found for {addr_str}"))?,
                    };
                    let conn = ep
                        .connect_with(qc, addr, sni_host)
                        .map_err(|e| format!("QUIC connect: {e}"))?
                        .await
                        .map_err(|e| format!("QUIC establish: {e}"))?;
                    // Server opens the bidirectional stream (stream ownership workaround)
                    let (send, recv) = conn
                        .accept_bi()
                        .await
                        .map_err(|e| format!("accept bi stream: {e}"))?;
                    Ok::<_, String>((send, recv))
                }
            },
            10,
        )
        .await;

        let (mut send, mut recv) = match connect_result {
            Ok(streams) => streams,
            Err(e) => {
                error!(error = %e, "all reconnect attempts failed, exiting");
                lifecycle.notify_stopping();
                return Err(e.into());
            }
        };

        // Perform binary handshake (ServerHello / AgentHello)
        if let Err(e) = perform_handshake(&mut send, &mut recv, &identity.cert_der).await {
            warn!(error = %e, "handshake failed, will reconnect");
            continue;
        }

        // Wrap QUIC streams into AsyncControlStream
        let stream = mesh_agent_core::AsyncControlStream::new(tokio::io::join(recv, send));
        let mut conn = mesh_agent_core::AgentConnection::new(stream, config.clone());

        // Register with server
        if let Err(e) = conn
            .send_control(mesh_protocol::ControlMessage::AgentRegister {
                capabilities: vec![
                    mesh_protocol::AgentCapability::RemoteDesktop,
                    mesh_protocol::AgentCapability::Terminal,
                    mesh_protocol::AgentCapability::FileManager,
                ],
                hostname: gethostname::gethostname().to_string_lossy().to_string(),
                os: std::env::consts::OS.to_string(),
                arch: std::env::consts::ARCH.to_string(),
                version: env!("CARGO_PKG_VERSION").to_string(),
            })
            .await
        {
            warn!(error = %e, "failed to send AgentRegister, will reconnect");
            continue;
        }

        info!("registered with server, entering control loop");

        // Control loop — dispatch messages until disconnect
        loop {
            tokio::select! {
                biased;
                _ = tokio::signal::ctrl_c() => {
                    info!("received SIGINT, shutting down");
                    break 'outer;
                }
                _ = sigterm.recv() => {
                    info!("received SIGTERM, shutting down");
                    break 'outer;
                }
                msg = conn.receive_control() => {
                    match msg {
                        Ok(mesh_protocol::ControlMessage::SessionRequest {
                            token, relay_url, permissions,
                        }) => {
                            let capture = platform_linux::create_screen_capture();
                            let injector = platform_linux::create_input_injector();
                            match conn.handle_session_request(
                                token, relay_url, permissions, capture, injector,
                            ).await {
                                Ok(_handle) => {} // session runs independently
                                Err(e) => warn!(error = %e, "failed to accept session"),
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::AgentUpdate {
                            version, url, signature,
                        }) => {
                            if let Some(ref uc) = update_config {
                                match mesh_agent_core::update::apply_update(
                                    uc, &version, &url, "", &signature,
                                ).await {
                                    Ok(true) => {
                                        info!(version, "update applied, restarting");
                                        lifecycle.notify_stopping();
                                        std::process::exit(EXIT_CODE_RESTART);
                                    }
                                    Ok(false) => info!("update skipped (same version)"),
                                    Err(e) => error!(error = %e, "update failed"),
                                }
                            }
                        }
                        Ok(_other) => { /* ignore unknown messages */ }
                        Err(e) if e.to_string().contains("ping received") => {
                            // Ping was handled (pong sent), continue listening
                            continue;
                        }
                        Err(mesh_agent_core::ConnectionError::Io(_)) => {
                            warn!("connection lost, will reconnect");
                            break; // break inner loop, outer loop reconnects
                        }
                        Err(e) => {
                            warn!(error = %e, "control error, will reconnect");
                            break;
                        }
                    }
                }
            }
        }
    }

    lifecycle.notify_stopping();
    info!("mesh-agent stopped");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use clap::Parser;

    #[test]
    fn test_exit_code_restart_is_42() {
        assert_eq!(EXIT_CODE_RESTART, 42);
    }

    #[test]
    fn test_cli_args_valid_parse() {
        let args = Args::try_parse_from([
            "mesh-agent",
            "--server-addr",
            "127.0.0.1:9090",
            "--server-ca",
            "/tmp/ca.pem",
        ])
        .unwrap();
        assert_eq!(args.server_addr, "127.0.0.1:9090");
        assert_eq!(args.server_ca, PathBuf::from("/tmp/ca.pem"));
        assert_eq!(args.data_dir, PathBuf::from("/var/lib/mesh-agent"));
        assert!(args.update_public_key.is_none());
    }

    #[test]
    fn test_cli_args_custom_data_dir() {
        let args = Args::try_parse_from([
            "mesh-agent",
            "--server-addr",
            "10.0.0.1:9090",
            "--server-ca",
            "/etc/agent/ca.pem",
            "--data-dir",
            "/opt/agent/data",
        ])
        .unwrap();
        assert_eq!(args.data_dir, PathBuf::from("/opt/agent/data"));
    }

    #[test]
    fn test_cli_args_with_update_key() {
        let key_hex = "a".repeat(64);
        let args = Args::try_parse_from([
            "mesh-agent",
            "--server-addr",
            "127.0.0.1:9090",
            "--server-ca",
            "/tmp/ca.pem",
            "--update-public-key",
            &key_hex,
        ])
        .unwrap();
        assert_eq!(args.update_public_key, Some(key_hex));
    }

    #[test]
    fn test_cli_args_missing_server_addr() {
        let result = Args::try_parse_from(["mesh-agent", "--server-ca", "/tmp/ca.pem"]);
        assert!(result.is_err());
    }

    #[test]
    fn test_cli_args_missing_server_ca() {
        let result = Args::try_parse_from(["mesh-agent", "--server-addr", "127.0.0.1:9090"]);
        assert!(result.is_err());
    }

    #[test]
    fn test_build_quic_config_valid_certs() {
        let ca_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut ca_params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        ca_params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        ca_params.distinguished_name.push(
            rcgen::DnType::CommonName,
            rcgen::DnValue::Utf8String("Test CA".to_string()),
        );
        let ca_cert = ca_params.self_signed(&ca_key).unwrap();
        let ca_pem = ca_cert.pem();

        let dir = tempfile::tempdir().unwrap();
        let identity = mesh_agent_core::AgentIdentity::load_or_create(dir.path()).unwrap();

        let result = build_quic_config(&ca_pem, &identity);
        assert!(
            result.is_ok(),
            "expected valid config, got: {:?}",
            result.err()
        );
    }

    #[test]
    fn test_build_quic_config_empty_ca_pem() {
        let dir = tempfile::tempdir().unwrap();
        let identity = mesh_agent_core::AgentIdentity::load_or_create(dir.path()).unwrap();

        // Empty PEM yields empty root store — config still builds but would fail at handshake
        let result = build_quic_config("", &identity);
        assert!(result.is_ok());
    }

    #[test]
    fn test_cli_args_with_enrollment_flags() {
        let args = Args::try_parse_from([
            "mesh-agent",
            "--server-addr",
            "127.0.0.1:9090",
            "--server-ca",
            "/tmp/ca.pem",
            "--enroll-url",
            "https://opengate.example.com",
            "--enroll-token",
            "abc123",
        ])
        .unwrap();
        assert_eq!(
            args.enroll_url,
            Some("https://opengate.example.com".to_string())
        );
        assert_eq!(args.enroll_token, Some("abc123".to_string()));
    }

    #[test]
    fn test_needs_enrollment_empty_dir() {
        let dir = tempfile::tempdir().unwrap();
        assert!(needs_enrollment(dir.path()));
    }

    #[test]
    fn test_needs_enrollment_complete_identity() {
        let dir = tempfile::tempdir().unwrap();
        // Create all three identity files.
        std::fs::write(dir.path().join("device_id.txt"), "test-id").unwrap();
        std::fs::write(dir.path().join("agent.crt"), b"cert").unwrap();
        std::fs::write(dir.path().join("agent.key"), b"key").unwrap();
        assert!(!needs_enrollment(dir.path()));
    }

    #[test]
    fn test_needs_enrollment_partial_identity() {
        let dir = tempfile::tempdir().unwrap();
        // Only device_id and key (pending enrollment).
        std::fs::write(dir.path().join("device_id.txt"), "test-id").unwrap();
        std::fs::write(dir.path().join("agent.key"), b"key").unwrap();
        assert!(needs_enrollment(dir.path()));
    }
}
