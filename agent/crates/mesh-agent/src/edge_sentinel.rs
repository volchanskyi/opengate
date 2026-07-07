use std::path::PathBuf;
use std::time::Duration;

use tracing::{debug, warn};

use crate::host_logs::{collect_host_logs, log_rate_vector, LogSource};
use mesh_agent_core::ml::log_rate::LOG_RATE_DIMS;

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
/// reads a capped slice of every host source, folds it into the WS-2 log-rate
/// feature vector (level counts + top-unit ranks + volume — never message
/// text), and yields for [`LOG_READER_INTERVAL`] between windows.
pub(crate) fn spawn_log_readers(log_dir: PathBuf) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || loop {
        std::thread::sleep(LOG_READER_INTERVAL);
        for source in LOG_SOURCES {
            let entries = collect_host_logs(source, &log_dir);
            if entries.is_empty() {
                continue;
            }
            let rates = log_rate_vector(&entries);
            debug!(
                ?source,
                entries = entries.len(),
                error = rates[0],
                warn = rates[1],
                volume = rates[LOG_RATE_DIMS - 1],
                "edge-sentinel log-rate window"
            );
        }
    })
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
