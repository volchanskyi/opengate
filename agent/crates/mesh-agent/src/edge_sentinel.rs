use std::path::PathBuf;
use std::sync::mpsc::SyncSender;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tracing::{debug, warn};

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

pub(crate) fn spawn_sampler() -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(|| {
        use mesh_agent_core::ml::sampler::{MetricSampler, SysinfoSampler};

        let mut sampler = match SysinfoSampler::new(10) {
            Ok(sampler) => sampler,
            Err(e) => {
                warn!(error = %e, "edge-sentinel sampler disabled");
                return;
            }
        };

        loop {
            std::thread::sleep(Duration::from_secs(1));
            match sampler.sample() {
                Ok(sample) => debug!(
                    cpu = sample.cpu_total_percent,
                    mem = sample.memory_used_percent,
                    disk = sample.disk_used_percent,
                    processes = sample.processes.len(),
                    "edge-sentinel sample"
                ),
                Err(e) => warn!(error = %e, "edge-sentinel sample failed"),
            }
        }
    })
}
