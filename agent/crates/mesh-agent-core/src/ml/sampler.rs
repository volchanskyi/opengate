use std::collections::VecDeque;
use std::time::Duration;

use sysinfo::{Disks, Networks, System, MINIMUM_CPU_UPDATE_INTERVAL};
use thiserror::Error;

use super::redact::cmdline_hash;

/// One ranked process entry from a host sample.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProcessSample {
    /// Stable rank within this sample; rank is the future series key.
    pub rank: u8,
    /// Executable basename, never the full command line.
    pub basename: String,
    /// Optional hash of the full command line for audited on-demand flows.
    pub cmdline_hash: Option<String>,
}

/// Host-level metric snapshot consumed by the local detector.
#[derive(Debug, Clone, PartialEq)]
pub struct MetricSample {
    /// Global CPU usage percentage.
    pub cpu_total_percent: f32,
    /// Used memory percentage.
    pub memory_used_percent: f32,
    /// Used disk percentage across mounted disks.
    pub disk_used_percent: f32,
    /// Total received network bytes.
    pub network_rx_bytes: u64,
    /// Total transmitted network bytes.
    pub network_tx_bytes: u64,
    /// Top processes by CPU rank.
    pub processes: Vec<ProcessSample>,
}

/// Errors returned by metric samplers.
#[derive(Debug, Error, PartialEq, Eq)]
#[non_exhaustive]
pub enum SamplerError {
    /// The fake sampler has no more queued samples.
    #[error("no sample available")]
    Empty,
    /// The configured process top-N is too large for a compact rank.
    #[error("top process count must fit in u8")]
    TopNTooLarge,
}

/// Synchronous host metric sampler.
pub trait MetricSampler {
    /// Capture the next sample.
    fn sample(&mut self) -> Result<MetricSample, SamplerError>;
}

/// Deterministic sampler for unit and integration tests.
#[derive(Debug, Clone)]
pub struct FakeSampler {
    samples: VecDeque<MetricSample>,
}

impl FakeSampler {
    /// Create a fake sampler from a finite sequence.
    pub fn new(samples: Vec<MetricSample>) -> Self {
        Self {
            samples: samples.into(),
        }
    }
}

impl MetricSampler for FakeSampler {
    fn sample(&mut self) -> Result<MetricSample, SamplerError> {
        self.samples.pop_front().ok_or(SamplerError::Empty)
    }
}

/// `sysinfo` backed host sampler.
pub struct SysinfoSampler {
    system: System,
    networks: Networks,
    top_processes: usize,
    include_cmdline_hash: bool,
}

impl SysinfoSampler {
    /// Create a sampler that records top processes by rank only.
    pub fn new(top_processes: usize) -> Result<Self, SamplerError> {
        if top_processes > u8::MAX as usize {
            return Err(SamplerError::TopNTooLarge);
        }
        Ok(Self {
            system: System::new_all(),
            networks: Networks::new_with_refreshed_list(),
            top_processes,
            include_cmdline_hash: false,
        })
    }

    /// Enable or disable full-cmdline hashing for audited on-demand paths.
    pub fn with_cmdline_hash(mut self, enabled: bool) -> Self {
        self.include_cmdline_hash = enabled;
        self
    }
}

impl MetricSampler for SysinfoSampler {
    fn sample(&mut self) -> Result<MetricSample, SamplerError> {
        self.system.refresh_memory();
        self.system.refresh_cpu_usage();
        std::thread::sleep(MINIMUM_CPU_UPDATE_INTERVAL.max(Duration::from_millis(200)));
        self.system.refresh_cpu_usage();
        self.system
            .refresh_processes(sysinfo::ProcessesToUpdate::All, true);
        self.networks.refresh(true);

        let total_memory = self.system.total_memory();
        let memory_used_percent = if total_memory == 0 {
            0.0
        } else {
            (self.system.used_memory() as f32 / total_memory as f32) * 100.0
        };

        let disks = Disks::new_with_refreshed_list();
        let (disk_total, disk_free) = disks.iter().fold((0u64, 0u64), |(total, free), disk| {
            (total + disk.total_space(), free + disk.available_space())
        });
        let disk_used_percent = if disk_total == 0 {
            0.0
        } else {
            ((disk_total - disk_free) as f32 / disk_total as f32) * 100.0
        };

        let (network_rx_bytes, network_tx_bytes) = self
            .networks
            .iter()
            .fold((0u64, 0u64), |(rx, tx), (_, data)| {
                (rx + data.total_received(), tx + data.total_transmitted())
            });

        let mut processes: Vec<_> = self.system.processes().values().collect();
        processes.sort_by(|left, right| right.cpu_usage().total_cmp(&left.cpu_usage()));
        let processes = processes
            .into_iter()
            .take(self.top_processes)
            .enumerate()
            .map(|(index, process)| {
                let cmdline_hash = if self.include_cmdline_hash {
                    let cmdline = process
                        .cmd()
                        .iter()
                        .map(|part| part.to_string_lossy())
                        .collect::<Vec<_>>()
                        .join(" ");
                    if cmdline.is_empty() {
                        None
                    } else {
                        Some(cmdline_hash(&cmdline))
                    }
                } else {
                    None
                };
                ProcessSample {
                    rank: (index + 1) as u8,
                    basename: process_basename(process),
                    cmdline_hash,
                }
            })
            .collect();

        Ok(MetricSample {
            cpu_total_percent: self.system.global_cpu_usage(),
            memory_used_percent,
            disk_used_percent,
            network_rx_bytes,
            network_tx_bytes,
            processes,
        })
    }
}

fn process_basename(process: &sysinfo::Process) -> String {
    if let Some(exe) = process.exe() {
        if let Some(name) = exe.file_name() {
            return name.to_string_lossy().to_string();
        }
    }
    process.name().to_string_lossy().to_string()
}
