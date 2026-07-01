use std::time::Duration;

use tracing::{debug, warn};

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
