use std::path::PathBuf;
use std::sync::mpsc::SyncSender;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tracing::{debug, info, warn};

use crate::host_logs::{build_log_rate_window, collect_host_logs, LogSource};
use mesh_protocol::ControlMessage;

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
/// trains a local anomaly ensemble on a warm-up window, and — when `store` is
/// set — persists each raw sample with its inline anomaly bit into the graduated
/// `LocalTsdb` (the sovereign min/max/last + 1 s raw copy). The store is a cache:
/// an open failure recreates it fresh and, failing that, degrades to log-only —
/// it never aborts sampling or the agent.
pub(crate) fn spawn_sampler(store: Option<StoreConfig>) -> tokio::task::JoinHandle<()> {
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
        let mut sink = store.and_then(|cfg| open_sink(&cfg));

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
            if let Some(sink) = sink.as_mut() {
                if let Err(e) = sink.record(unix_now(), &sample, anomaly) {
                    warn!(error = %e, "edge-sentinel store write failed");
                }
            }
        }
    })
}

/// Open the local store, recreating it fresh on a corrupt/incompatible file, and
/// degrading to `None` (log-only sampling) if even that fails.
fn open_sink(cfg: &StoreConfig) -> Option<mesh_agent_core::ml::store_sink::LocalStoreSink> {
    use mesh_agent_core::ml::store_sink::LocalStoreSink;
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
