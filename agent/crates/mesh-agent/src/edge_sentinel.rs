use std::path::PathBuf;
use std::sync::mpsc::SyncSender;
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tracing::{debug, info, warn};

use crate::host_logs::{build_log_rate_window, collect_host_logs, LogSource};
use mesh_agent_core::ml::store_sink::LocalStoreSink;
use mesh_protocol::ControlMessage;

/// The sampler-owned local store, shared with the WS-15 backfill coordinator on
/// the control loop. The sampler holds the lock only for the sub-millisecond
/// `record`/`commit` each second; the coordinator holds it only to open an MVCC
/// snapshot or advance a cursor — neither blocks the other for a meaningful time.
pub(crate) type SharedSink = Arc<Mutex<LocalStoreSink>>;

/// Interval between host log-rate windows. Long enough that the bounded reads
/// never compete with control or session traffic on constrained fleet hosts.
const LOG_READER_INTERVAL: Duration = Duration::from_secs(60);

/// Host log sources the reader task sweeps each window. Sources that do not
/// apply to the current platform collect nothing and are skipped.
const LOG_SOURCES: [LogSource; 3] = [
    LogSource::AgentSelf,
    LogSource::Journald,
    LogSource::WindowsEventLog,
];

/// Spawn the bounded, default-off host log-rate reader task. Each window it
/// reads a capped slice of every host source, folds it into the log-rate
/// feature vector (level counts + top-unit ranks + volume — never message text),
/// and forwards it to the control loop as a metric window over `sink`. A window
/// is dropped when the channel is full so a log burst can never backpressure the
/// control stream. The task yields for [`LOG_READER_INTERVAL`] between windows.
pub(crate) fn spawn_log_readers(
    log_dir: PathBuf,
    sink: SyncSender<ControlMessage>,
) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || loop {
        std::thread::sleep(LOG_READER_INTERVAL);
        for source in LOG_SOURCES {
            let entries = collect_host_logs(source, &log_dir);
            let Some(window) = build_log_rate_window(source, &entries, unix_now()) else {
                continue;
            };
            debug!(
                ?source,
                entries = entries.len(),
                "edge-sentinel log-rate window"
            );
            if sink.try_send(window).is_err() {
                debug!(?source, "log-rate window dropped: telemetry channel full");
            }
        }
    })
}

/// Interval between auto-discovery sweeps. Long by design: the host profile
/// changes rarely, and a sweep shells out to package managers and lists
/// services, so it must never compete with control or session traffic. Reports
/// are change-triggered — a sweep only ships when the profile differs from the
/// last one shipped.
const DISCOVERY_INTERVAL: Duration = Duration::from_secs(1800);

/// Spawn the bounded, default-off auto-discovery task (WS-16). Each sweep
/// profiles the host — listening ports, services, DB engines, containers,
/// installed packages — into a bounded, secret-free `DiscoveryReport` and, when
/// the profile changed since the last shipped report, forwards it to the control
/// loop over `sink`. A report is dropped when the channel is full so a sweep can
/// never backpressure the control stream. The first sweep runs immediately; the
/// task then yields for [`DISCOVERY_INTERVAL`] between sweeps.
pub(crate) fn spawn_discovery(sink: SyncSender<ControlMessage>) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        let mut last_fingerprint: Option<u64> = None;
        loop {
            let profile = mesh_agent_core::discovery::collect_profile();
            let fingerprint = profile.fingerprint();
            if last_fingerprint != Some(fingerprint) {
                last_fingerprint = Some(fingerprint);
                debug!(
                    ports = profile.ports.len(),
                    services = profile.services.len(),
                    db_engines = profile.db_engines.len(),
                    containers = profile.containers.len(),
                    packages = profile.packages.len(),
                    truncated = profile.truncated,
                    "edge-sentinel discovery report"
                );
                if sink.try_send(profile.into_report(unix_now())).is_err() {
                    debug!("discovery report dropped: telemetry channel full");
                }
            }
            std::thread::sleep(DISCOVERY_INTERVAL);
        }
    })
}

/// Current Unix time in whole seconds, clamped to 0 before the epoch.
fn unix_now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Local-store wiring for the sampler task: where the redb multi-tier store
/// lives and its footprint cap.
pub(crate) struct StoreConfig {
    /// Store directory (under the agent data dir).
    pub path: PathBuf,
    /// Hard footprint cap in bytes.
    pub cap_bytes: u64,
}

/// Samples the required warm-up window before the ensemble can be trained.
const WARMUP_SAMPLES: usize = 30;
/// Ensemble geometry (staggered k=2 models over the CPU/mem/disk feature vector).
const ENSEMBLE_MODELS: usize = 6;
const ENSEMBLE_ITERS: usize = 20;
/// Durable flush cadence for the local store (bounded-loss window, in samples).
const STORE_COMMIT_EVERY: usize = 60;

/// Spawn the Edge-Sentinel sampler task. It samples host metrics once per second,
/// trains a local anomaly ensemble on a warm-up window, and — when `sink` is
/// set — persists each raw sample with its inline anomaly bit into the graduated
/// `LocalTsdb` (the sovereign min/max/last + 1 s raw copy). The store is shared
/// with the WS-15 backfill coordinator; the sampler holds the lock only for the
/// per-second append/commit. A `None` sink degrades to log-only sampling.
pub(crate) fn spawn_sampler(sink: Option<SharedSink>) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        use mesh_agent_core::ml::ensemble::EdgeMlEnsemble;
        use mesh_agent_core::ml::sampler::{MetricSampler, SysinfoSampler};

        let mut sampler = match SysinfoSampler::new(10) {
            Ok(sampler) => sampler,
            Err(e) => {
                warn!(error = %e, "edge-sentinel sampler disabled");
                return;
            }
        };

        let mut warmup: Vec<[f32; 3]> = Vec::with_capacity(WARMUP_SAMPLES);
        let mut ensemble: Option<EdgeMlEnsemble<3>> = None;

        loop {
            std::thread::sleep(Duration::from_secs(1));
            let sample = match sampler.sample() {
                Ok(sample) => sample,
                Err(e) => {
                    warn!(error = %e, "edge-sentinel sample failed");
                    continue;
                }
            };
            let features = [
                sample.cpu_total_percent,
                sample.memory_used_percent,
                sample.disk_used_percent,
            ];
            // Cold start: collect a warm-up window, then train once. Until the
            // ensemble exists, samples are stored with a `false` anomaly bit.
            let anomaly = match &ensemble {
                Some(model) => model.is_anomaly(&features),
                None => {
                    warmup.push(features);
                    if warmup.len() >= WARMUP_SAMPLES {
                        match EdgeMlEnsemble::<3>::train_staggered(
                            &warmup,
                            ENSEMBLE_MODELS,
                            ENSEMBLE_ITERS,
                        ) {
                            Ok(model) => {
                                info!("edge-sentinel anomaly ensemble trained");
                                ensemble = Some(model);
                            }
                            Err(e) => warn!(error = %e, "edge-sentinel ensemble train failed"),
                        }
                    }
                    false
                }
            };
            debug!(
                cpu = sample.cpu_total_percent,
                mem = sample.memory_used_percent,
                disk = sample.disk_used_percent,
                anomaly,
                "edge-sentinel sample"
            );
            if let Some(sink) = sink.as_ref() {
                match sink.lock() {
                    Ok(mut sink) => {
                        if let Err(e) = sink.record(unix_now(), &sample, anomaly) {
                            warn!(error = %e, "edge-sentinel store write failed");
                        }
                    }
                    Err(e) => warn!(error = %e, "edge-sentinel store lock poisoned"),
                }
            }
        }
    })
}

/// Open the local store, recreating it fresh on a corrupt/incompatible file, and
/// degrading to `None` (log-only sampling) if even that fails.
pub(crate) fn open_sink(cfg: &StoreConfig) -> Option<LocalStoreSink> {
    match LocalStoreSink::open(&cfg.path, cfg.cap_bytes, STORE_COMMIT_EVERY) {
        Ok(sink) => {
            info!(path = %cfg.path.display(), "edge-sentinel local store opened");
            Some(sink)
        }
        Err(e) => {
            warn!(error = %e, path = %cfg.path.display(), "local store open failed; recreating fresh");
            if let Err(e) = std::fs::remove_dir_all(&cfg.path) {
                debug!(error = %e, "could not remove the old store dir before recreate");
            }
            match LocalStoreSink::open(&cfg.path, cfg.cap_bytes, STORE_COMMIT_EVERY) {
                Ok(sink) => Some(sink),
                Err(e) => {
                    warn!(error = %e, "edge-sentinel local store disabled (sampling continues)");
                    None
                }
            }
        }
    }
}
