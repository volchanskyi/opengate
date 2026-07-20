//! OpenGate mesh-agent binary.
//!
//! Connects to the server via QUIC, registers capabilities,
//! handles session requests, and applies binary updates.
//! Exit code 42 signals the service manager to restart after an update.

mod backfill_loop;
mod edge_sentinel;
mod host_logs;
mod logs;

use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use anyhow::{Context, Result};
use clap::Parser;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha384};
use tokio::io::AsyncWriteExt;
use tracing::{debug, error, info, warn};

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

    let mut quinn_config = quinn::ClientConfig::new(Arc::new(
        quinn::crypto::rustls::QuicClientConfig::try_from(tls_config)?,
    ));

    // Match server QUIC transport config: MaxIdleTimeout=90s, KeepAlivePeriod=30s.
    let mut transport = quinn::TransportConfig::default();
    transport.max_idle_timeout(Some(
        std::time::Duration::from_secs(90)
            .try_into()
            .context("idle timeout")?,
    ));
    transport.keep_alive_interval(Some(std::time::Duration::from_secs(30)));
    quinn_config.transport_config(Arc::new(transport));

    Ok(quinn_config)
}

/// Perform the full binary handshake: send AgentHello, read ServerHello.
/// The agent opens the stream and writes first (RFC 9000 stream-discovery:
/// the opener must write before the peer's accept/read can return). Returns
/// the server's CA cert hash from ServerHello, which the agent caches to drive
/// the 0x14 fast path on subsequent reconnects.
async fn perform_full_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    cert_der: &[u8],
) -> Result<[u8; 48]> {
    // Compute agent cert SHA-384 hash
    let agent_cert_hash: [u8; 48] = Sha384::digest(cert_der).into();

    // Generate random nonce
    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).context("generate nonce")?;

    // Build and send AgentHello first.
    let agent_hello = mesh_protocol::HandshakeMessage::AgentHello {
        nonce,
        agent_cert_hash,
    };
    send.write_all(&agent_hello.encode_binary())
        .await
        .context("write AgentHello")?;
    send.flush().await.context("flush AgentHello")?;

    // Read ServerHello (81 bytes: 1 type + 32 nonce + 48 cert_hash)
    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf)
        .await
        .context("read ServerHello")?;

    let server_hello =
        mesh_protocol::HandshakeMessage::decode_binary(&hello_buf).context("decode ServerHello")?;

    match server_hello {
        mesh_protocol::HandshakeMessage::ServerHello { cert_hash, .. } => {
            info!("received ServerHello");
            info!("handshake complete");
            Ok(cert_hash)
        }
        other => anyhow::bail!("expected ServerHello, got {:?}", other),
    }
}

/// Perform the 0x14 fast-path handshake on reconnect: send SkipAuth carrying
/// the cached CA cert hash and proceed optimistically. The server replies only
/// on rejection (stale hash) by tearing the connection down, which surfaces as
/// a failure during registration so the caller falls back to a full handshake.
async fn perform_fast_handshake(
    send: &mut quinn::SendStream,
    cached_ca_hash: &[u8; 48],
) -> Result<()> {
    let skip_auth = mesh_protocol::HandshakeMessage::SkipAuth {
        cached_cert_hash: *cached_ca_hash,
    };
    send.write_all(&skip_auth.encode_binary())
        .await
        .context("write SkipAuth")?;
    send.flush().await.context("flush SkipAuth")?;
    info!("sent fast-path SkipAuth");
    Ok(())
}

/// Default log directory for persistent log files.
const LOG_DIR: &str = "/var/log/mesh-agent";

/// Bounded backlog of log-rate telemetry windows awaiting the control loop.
/// Windows beyond this are dropped so telemetry never backpressures control.
const LOG_RATE_TELEMETRY_CAP: usize = 32;

/// Bounded backlog of discovery reports awaiting the control loop. Reports are
/// change-triggered and infrequent, so a small buffer suffices; reports beyond
/// it are dropped rather than backpressuring control.
const DISCOVERY_TELEMETRY_CAP: usize = 4;

/// Bounded backlog of WS-19 threshold-alert health summaries awaiting the control
/// loop. Emission is throttled and breach-driven, so a small buffer suffices;
/// summaries beyond it are dropped rather than backpressuring control.
const HEALTH_TELEMETRY_CAP: usize = 8;

/// Hard footprint cap for the Edge-Sentinel local store, in MiB. The store
/// enforces it with coarsest-first eviction and host-free backoff, so it is a
/// coarse fleet-wide safety limit rather than a per-host tuning knob.
const EDGE_STORE_CAP_MB: u64 = 512;

/// Set up tracing with both stdout and rolling file appender.
/// Returns the guard that must be held for the lifetime of the program.
fn setup_logging() -> tracing_appender::non_blocking::WorkerGuard {
    use tracing_subscriber::layer::SubscriberExt;
    use tracing_subscriber::util::SubscriberInitExt;

    let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));

    // Rolling file appender: daily rotation, 7 files retained.
    let log_dir = PathBuf::from(LOG_DIR);
    if let Err(e) = std::fs::create_dir_all(&log_dir) {
        // Runs before the tracing subscriber is installed (`.init()` below), so
        // stderr is the only working sink — a `tracing` macro here would be dropped.
        eprintln!(
            "warning: failed to create log dir {}: {e}",
            log_dir.display()
        );
    }

    let file_appender = tracing_appender::rolling::daily(&log_dir, "agent.log");
    let (non_blocking, guard) = tracing_appender::non_blocking(file_appender);

    tracing_subscriber::registry()
        .with(env_filter)
        .with(tracing_subscriber::fmt::layer().with_writer(std::io::stdout))
        .with(
            tracing_subscriber::fmt::layer()
                .with_ansi(false)
                .with_writer(non_blocking),
        )
        .init();

    guard
}

#[tokio::main]
async fn main() -> Result<()> {
    // Parse args before initialising logging so `--help` / `--version` / a bad
    // argument exit via clap without creating the log directory or spawning the
    // appender thread.
    let args = Args::parse();

    let _log_guard = setup_logging();

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

    // Load existing identity, or enroll to get a CA-signed certificate.
    // Enrollment also writes the CA PEM to --server-ca and the update signing
    // key, so it must happen before we read those files.
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

    // Parse update public key: CLI flag takes precedence, then saved file from enrollment.
    // This runs AFTER enrollment so the signing key file exists on first boot.
    let update_public_key: Option<[u8; 32]> = match &args.update_public_key {
        Some(hex_str) => Some(parse_ed25519_pubkey(hex_str)?),
        None => {
            let key_path = args.data_dir.join("update-signing-key.hex");
            match tokio::fs::read_to_string(&key_path).await {
                Ok(hex_str) => {
                    let key = parse_ed25519_pubkey(hex_str.trim())?;
                    info!("loaded update signing key from enrollment");
                    Some(key)
                }
                Err(e) => {
                    warn!(
                        path = %key_path.display(),
                        error = %e,
                        "no update public key configured, auto-updates disabled"
                    );
                    None
                }
            }
        }
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

    // Edge-Sentinel collectors run unconditionally — every agent samples host
    // metrics, persists them to the local store, reads host log rates, and
    // auto-discovers its footprint from the start. The sampler-owned local store
    // is the sovereign copy of min/max/last + 1 s raw that central avg-only
    // VictoriaMetrics does not keep; it is shared with the WS-15 reconnect-backfill
    // coordinator on the control loop, and it opens on every start (recreating a
    // corrupt cache), degrading to log-only sampling only if even that fails.
    let shared_sink: Option<edge_sentinel::SharedSink> = {
        let path = args.data_dir.join("edge-tsdb");
        info!(path = %path.display(), cap_mb = EDGE_STORE_CAP_MB, "edge-sentinel local store");
        let cfg = edge_sentinel::StoreConfig {
            path,
            cap_bytes: EDGE_STORE_CAP_MB.saturating_mul(1024 * 1024),
        };
        edge_sentinel::open_sink(&cfg).map(|s| std::sync::Arc::new(std::sync::Mutex::new(s)))
    };

    // Maintenance-mode gate: server-authoritative desired state, cleared to
    // Active on every registration and flipped by `SetMaintenanceMode`. Each
    // collector holds a clone and suppresses its work while in maintenance; the
    // control channel and remote-management paths stay live.
    let maintenance = mesh_agent_core::maintenance::MaintenanceGate::new();

    // WS-19 threshold alerts: the sampler owns the evaluator; the control loop
    // pushes each tenant ruleset into `alert_rules_mailbox` and drains
    // breach-carrying summaries from `health_rx`.
    let alert_rules_mailbox: edge_sentinel::AlertRulesMailbox =
        std::sync::Arc::new(std::sync::Mutex::new(None));
    let (health_tx, health_rx) =
        std::sync::mpsc::sync_channel::<mesh_protocol::ControlMessage>(HEALTH_TELEMETRY_CAP);
    let _edge_sentinel_sampler = {
        info!("edge-sentinel sampler starting");
        let alerts = edge_sentinel::AlertWiring {
            rules: alert_rules_mailbox.clone(),
            health_tx,
        };
        edge_sentinel::spawn_sampler(shared_sink.clone(), Some(alerts), maintenance.clone())
    };

    // WS-15 reconnect-backfill coordinator: present only when the local store
    // opened, and its presence gates advertising the `Backfill` capability to
    // the server (there is nothing to drain without a store).
    let mut backfill = shared_sink
        .clone()
        .map(backfill_loop::BackfillCoordinator::new);

    // Log-rate windows produced by the reader task reach the control loop over a
    // bounded channel; the receiver is drained when connected (see below).
    let (log_rate_tx, log_rate_rx) =
        std::sync::mpsc::sync_channel::<mesh_protocol::ControlMessage>(LOG_RATE_TELEMETRY_CAP);
    let _edge_log_readers =
        edge_sentinel::spawn_log_readers(PathBuf::from(LOG_DIR), log_rate_tx, maintenance.clone());

    // Auto-discovery reports produced by the discovery task reach the control
    // loop over a bounded channel, drained alongside log-rate windows below.
    let (discovery_tx, discovery_rx) =
        std::sync::mpsc::sync_channel::<mesh_protocol::ControlMessage>(DISCOVERY_TELEMETRY_CAP);
    let _edge_discovery = edge_sentinel::spawn_discovery(discovery_tx, maintenance.clone());

    // Shutdown signal handler
    let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?;

    // Fast-path (0x14) reconnect state. cached_ca_hash is learned from the
    // first full handshake's ServerHello; once set, reconnects send SkipAuth
    // instead of the full exchange. force_full_next forces one full handshake
    // after an early fast-path failure (e.g. the server rotated its CA and
    // rejected the stale hash), re-validating and re-caching before the agent
    // resumes the fast path.
    let mut cached_ca_hash: Option<[u8; 48]> = None;
    let mut force_full_next = false;

    // Flap-guard: bounds the self-inflicted reconnect rate when a registered
    // connection drops shortly after registering. An accept-then-drop condition
    // would otherwise respin at the dial rate; jitter also de-synchronises a
    // reconnecting herd after a node restart.
    let mut governor = mesh_agent_core::ReconnectGovernor::new();
    let mut reconnect_rng = rand::rng();

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
                    // Agent opens the bidirectional control stream and writes first.
                    let (send, recv) = conn
                        .open_bi()
                        .await
                        .map_err(|e| format!("open bi stream: {e}"))?;
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

        // Choose the fast path (0x14) when we hold a cached CA hash and the
        // last attempt wasn't an early fast-path rejection.
        let used_fast_path = cached_ca_hash.is_some() && !force_full_next;
        let handshake = if used_fast_path {
            perform_fast_handshake(&mut send, &cached_ca_hash.unwrap())
                .await
                .map(|()| None)
        } else {
            perform_full_handshake(&mut send, &mut recv, &identity.cert_der)
                .await
                .map(Some)
        };
        match handshake {
            Ok(Some(ca_hash)) => {
                // Full handshake succeeded — (re)cache the CA hash and clear
                // any prior fast-path failure.
                cached_ca_hash = Some(ca_hash);
                force_full_next = false;
            }
            Ok(None) => {} // fast path: keep the existing cached hash
            Err(e) => {
                warn!(error = %e, "handshake failed, will reconnect");
                if used_fast_path {
                    force_full_next = true;
                }
                continue;
            }
        }

        // Wrap QUIC streams into AsyncControlStream
        let stream = mesh_agent_core::AsyncControlStream::new(tokio::io::join(recv, send));
        let mut conn = mesh_agent_core::AgentConnection::new(stream);

        // Register with server. Discovery and threshold alerts are always-on, so
        // they are advertised on every registration; Backfill is advertised only
        // when the local store opened (see `agent_capabilities`).
        let capabilities = agent_capabilities(backfill.is_some());
        if let Err(e) = conn
            .send_control(mesh_protocol::ControlMessage::AgentRegister {
                capabilities,
                hostname: gethostname::gethostname().to_string_lossy().to_string(),
                os: os_pretty_name(),
                arch: std::env::consts::ARCH.to_string(),
                version: env!("AGENT_VERSION").to_string(),
            })
            .await
        {
            // A fast-path connection that fails at registration was likely
            // rejected (stale CA hash); fall back to a full handshake next time.
            warn!(error = %e, "failed to send AgentRegister, will reconnect");
            if used_fast_path {
                force_full_next = true;
            }
            continue;
        }

        // Registration succeeded: the fast path (if used) is validated.
        force_full_next = false;
        // Reset maintenance to Active on every registration. The server pushes
        // `SetMaintenanceMode(true)` right after register only for a device that
        // is currently in maintenance; an Active device gets no message and must
        // therefore default to Active here.
        maintenance.set(false);
        // Stamp when this session became registered so the flap-guard at the
        // bottom of the loop can tell a stable session from an instant drop.
        let connected_at = std::time::Instant::now();
        info!(
            fast_path = used_fast_path,
            "registered with server, entering control loop"
        );

        // Registration succeeded — cancel watchdog and clear sentinel.
        if pending_update {
            watchdog_cancel.notify_one();
            mesh_agent_core::update::clear_update_pending(&args.data_dir).await;
            mesh_agent_core::update::reset_rollback_count(&args.data_dir).await;
            info!("post-update verification passed, sentinel cleared");
        }

        // WS-15: start a reconnect-backfill cycle for this session. `bf_send_at`
        // paces the drain to the granted rate; `bf_retry_at` schedules a
        // deferred re-request. Both stay `None` until a grant/defer arrives. A
        // send failure here surfaces again on the next control send and drives a
        // reconnect, so it is only logged.
        let mut bf_send_at: Option<tokio::time::Instant> = None;
        let mut bf_retry_at: Option<tokio::time::Instant> = None;
        if let Some(bf) = backfill.as_mut() {
            if let Some(msg) = bf.start_session().await {
                match conn.send_control(msg).await {
                    Ok(()) => debug!("backfill: requested admission slot"),
                    Err(e) => warn!(error = %e, "backfill slot request failed"),
                }
            }
        }

        // Control loop — dispatch messages until disconnect
        let mut heartbeat = tokio::time::interval(std::time::Duration::from_secs(60));
        heartbeat.tick().await; // consume immediate first tick
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
                _ = heartbeat.tick() => {
                    let now = std::time::SystemTime::now()
                        .duration_since(std::time::UNIX_EPOCH)
                        .map(|d| d.as_secs() as i64)
                        .unwrap_or(0);
                    if let Err(e) = conn.send_control(
                        mesh_protocol::ControlMessage::AgentHeartbeat { timestamp: now },
                    ).await {
                        warn!(error = %e, "heartbeat failed, will reconnect");
                        break;
                    }
                    tracing::debug!("heartbeat sent");
                    if backfill.as_ref().is_some_and(backfill_loop::BackfillCoordinator::is_draining) {
                        tracing::debug!("backfill: reconnect drain in progress");
                    }

                    // Forward any queued log-rate windows and discovery reports.
                    // Drain into a Vec first so no receiver is held across the
                    // send await; the bounded channels already dropped anything
                    // beyond capacity, so neither can backpressure the control
                    // stream.
                    let mut windows: Vec<mesh_protocol::ControlMessage> =
                        std::iter::from_fn(|| log_rate_rx.try_recv().ok()).collect();
                    windows.extend(std::iter::from_fn(|| discovery_rx.try_recv().ok()));
                    windows.extend(std::iter::from_fn(|| health_rx.try_recv().ok()));
                    let mut telemetry_lost = false;
                    for window in windows {
                        if let Err(e) = conn.send_control(window).await {
                            warn!(error = %e, "log-rate telemetry send failed, will reconnect");
                            telemetry_lost = true;
                            break;
                        }
                    }
                    if telemetry_lost {
                        break;
                    }
                }
                _ = sleep_until_opt(bf_send_at) => {
                    // Paced backfill send: one batch (or a re-slot request when the
                    // grant expired mid-drain). The next send is re-armed on the
                    // matching ack; a send failure drives a reconnect.
                    bf_send_at = None;
                    if let Some(bf) = backfill.as_mut() {
                        if let Some(msg) = bf.next_batch().await {
                            if let Err(e) = conn.send_control(msg).await {
                                warn!(error = %e, "backfill send failed, will reconnect");
                                break;
                            }
                        }
                    }
                }
                _ = sleep_until_opt(bf_retry_at) => {
                    // A deferred admission slot's jittered retry has elapsed.
                    bf_retry_at = None;
                    if let Some(bf) = backfill.as_mut() {
                        if let Some(msg) = bf.start_session().await {
                            if let Err(e) = conn.send_control(msg).await {
                                warn!(error = %e, "backfill retry request failed, will reconnect");
                                break;
                            }
                        }
                    }
                }
                msg = conn.receive_control() => {
                    match msg {
                        Ok(mesh_protocol::ControlMessage::SessionRequest {
                            token, relay_url, permissions,
                        }) => {
                            let capture: Box<dyn mesh_agent_core::ScreenCapture> =
                                Box::new(mesh_agent_core::NullCapture);
                            let injector: Box<dyn mesh_agent_core::InputInjector> =
                                Box::new(mesh_agent_core::NullInput);
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
                        Ok(mesh_protocol::ControlMessage::RestartAgent { reason }) => {
                            info!(reason, "restart requested by server, exiting with code 42");
                            lifecycle.notify_stopping();
                            std::process::exit(EXIT_CODE_RESTART);
                        }
                        Ok(mesh_protocol::ControlMessage::RequestHardwareReport) => {
                            info!("hardware report requested by server");
                            match std::panic::catch_unwind(collect_hardware_info) {
                                Ok(report) => {
                                    if let Err(e) = conn.send_control(report).await {
                                        warn!(error = %e, "failed to send hardware report");
                                    }
                                }
                                Err(_) => {
                                    let msg = mesh_protocol::ControlMessage::HardwareReportError {
                                        error: "failed to collect hardware info".to_string(),
                                    };
                                    if let Err(e) = conn.send_control(msg).await {
                                        warn!(error = %e, "failed to send hardware report error");
                                    }
                                }
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::RequestDeviceLogs {
                            log_level,
                            time_from,
                            time_to,
                            search,
                            log_offset,
                            log_limit,
                            // This path returns the agent's own log files; the
                            // host-source selector and unit filter are not
                            // consumed here.
                            ..
                        }) => {
                            info!("device logs requested by server");
                            let collector = logs::LogCollector::new(PathBuf::from(LOG_DIR));
                            let filter = logs::LogFilter {
                                level: if log_level.is_empty() { None } else { Some(log_level) },
                                time_from: if time_from.is_empty() { None } else { Some(time_from) },
                                time_to: if time_to.is_empty() { None } else { Some(time_to) },
                                search: if search.is_empty() { None } else { Some(search) },
                                offset: log_offset,
                                limit: log_limit,
                            };
                            match collector.collect(&filter) {
                                Ok(mut result) => {
                                    // Edge-side redaction is the first of two
                                    // independent guards on secret-dense raw lines.
                                    host_logs::redact_entries(&mut result.entries);
                                    let msg = mesh_protocol::ControlMessage::DeviceLogsResponse {
                                        log_entries: result.entries,
                                        total_count: result.total_count,
                                        has_more: result.has_more,
                                    };
                                    if let Err(e) = conn.send_control(msg).await {
                                        warn!(error = %e, "failed to send device logs response");
                                    }
                                }
                                Err(e) => {
                                    let msg = mesh_protocol::ControlMessage::DeviceLogsError {
                                        error: e.to_string(),
                                    };
                                    if let Err(e) = conn.send_control(msg).await {
                                        warn!(error = %e, "failed to send device logs error");
                                    }
                                }
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::GrantBackfill { rate, deadline }) => {
                            if let Some(bf) = backfill.as_mut() {
                                info!(rate, deadline, "backfill: admission slot granted");
                                bf.on_grant(rate, deadline);
                                bf_send_at = Some(tokio::time::Instant::now());
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::DeferBackfill { retry_after }) => {
                            if let Some(bf) = backfill.as_mut() {
                                let delay = bf.on_defer(retry_after);
                                debug!(retry_after, delay_s = delay.as_secs(), "backfill: deferred");
                                bf_retry_at = Some(tokio::time::Instant::now() + delay);
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::MetricBackfillAck { tier, cursor }) => {
                            if let Some(bf) = backfill.as_mut() {
                                let delay = bf.on_ack(tier, cursor).await;
                                bf_send_at = Some(tokio::time::Instant::now() + delay);
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::RequestLocalHistory {
                            dim, from_ts, to_ts, max_points,
                        }) => {
                            if let Some(bf) = backfill.as_ref() {
                                let resp = bf.answer_history(dim, from_ts, to_ts, max_points).await;
                                if let Err(e) = conn.send_control(resp).await {
                                    warn!(error = %e, "failed to send local-history response");
                                }
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::SetMaintenanceMode { enabled }) => {
                            // Server-authoritative desired state. Flip the gate the
                            // collectors consult, then echo the applied state so the
                            // server can track applied vs. desired convergence.
                            maintenance.set(enabled);
                            info!(enabled, "maintenance mode set by server");
                            if let Err(e) = conn.send_control(
                                mesh_protocol::ControlMessage::MaintenanceApplied { enabled },
                            ).await {
                                warn!(error = %e, "failed to send MaintenanceApplied, will reconnect");
                                break;
                            }
                        }
                        Ok(mesh_protocol::ControlMessage::PushAlertRules { rules }) => {
                            // WS-19: hand the tenant ruleset to the sampler's
                            // evaluator via the shared mailbox (next tick installs it).
                            debug!(count = rules.len(), "edge-sentinel: threshold-alert ruleset received");
                            if let Ok(mut slot) = alert_rules_mailbox.lock() {
                                *slot = Some(rules);
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

        // The control loop broke (connection lost). If the session dropped
        // within the stability window, back off before reconnecting so an
        // accept-then-drop condition cannot respin at the dial rate; a session
        // that stayed up resets the backoff and reconnects immediately.
        if let Some(delay) = governor.record_disconnect(connected_at.elapsed(), &mut reconnect_rng)
        {
            warn!(
                delay_ms = delay.as_millis(),
                flap_count = governor.flap_count(),
                "connection dropped shortly after registering, backing off before reconnect"
            );
            tokio::time::sleep(delay).await;
        }
    }

    lifecycle.notify_stopping();
    info!("mesh-agent stopped");
    Ok(())
}

/// Sleep until `at`, or never (pend forever) when `at` is `None`. Lets a
/// `tokio::select!` arm hold an optional backfill pacing/retry deadline without a
/// dedicated always-armed timer.
async fn sleep_until_opt(at: Option<tokio::time::Instant>) {
    match at {
        Some(t) => tokio::time::sleep_until(t).await,
        None => std::future::pending::<()>().await,
    }
}

/// Collects hardware inventory from the local system.
fn collect_hardware_info() -> mesh_protocol::ControlMessage {
    use sysinfo::{Disks, System};

    let sys = System::new_all();

    let cpu_model = sys
        .cpus()
        .first()
        .map(|c| c.brand().to_string())
        .unwrap_or_default();
    let cpu_cores = sys.cpus().len() as u32;
    let ram_total_mb = sys.total_memory() / (1024 * 1024);

    let disks = Disks::new_with_refreshed_list();
    let (disk_total, disk_free) = disks.iter().fold((0u64, 0u64), |(t, f), d| {
        (t + d.total_space(), f + d.available_space())
    });

    let network_interfaces = collect_network_interfaces();

    mesh_protocol::ControlMessage::HardwareReport {
        cpu_model,
        cpu_cores,
        ram_total_mb,
        disk_total_mb: disk_total / (1024 * 1024),
        disk_free_mb: disk_free / (1024 * 1024),
        network_interfaces,
    }
}

/// Collects network interface information using libc getifaddrs.
fn collect_network_interfaces() -> Vec<mesh_protocol::NetworkInterface> {
    use std::collections::HashMap;
    use std::ffi::CStr;
    use std::net::{Ipv4Addr, Ipv6Addr};

    let mut interfaces: HashMap<String, mesh_protocol::NetworkInterface> = HashMap::new();

    unsafe {
        let mut ifaddrs: *mut libc::ifaddrs = std::ptr::null_mut();
        if libc::getifaddrs(&mut ifaddrs) != 0 {
            return Vec::new();
        }

        let mut current = ifaddrs;
        while !current.is_null() {
            let ifa = &*current;
            let name = CStr::from_ptr(ifa.ifa_name).to_string_lossy().to_string();

            let entry =
                interfaces
                    .entry(name.clone())
                    .or_insert_with(|| mesh_protocol::NetworkInterface {
                        name: name.clone(),
                        mac: String::new(),
                        ipv4: Vec::new(),
                        ipv6: Vec::new(),
                    });

            if !ifa.ifa_addr.is_null() {
                let family = (*ifa.ifa_addr).sa_family as i32;
                if family == libc::AF_INET {
                    let addr = &*(ifa.ifa_addr as *const libc::sockaddr_in);
                    let ip = Ipv4Addr::from(u32::from_be(addr.sin_addr.s_addr));
                    entry.ipv4.push(ip.to_string());
                } else if family == libc::AF_INET6 {
                    let addr = &*(ifa.ifa_addr as *const libc::sockaddr_in6);
                    let ip = Ipv6Addr::from(addr.sin6_addr.s6_addr);
                    entry.ipv6.push(ip.to_string());
                } else if family == libc::AF_PACKET {
                    let addr = &*(ifa.ifa_addr as *const libc::sockaddr_ll);
                    if addr.sll_halen == 6 {
                        entry.mac = format!(
                            "{:02x}:{:02x}:{:02x}:{:02x}:{:02x}:{:02x}",
                            addr.sll_addr[0],
                            addr.sll_addr[1],
                            addr.sll_addr[2],
                            addr.sll_addr[3],
                            addr.sll_addr[4],
                            addr.sll_addr[5],
                        );
                    }
                }
            }

            current = ifa.ifa_next;
        }

        libc::freeifaddrs(ifaddrs);
    }

    interfaces.into_values().collect()
}

fn uninstall_agent(data_dir: &std::path::Path) {
    const SERVICE_NAME: &str = "mesh-agent";
    const CONFIG_DIR: &str = "/etc/opengate-agent";
    const INSTALL_DIR: &str = "/usr/local/bin";
    const BINARY_NAME: &str = "mesh-agent";

    stop_systemd_service(SERVICE_NAME);
    remove_identity_files(data_dir);
    remove_dirs(&[data_dir.to_path_buf(), std::path::PathBuf::from(CONFIG_DIR)]);
    remove_systemd_unit(SERVICE_NAME);
    daemon_reload();
    remove_binary(INSTALL_DIR, BINARY_NAME);

    info!("agent uninstalled: service stopped, files removed");
}

fn stop_systemd_service(name: &str) {
    use std::process::Command;
    for action in &["stop", "disable"] {
        if let Err(e) = Command::new("systemctl").args([action, name]).output() {
            warn!(action, error = %e, "systemctl command failed");
        }
    }
}

fn remove_identity_files(data_dir: &std::path::Path) {
    let identity_files = [
        mesh_agent_core::DEVICE_ID_FILE,
        mesh_agent_core::CERT_FILE,
        mesh_agent_core::KEY_FILE,
        "server_ca.pem",
    ];
    for filename in &identity_files {
        try_remove_file(&data_dir.join(filename), "identity file");
    }
}

fn remove_dirs(dirs: &[std::path::PathBuf]) {
    for dir in dirs {
        if dir.exists() {
            if let Err(e) = std::fs::remove_dir_all(dir) {
                warn!(dir = %dir.display(), error = %e, "failed to remove directory");
            }
        }
    }
}

fn remove_systemd_unit(name: &str) {
    let path = std::path::PathBuf::from(format!("/etc/systemd/system/{name}.service"));
    try_remove_file(&path, "service file");
}

fn daemon_reload() {
    use std::process::Command;
    // Non-systemd hosts will fail here harmlessly — log at debug.
    match Command::new("systemctl").arg("daemon-reload").output() {
        Ok(out) if !out.status.success() => {
            debug!(
                stderr = %String::from_utf8_lossy(&out.stderr),
                "systemctl daemon-reload exited non-zero"
            );
        }
        Err(e) => debug!(error = %e, "systemctl daemon-reload not available"),
        _ => {}
    }
}

fn remove_binary(install_dir: &str, name: &str) {
    let path = std::path::PathBuf::from(install_dir).join(name);
    try_remove_file(&path, "binary");
}

fn try_remove_file(path: &std::path::Path, kind: &str) {
    if path.exists() {
        if let Err(e) = std::fs::remove_file(path) {
            warn!(file = %path.display(), error = %e, kind, "failed to remove file");
        }
    }
}

/// Decode a hex-encoded Ed25519 public key into a 32-byte array.
fn parse_ed25519_pubkey(hex_str: &str) -> Result<[u8; 32]> {
    let bytes = hex::decode(hex_str).context("decode Ed25519 public key hex")?;
    bytes.try_into().map_err(|v: Vec<u8>| {
        anyhow::anyhow!("Ed25519 public key must be 32 bytes, got {}", v.len())
    })
}

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
    if let Err(e) = conn
        .send_control(mesh_protocol::ControlMessage::AgentUpdateAck {
            version,
            success,
            error,
        })
        .await
    {
        warn!(error = %e, "failed to send AgentUpdateAck (server may be disconnected)");
    }
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

/// Capabilities the agent advertises on registration. Terminal, file management,
/// hardware inventory, device logs, discovery, and threshold alerts are always-on
/// capabilities of every agent. `Backfill` is advertised only when the local
/// store opened (`has_local_store`), since the reconnect-backfill coordinator has
/// nothing to drain without it.
fn agent_capabilities(has_local_store: bool) -> Vec<mesh_protocol::AgentCapability> {
    use mesh_protocol::AgentCapability;
    let mut caps = vec![
        AgentCapability::Terminal,
        AgentCapability::FileManager,
        AgentCapability::HardwareInventory,
        AgentCapability::DeviceLogs,
        AgentCapability::Discovery,
        AgentCapability::ThresholdAlerts,
    ];
    if has_local_store {
        caps.push(AgentCapability::Backfill);
    }
    caps
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
    fn agent_capabilities_always_advertise_edge_sentinel() {
        // Discovery and threshold alerts are always-on — no opt-in gate — and
        // the baseline capabilities are always present.
        let caps = agent_capabilities(false);
        assert!(caps.contains(&mesh_protocol::AgentCapability::Discovery));
        assert!(caps.contains(&mesh_protocol::AgentCapability::ThresholdAlerts));
        assert!(caps.contains(&mesh_protocol::AgentCapability::Terminal));
        assert!(caps.contains(&mesh_protocol::AgentCapability::FileManager));
        assert!(caps.contains(&mesh_protocol::AgentCapability::HardwareInventory));
        assert!(caps.contains(&mesh_protocol::AgentCapability::DeviceLogs));
        // Backfill is withheld when the local store did not open.
        assert!(!caps.contains(&mesh_protocol::AgentCapability::Backfill));
    }

    #[test]
    fn agent_capabilities_advertise_backfill_only_with_local_store() {
        let caps = agent_capabilities(true);
        assert!(caps.contains(&mesh_protocol::AgentCapability::Backfill));
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

    #[test]
    fn test_parse_ed25519_pubkey_valid() {
        let hex = "a".repeat(64); // 32 bytes as hex
        let key = parse_ed25519_pubkey(&hex).unwrap();
        assert_eq!(key, [0xaa; 32]);
    }

    #[test]
    fn test_parse_ed25519_pubkey_wrong_length() {
        let hex = "aa".repeat(16); // 16 bytes, not 32
        let err = parse_ed25519_pubkey(&hex).unwrap_err();
        assert!(err.to_string().contains("32 bytes"));
    }

    #[test]
    fn test_parse_ed25519_pubkey_invalid_hex() {
        let err = parse_ed25519_pubkey("not-hex").unwrap_err();
        assert!(err.to_string().contains("hex"));
    }

    #[test]
    fn test_parse_ed25519_pubkey_empty() {
        let key = parse_ed25519_pubkey("").unwrap_err();
        assert!(key.to_string().contains("32 bytes"));
    }
}
