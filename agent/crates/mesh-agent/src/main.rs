//! OpenGate mesh-agent binary.
//!
//! Connects to the server via QUIC, registers capabilities,
//! handles session requests, and applies binary updates.
//! Exit code 42 signals the service manager to restart after an update.

use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use anyhow::{Context, Result};
use clap::Parser;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha384};
use tokio::io::AsyncWriteExt;
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
    update_signing_key: Option<String>,
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

    // Save the update signing key if the server provided one.
    if let Some(ref key_hex) = enroll_resp.update_signing_key {
        let key_path = data_dir.join("update-signing-key.hex");
        tokio::fs::write(&key_path, key_hex)
            .await
            .context("save update signing key")?;
        info!("saved update signing key from enrollment");
    }

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
    send.flush().await.context("flush AgentHello")?;

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
        version = env!("AGENT_VERSION"),
        server_addr = %args.server_addr,
        data_dir = %args.data_dir.display(),
        "mesh-agent starting"
    );

    // Ensure data directory exists
    tokio::fs::create_dir_all(&args.data_dir)
        .await
        .context("create data directory")?;

    // Parse update public key: CLI flag takes precedence, then saved file from enrollment.
    let update_public_key: Option<[u8; 32]> = match &args.update_public_key {
        Some(hex_str) => {
            let bytes = hex::decode(hex_str).context("decode update public key hex")?;
            let key: [u8; 32] = bytes.try_into().map_err(|v: Vec<u8>| {
                anyhow::anyhow!("update public key must be 32 bytes, got {}", v.len())
            })?;
            Some(key)
        }
        None => {
            // Try loading from enrollment-saved file.
            let key_path = args.data_dir.join("update-signing-key.hex");
            match tokio::fs::read_to_string(&key_path).await {
                Ok(hex_str) => {
                    let hex_str = hex_str.trim();
                    let bytes = hex::decode(hex_str).context("decode saved update signing key")?;
                    let key: [u8; 32] = bytes.try_into().map_err(|v: Vec<u8>| {
                        anyhow::anyhow!("saved signing key must be 32 bytes, got {}", v.len())
                    })?;
                    info!("loaded update signing key from enrollment");
                    Some(key)
                }
                Err(_) => {
                    warn!("no update public key configured, auto-updates disabled");
                    None
                }
            }
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

    // Rollback guard: if a previous update left a sentinel, start a watchdog.
    // The watchdog is cancelled once we successfully register with the server.
    // If registration doesn't happen within 60 seconds, rollback and restart.
    let pending_update = mesh_agent_core::update::is_update_pending(&args.data_dir);
    let watchdog_cancel = Arc::new(tokio::sync::Notify::new());
    if pending_update {
        let count = mesh_agent_core::update::rollback_count(&args.data_dir).await;
        if count >= mesh_agent_core::update::MAX_ROLLBACKS {
            error!(
                count,
                "max rollback attempts reached, leaving current binary in place"
            );
            mesh_agent_core::update::clear_update_pending(&args.data_dir).await;
            mesh_agent_core::update::reset_rollback_count(&args.data_dir).await;
        } else {
            info!(
                count,
                "post-update watchdog active, will rollback if registration fails within 60s"
            );
            spawn_update_watchdog(&args.data_dir, &update_config, watchdog_cancel.clone());
        }
    }

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
                capabilities: {
                    let mut caps = vec![
                        mesh_protocol::AgentCapability::Terminal,
                        mesh_protocol::AgentCapability::FileManager,
                    ];
                    if platform_linux::has_display() {
                        caps.push(mesh_protocol::AgentCapability::RemoteDesktop);
                    }
                    caps
                },
                hostname: gethostname::gethostname().to_string_lossy().to_string(),
                os: os_pretty_name(),
                arch: std::env::consts::ARCH.to_string(),
                version: env!("AGENT_VERSION").to_string(),
            })
            .await
        {
            warn!(error = %e, "failed to send AgentRegister, will reconnect");
            continue;
        }

        info!("registered with server, entering control loop");

        // Registration succeeded — cancel watchdog and clear sentinel.
        if pending_update {
            watchdog_cancel.notify_one();
            mesh_agent_core::update::clear_update_pending(&args.data_dir).await;
            mesh_agent_core::update::reset_rollback_count(&args.data_dir).await;
            info!("post-update verification passed, sentinel cleared");
        }

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
                            version, url, sha256, signature,
                        }) => {
                            if let Some(ref uc) = update_config {
                                // Version comparison: skip if incoming <= current
                                if should_skip_version(&version) {
                                    info!(version, "update skipped: already up to date");
                                    send_update_ack(&mut conn, version, true, "already up to date".into()).await;
                                    continue;
                                }

                                match mesh_agent_core::update::apply_update(
                                    uc, &version, &url, &sha256, &signature,
                                ).await {
                                    Ok(true) => {
                                        info!(version, "update applied, restarting");
                                        send_update_ack(&mut conn, version, true, String::new()).await;
                                        lifecycle.notify_stopping();
                                        std::process::exit(EXIT_CODE_RESTART);
                                    }
                                    Ok(false) => info!("update skipped (same version)"),
                                    Err(e) => {
                                        error!(error = %e, "update failed");
                                        send_update_ack(&mut conn, version, false, e.to_string()).await;
                                    }
                                }
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::AgentDeregistered { reason }) => {
                            warn!(reason, "device deregistered by server, cleaning up");
                            uninstall_agent(&args.data_dir);
                            lifecycle.notify_stopping();
                            info!("agent fully uninstalled, exiting");
                            std::process::exit(0);
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

/// Fully uninstalls the agent: stops systemd service, removes identity,
/// config, data directories, service unit, and binary.
/// Called when the server deregisters this device.
fn uninstall_agent(data_dir: &std::path::Path) {
    use std::process::Command;

    const SERVICE_NAME: &str = "mesh-agent";
    const CONFIG_DIR: &str = "/etc/opengate-agent";
    const INSTALL_DIR: &str = "/usr/local/bin";
    const BINARY_NAME: &str = "mesh-agent";

    // Stop and disable the systemd service (best-effort).
    for action in &["stop", "disable"] {
        if let Err(e) = Command::new("systemctl")
            .args([action, SERVICE_NAME])
            .output()
        {
            warn!(action, error = %e, "systemctl command failed");
        }
    }

    // Remove identity files from data directory.
    let identity_files = [
        mesh_agent_core::DEVICE_ID_FILE,
        mesh_agent_core::CERT_FILE,
        mesh_agent_core::KEY_FILE,
        "server_ca.pem",
    ];
    for filename in &identity_files {
        let path = data_dir.join(filename);
        if path.exists() {
            if let Err(e) = std::fs::remove_file(&path) {
                warn!(file = %path.display(), error = %e, "failed to remove identity file");
            }
        }
    }

    // Remove directories and service unit.
    let dirs_to_remove = [data_dir.to_path_buf(), std::path::PathBuf::from(CONFIG_DIR)];
    for dir in &dirs_to_remove {
        if dir.exists() {
            if let Err(e) = std::fs::remove_dir_all(dir) {
                warn!(dir = %dir.display(), error = %e, "failed to remove directory");
            }
        }
    }

    let service_file =
        std::path::PathBuf::from(format!("/etc/systemd/system/{SERVICE_NAME}.service"));
    if service_file.exists() {
        if let Err(e) = std::fs::remove_file(&service_file) {
            warn!(file = %service_file.display(), error = %e, "failed to remove service file");
        }
    }

    // Reload systemd after removing the unit file.
    let _ = Command::new("systemctl").arg("daemon-reload").output();

    // Remove the binary itself.
    let binary_path = std::path::PathBuf::from(INSTALL_DIR).join(BINARY_NAME);
    if binary_path.exists() {
        if let Err(e) = std::fs::remove_file(&binary_path) {
            warn!(file = %binary_path.display(), error = %e, "failed to remove binary");
        }
    }

    info!("agent uninstalled: service stopped, files removed");
}

/// Returns `true` if the incoming version should be skipped (not newer than current).
///
/// Parses both `AGENT_VERSION` and `incoming` as semver. If the incoming version
/// is less than or equal to the current version, the update is skipped.
/// If either version fails to parse, the function returns `false` (fail-open)
/// to allow the update to proceed.
fn should_skip_version(incoming: &str) -> bool {
    let current = env!("AGENT_VERSION");
    match (
        semver::Version::parse(current),
        semver::Version::parse(incoming),
    ) {
        (Ok(cur), Ok(inc)) => inc <= cur,
        _ => false, // fail-open: if either version is invalid, proceed with update
    }
}

/// Sends an `AgentUpdateAck` control message, ignoring send failures.
async fn send_update_ack<S: mesh_agent_core::ControlStream>(
    conn: &mut mesh_agent_core::AgentConnection<S>,
    version: String,
    success: bool,
    error: String,
) {
    let _ = conn
        .send_control(mesh_protocol::ControlMessage::AgentUpdateAck {
            version,
            success,
            error,
        })
        .await;
}

/// Spawns the post-update watchdog task. If registration doesn't succeed
/// within 60 seconds, the watchdog rolls back to the previous binary and restarts.
fn spawn_update_watchdog(
    data_dir: &Path,
    update_config: &Option<mesh_agent_core::UpdateConfig>,
    cancel: Arc<tokio::sync::Notify>,
) {
    let data_dir = data_dir.to_path_buf();
    let binary_path = update_config
        .as_ref()
        .map(|c| c.current_binary_path.clone())
        .unwrap_or_else(|| std::env::current_exe().unwrap_or_else(|_| PathBuf::from("mesh-agent")));
    tokio::spawn(async move {
        tokio::select! {
            _ = tokio::time::sleep(std::time::Duration::from_secs(60)) => {
                error!("watchdog: registration did not complete in 60s, rolling back");
                mesh_agent_core::update::increment_rollback_count(&data_dir).await;
                match mesh_agent_core::update::rollback(&binary_path).await {
                    Ok(true) => {
                        info!("watchdog: rollback complete, restarting");
                        std::process::exit(EXIT_CODE_RESTART);
                    }
                    Ok(false) => error!("watchdog: no .prev binary to rollback to"),
                    Err(e) => error!(error = %e, "watchdog: rollback failed"),
                }
            }
            _ = cancel.notified() => {
                // Registration succeeded — watchdog cancelled.
            }
        }
    });
}

/// Returns a human-readable OS name by parsing `/etc/os-release` on Linux.
/// Falls back to `std::env::consts::OS` (e.g. "linux") on other platforms or
/// if the file cannot be read.
fn os_pretty_name() -> String {
    #[cfg(target_os = "linux")]
    {
        if let Ok(contents) = std::fs::read_to_string("/etc/os-release") {
            for line in contents.lines() {
                if let Some(value) = line.strip_prefix("PRETTY_NAME=") {
                    return value.trim_matches('"').to_string();
                }
            }
        }
    }
    std::env::consts::OS.to_string()
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

    #[test]
    fn test_should_skip_version_older() {
        // Anything older than current AGENT_VERSION should be skipped.
        assert!(should_skip_version("0.7.0"));
        assert!(should_skip_version("0.13.0"));
    }

    #[test]
    fn test_should_skip_version_same() {
        assert!(should_skip_version(env!("AGENT_VERSION")));
    }

    #[test]
    fn test_should_skip_version_newer() {
        assert!(!should_skip_version("99.0.0"));
        assert!(!should_skip_version("99.1.0"));
    }

    #[test]
    fn test_should_skip_version_invalid_semver_proceeds() {
        // Invalid semver should fail-open (proceed with update).
        assert!(!should_skip_version("not-a-version"));
        assert!(!should_skip_version(""));
    }

    #[test]
    fn test_should_skip_version_prerelease() {
        // Pre-release of a future version should not be skipped.
        assert!(!should_skip_version("99.0.0-rc.1"));
    }
}
