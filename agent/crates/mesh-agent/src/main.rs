//! Agent binary: connects to the server over QUIC, registers capabilities, serves sessions and
//! applies updates; exit code 42 asks the service manager to restart.

mod backfill_loop;
mod cert_renewal;
mod client_tls;
mod clock;
mod control_loop;
mod device_logs;
mod edge_sentinel;
mod enrollment;
mod event_watch;
mod handshake;
mod hardware_report;
mod host_logs;
mod lifecycle;
mod logs;
mod requests;
mod retro_job;
mod stop;

use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use anyhow::{Context, Result};
use clap::Parser;
use tracing::{debug, error, info, warn};

/// OpenGate mesh agent.
#[derive(Parser, Debug)]
#[command(version, about = "OpenGate mesh agent")]
struct Args {
    /// Server address (host:port) for QUIC connection.
    #[arg(long, env = "OPENGATE_SERVER_ADDR")]
    server_addr: String,

    /// Path to the PEM bundle of master keys the agent trusts.
    #[arg(long, env = "OPENGATE_SERVER_CA")]
    server_ca: PathBuf,

    /// Data directory for identity, keys, the install note, and temporary files.
    #[arg(long, default_value = "/var/lib/mesh-agent", env = "OPENGATE_DATA_DIR")]
    data_dir: PathBuf,

    /// Directory for the agent's rotated log files, which the server reads back.
    #[arg(long, default_value = LOG_DIR, env = "OPENGATE_LOG_DIR")]
    log_dir: PathBuf,

    /// Ed25519 public key hex for verifying update signatures (optional).
    #[arg(long, env = "OPENGATE_UPDATE_PUBLIC_KEY")]
    update_public_key: Option<String>,
}

/// Exit code that tells systemd (RestartForceExitStatus=42) to restart the agent, after a binary
/// update or a new identity.
pub(crate) const EXIT_CODE_RESTART: i32 = 42;

/// Default log directory for persistent log files.
const LOG_DIR: &str = "/var/log/mesh-agent";

/// Backlog of discovery reports awaiting the control loop; reports beyond it are dropped.
const DISCOVERY_TELEMETRY_CAP: usize = 4;

/// Backlog of threshold-alert health summaries awaiting the control loop; extras are dropped.
const HEALTH_TELEMETRY_CAP: usize = 8;

/// Backlog of host-metric windows (one per 60 s, drained every 60 s); extras are dropped.
const HOST_METRIC_TELEMETRY_CAP: usize = 16;

/// Footprint cap for the local store in MiB, enforced by coarsest-first eviction.
const EDGE_STORE_CAP_MB: u64 = 512;

/// How long a stop waits for the background tasks, inside the unit's 15 s `TimeoutStopSec`.
const BACKGROUND_GRACE: std::time::Duration = std::time::Duration::from_secs(3);

/// How long the runtime waits for blocking work still running after the background grace.
const RUNTIME_GRACE: std::time::Duration = std::time::Duration::from_secs(1);

/// Sets up tracing on stdout plus a daily-rotated file when the log directory is writable.
/// Returns the file guard to hold for the program lifetime, or `None` for stdout-only logging.
fn setup_logging(log_dir: &Path) -> Option<tracing_appender::non_blocking::WorkerGuard> {
    use tracing_subscriber::layer::SubscriberExt;
    use tracing_subscriber::util::SubscriberInitExt;

    let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));
    let stdout_layer = tracing_subscriber::fmt::layer().with_writer(std::io::stdout);

    // The `agent.log` prefix is how the device-log collector finds files; the builder reports an
    // unusable directory where `rolling::daily` panics.
    let appender = std::fs::create_dir_all(log_dir)
        .map_err(|e| format!("create it: {e}"))
        .and_then(|()| {
            tracing_appender::rolling::RollingFileAppender::builder()
                .rotation(tracing_appender::rolling::Rotation::DAILY)
                .filename_prefix("agent.log")
                .build(log_dir)
                .map_err(|e| format!("open a log file in it: {e}"))
        });

    match appender {
        Ok(file_appender) => {
            let (non_blocking, guard) = tracing_appender::non_blocking(file_appender);
            tracing_subscriber::registry()
                .with(env_filter)
                .with(stdout_layer)
                .with(
                    tracing_subscriber::fmt::layer()
                        .with_ansi(false)
                        .with_writer(non_blocking),
                )
                .init();
            Some(guard)
        }
        Err(why) => {
            tracing_subscriber::registry()
                .with(env_filter)
                .with(stdout_layer)
                .init();
            warn!(
                log_dir = %log_dir.display(),
                "logging to stdout only: could not {why}"
            );
            None
        }
    }
}

fn main() -> Result<()> {
    // Parsing before logging setup lets `--help`, `--version` and bad arguments exit without
    // creating the log directory.
    let args = Args::parse();

    let _log_guard = setup_logging(&args.log_dir);

    let runtime = tokio::runtime::Runtime::new().context("start the async runtime")?;
    let result = runtime.block_on(run(args));
    // A blocking call still running past the grace is abandoned, so it cannot hold the exit.
    runtime.shutdown_timeout(RUNTIME_GRACE);
    result
}

async fn run(args: Args) -> Result<()> {
    info!(
        version = env!("AGENT_VERSION"),
        server_addr = %args.server_addr,
        data_dir = %args.data_dir.display(),
        "mesh-agent starting"
    );

    // The data directory is owner-only because it holds the device's mTLS private key.
    mesh_agent_core::ensure_private_dir(&args.data_dir).context("create data directory")?;

    let paths = lifecycle::Paths {
        data_dir: args.data_dir.clone(),
        server_ca: args.server_ca.clone(),
    };
    // An interrupted save is settled before anything reads the identity.
    mesh_agent_core::identity_store::recover(&paths.data_dir, &paths.staged_targets())
        .context("recover an interrupted identity save")?;
    let identity = match lifecycle::start_up(&paths).await? {
        lifecycle::StartUp::Remove => lifecycle::self_remove(&paths),
        lifecycle::StartUp::Connect(identity) => identity,
    };

    let update_public_key = update_public_key(&args).await?;
    info!(device_id = %identity.device_id.0, "agent identity loaded");

    let ca_pem = tokio::fs::read_to_string(&args.server_ca)
        .await
        .context("read the trusted master keys")?;
    let tls = client_tls::ClientTls::load(&identity, &ca_pem)?;

    let update_config = update_public_key.map(|key| mesh_agent_core::UpdateConfig {
        signing_public_key: key,
        current_binary_path: std::env::current_exe()
            .unwrap_or_else(|_| PathBuf::from("mesh-agent")),
        data_dir: args.data_dir.clone(),
        download: mesh_agent_core::update::DownloadLimits::default(),
    });
    let pending_update = start_update_watchdog(&args.data_dir, &update_config).await;

    let service = platform_linux::create_service_lifecycle();
    let endpoint = quinn::Endpoint::client("0.0.0.0:0".parse::<SocketAddr>()?)?;
    // The stop request is heard from readiness on, so one arriving while background work starts
    // ends the agent cleanly.
    let sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?;
    service.notify_ready();
    info!("agent ready, connecting to server");

    let background = spawn_background(&args.data_dir);
    let Background {
        telemetry,
        shared,
        backfill,
        stop,
        tasks,
    } = background;
    let agent = control_loop::Agent {
        paths,
        server_addr: args.server_addr,
        log_dir: args.log_dir,
        identity,
        tls,
        endpoint,
        service,
        update_config: update_config.map(std::sync::Arc::new),
        pending_update,
        telemetry,
        shared,
        backfill,
        renewal: cert_renewal::Renewal::new(),
        requests: requests::Requests::default(),
        cached_ca_hash: None,
        force_full_next: false,
    };
    let result = control_loop::run(agent, sigterm).await;
    stop_background(&stop, tasks).await;
    result
}

/// Asks the background tasks to stop and waits for them up to [`BACKGROUND_GRACE`].
async fn stop_background(stop: &stop::Stop, tasks: Vec<tokio::task::JoinHandle<()>>) {
    stop.request();
    let total = tasks.len();
    let joined = tokio::time::timeout(BACKGROUND_GRACE, async {
        for task in tasks {
            if let Err(e) = task.await {
                warn!(error = %e, "a background task ended abnormally");
            }
        }
    })
    .await;
    match joined {
        Ok(()) => info!(tasks = total, "background tasks stopped"),
        Err(_) => warn!(
            grace = ?BACKGROUND_GRACE,
            "background tasks still running at exit, leaving them"
        ),
    }
}

/// The update-signing key: the flag, else the key enrollment saved, else none.
async fn update_public_key(args: &Args) -> Result<Option<[u8; 32]>> {
    if let Some(hex_str) = &args.update_public_key {
        return parse_ed25519_pubkey(hex_str).map(Some);
    }
    let key_path = args.data_dir.join(lifecycle::UPDATE_SIGNING_KEY_FILE);
    match tokio::fs::read_to_string(&key_path).await {
        Ok(hex_str) => {
            let key = parse_ed25519_pubkey(hex_str.trim())?;
            info!("loaded update signing key from enrollment");
            Ok(Some(key))
        }
        Err(e) => {
            warn!(
                path = %key_path.display(),
                error = %e,
                "no update public key configured, auto-updates disabled"
            );
            Ok(None)
        }
    }
}

/// Starts the post-update watchdog when an update sentinel is present; it rolls back and restarts
/// unless the server acknowledges the agent within 60 seconds.
async fn start_update_watchdog(
    data_dir: &Path,
    update_config: &Option<mesh_agent_core::UpdateConfig>,
) -> Option<control_loop::PendingUpdate> {
    if !mesh_agent_core::update::is_update_pending(data_dir) {
        return None;
    }
    let count = mesh_agent_core::update::rollback_count(data_dir).await;
    if count >= mesh_agent_core::update::MAX_ROLLBACKS {
        error!(
            count,
            "max rollback attempts reached, leaving current binary in place"
        );
        mesh_agent_core::update::clear_update_pending(data_dir).await;
        mesh_agent_core::update::reset_rollback_count(data_dir).await;
        return None;
    }
    info!(
        count,
        "post-update watchdog active, will rollback unless acknowledged within 60s"
    );
    let watchdog_cancel = Arc::new(tokio::sync::Notify::new());
    spawn_update_watchdog(data_dir, update_config, watchdog_cancel.clone());
    Some(control_loop::PendingUpdate { watchdog_cancel })
}

/// The background tasks' handles the control loop needs, and what stops them.
struct Background {
    telemetry: control_loop::Telemetry,
    shared: control_loop::Shared,
    backfill: Option<backfill_loop::BackfillCoordinator>,
    stop: stop::Stop,
    tasks: Vec<tokio::task::JoinHandle<()>>,
}

/// Starts the sampler, discovery, event watch and retro scans over the local store.
fn spawn_background(data_dir: &Path) -> Background {
    // The local store holds min/max/last and 1 s raw, which central keeps only as averages; the
    // backfill coordinator shares it, and sampling continues without persistence if it cannot open.
    let shared_sink: Option<edge_sentinel::SharedSink> = {
        let path = data_dir.join("edge-tsdb");
        info!(path = %path.display(), cap_mb = EDGE_STORE_CAP_MB, "edge-sentinel local store");
        let cfg = edge_sentinel::StoreConfig {
            path,
            cap_bytes: EDGE_STORE_CAP_MB.saturating_mul(1024 * 1024),
        };
        edge_sentinel::open_sink(&cfg).map(|s| Arc::new(std::sync::Mutex::new(s)))
    };

    // Collectors hold clones of this gate and pause while it reads maintenance; the control
    // channel stays live.
    let maintenance = mesh_agent_core::maintenance::MaintenanceGate::new();
    // The sampler owns the evaluator; the control loop installs rulesets through this mailbox.
    let alert_rules_mailbox: edge_sentinel::AlertRulesMailbox =
        Arc::new(std::sync::Mutex::new(None));
    // The sampler, event watch and retro scan share one queue, so one bound and one hourly
    // allowance cover the machine.
    let alert_sink = mesh_agent_core::alerts::AlertSink::default();
    // The event watch publishes which rules its log reader can answer; the sampler reports them.
    let event_coverage: event_watch::EventCoverage = Arc::new(std::sync::Mutex::new(Vec::new()));
    let (health_tx, health_rx) =
        std::sync::mpsc::sync_channel::<mesh_protocol::ControlMessage>(HEALTH_TELEMETRY_CAP);
    // Host-metric windows reach the control loop over a bounded channel drained on the heartbeat.
    let (host_metric_tx, host_metric_rx) =
        std::sync::mpsc::sync_channel::<mesh_protocol::ControlMessage>(HOST_METRIC_TELEMETRY_CAP);
    // The sampler publishes host load each second for background work that needs an idle machine.
    let host_load = edge_sentinel::LoadSignal::new();
    let stop = stop::Stop::new();
    let mut tasks = Vec::with_capacity(4);
    info!("edge-sentinel sampler starting");
    tasks.push(edge_sentinel::spawn_sampler(
        edge_sentinel::SamplerOutputs {
            sink: shared_sink.clone(),
            alerts: Some(edge_sentinel::AlertWiring {
                rules: alert_rules_mailbox.clone(),
                health_tx,
                alert_sink: alert_sink.clone(),
                event_coverage: event_coverage.clone(),
            }),
            host_metric_tx: Some(host_metric_tx),
            load: host_load.clone(),
        },
        maintenance.clone(),
        stop.clone(),
    ));

    let (discovery_tx, discovery_rx) =
        std::sync::mpsc::sync_channel::<mesh_protocol::ControlMessage>(DISCOVERY_TELEMETRY_CAP);
    tasks.push(edge_sentinel::spawn_discovery(
        discovery_tx,
        maintenance.clone(),
        stop.clone(),
    ));
    tasks.push(event_watch::spawn_event_watch(
        alert_sink.clone(),
        maintenance.clone(),
        event_coverage,
        stop.clone(),
    ));

    // Retro scans compare against the ruleset the control loop last installed, so a scan stops
    // when its own rule version is replaced.
    let installed_rules: retro_job::InstalledRules = Arc::new(std::sync::Mutex::new(Vec::new()));
    let store_config = shared_sink
        .as_ref()
        .and_then(|sink| sink.lock().ok().map(|sink| sink.config()))
        .unwrap_or_default();
    tasks.push(retro_job::spawn_retro_scans(retro_job::RetroWiring {
        sink: alert_sink.clone(),
        rules: installed_rules.clone(),
        store: shared_sink.clone(),
        maintenance: maintenance.clone(),
        load: host_load,
        data_dir: data_dir.to_path_buf(),
        store_config,
        stop: stop.clone(),
    }));

    Background {
        telemetry: control_loop::Telemetry {
            health_rx,
            host_metric_rx,
            discovery_rx,
        },
        shared: control_loop::Shared {
            alert_sink,
            alert_rules_mailbox,
            installed_rules,
            maintenance,
        },
        // The backfill coordinator exists only when the local store opened, and gates the
        // `Backfill` capability.
        backfill: shared_sink.map(backfill_loop::BackfillCoordinator::new),
        stop,
        tasks,
    }
}

/// Decode a hex-encoded Ed25519 public key into a 32-byte array.
fn parse_ed25519_pubkey(hex_str: &str) -> Result<[u8; 32]> {
    let bytes = hex::decode(hex_str).context("decode Ed25519 public key hex")?;
    bytes.try_into().map_err(|v: Vec<u8>| {
        anyhow::anyhow!("Ed25519 public key must be 32 bytes, got {}", v.len())
    })
}

/// Skips an incoming semver at or below `AGENT_VERSION`; a version that fails to parse proceeds.
pub(crate) fn should_skip_version(incoming: &str) -> bool {
    let current = env!("AGENT_VERSION");
    match (
        semver::Version::parse(current),
        semver::Version::parse(incoming),
    ) {
        (Ok(cur), Ok(inc)) => inc <= cur,
        _ => false,
    }
}

/// Sends every queued alert and returns whether the connection is still usable; a failed send
/// returns the rest to the sink for the server's duplicate check to resolve after reconnect.
pub(crate) async fn send_queued_alerts<S: mesh_agent_core::ControlStream>(
    conn: &mut mesh_agent_core::AgentConnection<S>,
    sink: &mesh_agent_core::alerts::AlertSink,
) -> bool {
    let mut queued = sink.drain();
    if queued.is_empty() {
        return true;
    }
    let raised = queued.len();

    let mut sent = 0;
    while let Some(alert) = queued.first() {
        if let Err(e) = conn
            .send_control(mesh_agent_core::alerts::alert_message(alert))
            .await
        {
            warn!(
                error = %e, rule_id = %alert.rule_id, sent, unsent = queued.len(),
                "alert send failed, will reconnect and offer them again"
            );
            sink.return_unsent(queued);
            return false;
        }
        debug!(
            rule_id = %alert.rule_id, subject = %alert.subject, summary = %alert.summary,
            "alert sent"
        );
        queued.remove(0);
        sent += 1;
    }

    // Alerts lost to either limit are reported beside what was delivered.
    let stats = sink.stats();
    if stats.dropped_oldest > 0 || stats.suppressed_by_ceiling > 0 {
        warn!(
            raised,
            dropped_oldest = stats.dropped_oldest,
            suppressed_by_ceiling = stats.suppressed_by_ceiling,
            "this machine has lost alerts to its own limits"
        );
    }
    true
}

/// Sends an `AgentUpdateAck` control message, ignoring send failures.
pub(crate) async fn send_update_ack<S: mesh_agent_core::ControlStream>(
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

/// Spawns the post-update watchdog, which rolls back and restarts if no acknowledgement arrives
/// within 60s.
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
                error!("watchdog: no acknowledgement in 60s, rolling back");
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
                // The server's acknowledgement stands the watchdog down.
            }
        }
    });
}

/// Returns `PRETTY_NAME` from `/etc/os-release`, or `std::env::consts::OS` when it is unreadable.
pub(crate) fn os_pretty_name() -> String {
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

/// Capabilities advertised on registration; `Backfill` needs the local store (`has_local_store`).
pub(crate) fn agent_capabilities(has_local_store: bool) -> Vec<mesh_protocol::AgentCapability> {
    use mesh_protocol::AgentCapability;
    let mut caps = vec![
        AgentCapability::Terminal,
        AgentCapability::FileManager,
        AgentCapability::HardwareInventory,
        AgentCapability::DeviceLogs,
        AgentCapability::Discovery,
        AgentCapability::ThresholdAlerts,
        AgentCapability::Alerts,
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
        let caps = agent_capabilities(false);
        assert!(caps.contains(&mesh_protocol::AgentCapability::Discovery));
        assert!(caps.contains(&mesh_protocol::AgentCapability::ThresholdAlerts));
        assert!(caps.contains(&mesh_protocol::AgentCapability::Terminal));
        assert!(caps.contains(&mesh_protocol::AgentCapability::FileManager));
        assert!(caps.contains(&mesh_protocol::AgentCapability::HardwareInventory));
        assert!(caps.contains(&mesh_protocol::AgentCapability::DeviceLogs));
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
    fn test_should_skip_version_older() {
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
        assert!(!should_skip_version("not-a-version"));
        assert!(!should_skip_version(""));
    }

    #[test]
    fn test_should_skip_version_prerelease() {
        assert!(!should_skip_version("99.0.0-rc.1"));
    }

    #[test]
    fn test_parse_ed25519_pubkey_valid() {
        let hex = "a".repeat(64);
        let key = parse_ed25519_pubkey(&hex).unwrap();
        assert_eq!(key, [0xaa; 32]);
    }

    #[test]
    fn test_parse_ed25519_pubkey_wrong_length() {
        let hex = "aa".repeat(16);
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

    fn queued_alert(rule_id: &str, window_start_secs: i64) -> mesh_agent_core::alerts::EdgeAlert {
        mesh_agent_core::alerts::EdgeAlert {
            rule_id: rule_id.to_string(),
            rule_version: 1,
            severity: mesh_agent_core::alerts::AlertSeverity::Warning,
            ts_micros: window_start_secs * 1_000_000,
            window_start_micros: window_start_secs * 1_000_000,
            window_end_micros: window_start_secs * 1_000_000,
            metric: "cpu.total".to_string(),
            value: Some(95.0),
            subject: "cpu.total".to_string(),
            summary: String::new(),
            evidence: Vec::new(),
            evidence_codec: String::new(),
            origin: mesh_agent_core::alerts::AlertOrigin::Live,
        }
    }

    fn connection(
        stream: tokio::io::DuplexStream,
    ) -> mesh_agent_core::AgentConnection<
        mesh_agent_core::AsyncControlStream<tokio::io::DuplexStream>,
    > {
        mesh_agent_core::AgentConnection::new(mesh_agent_core::AsyncControlStream::new(stream))
    }

    #[tokio::test]
    async fn queued_alerts_go_out_in_the_order_they_were_raised() {
        let sink = mesh_agent_core::alerts::AlertSink::default();
        sink.push(
            queued_alert("disk-critical", 1_700_000_000),
            1_700_000_000_000_000,
        );
        sink.push(
            queued_alert("cpu-saturated", 1_700_000_060),
            1_700_000_060_000_000,
        );

        let (agent_end, server_end) = tokio::io::duplex(64 * 1024);
        let mut agent = connection(agent_end);
        let mut server = connection(server_end);

        assert!(send_queued_alerts(&mut agent, &sink).await, "the link held");
        for expected in ["disk-critical", "cpu-saturated"] {
            match server.receive_control().await.expect("an alert frame") {
                mesh_protocol::ControlMessage::AgentAlert { rule_id, .. } => {
                    assert_eq!(rule_id, expected);
                }
                other => panic!("expected AgentAlert, got {other:?}"),
            }
        }
        assert!(sink.drain().is_empty(), "nothing is left to send");
    }

    #[tokio::test]
    async fn an_empty_queue_sends_nothing() {
        let sink = mesh_agent_core::alerts::AlertSink::default();
        let (agent_end, _server_end) = tokio::io::duplex(1024);
        let mut agent = connection(agent_end);
        assert!(send_queued_alerts(&mut agent, &sink).await);
    }

    #[tokio::test]
    async fn a_broken_link_hands_back_what_did_not_go() {
        let sink = mesh_agent_core::alerts::AlertSink::default();
        sink.push(
            queued_alert("disk-critical", 1_700_000_000),
            1_700_000_000_000_000,
        );
        sink.push(
            queued_alert("cpu-saturated", 1_700_000_060),
            1_700_000_060_000_000,
        );

        let (agent_end, server_end) = tokio::io::duplex(1024);
        drop(server_end);
        let mut agent = connection(agent_end);

        assert!(
            !send_queued_alerts(&mut agent, &sink).await,
            "the caller is told the link is gone"
        );
        let back: Vec<_> = sink.drain().into_iter().map(|a| a.rule_id).collect();
        assert_eq!(back, vec!["disk-critical", "cpu-saturated"]);
    }
}
