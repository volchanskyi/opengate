//! OpenGate mesh-agent binary.
//!
//! Connects to the server via QUIC, registers capabilities,
//! handles session requests, and applies binary updates.
//! Exit code 42 signals the service manager to restart after an update.

use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::Parser;
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
}

/// Exit code that tells systemd (RestartForceExitStatus=42) to restart the agent
/// after a successful binary update.
const EXIT_CODE_RESTART: i32 = 42;

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

    // Read server CA
    let ca_pem = tokio::fs::read_to_string(&args.server_ca)
        .await
        .context("read server CA certificate")?;

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

    let config = mesh_agent_core::AgentConfig {
        server_addr: args.server_addr,
        server_ca_pem: ca_pem,
        data_dir: args.data_dir.clone(),
    };

    // Load or create agent identity
    let identity = mesh_agent_core::AgentIdentity::load_or_create(&args.data_dir)
        .context("load agent identity")?;

    info!(device_id = %identity.device_id.0, "agent identity loaded");

    // Build update config
    let update_config = update_public_key.map(|key| mesh_agent_core::UpdateConfig {
        signing_public_key: key,
        current_binary_path: std::env::current_exe()
            .unwrap_or_else(|_| PathBuf::from("mesh-agent")),
        data_dir: args.data_dir.clone(),
    });

    // TODO: Connect via QUIC, run control loop with session dispatch and update handling.
    // For now, log the config and exit. Full QUIC connect will be wired in a follow-up.
    info!(
        device_id = %identity.device_id.0,
        server = %config.server_addr,
        updates_enabled = update_config.is_some(),
        "agent configured, QUIC control loop not yet wired"
    );

    // Placeholder: the control loop would dispatch AgentUpdate messages here.
    // On successful update, exit with code 42 for systemd restart.
    let _restart_exit_code = EXIT_CODE_RESTART;

    if let Some(_update_cfg) = &update_config {
        info!("update system ready");
    }

    // The actual QUIC connect + control loop will be implemented when
    // the agent binary is integrated with the full connection flow.
    // For now, this binary validates that all dependencies compile
    // and the CLI interface is correct.

    error!("QUIC control loop not yet implemented — exiting");
    std::process::exit(1);
}
