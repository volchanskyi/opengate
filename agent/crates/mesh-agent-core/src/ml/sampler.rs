use std::collections::VecDeque;
use std::time::{Duration, Instant};

use sysinfo::{Disks, Networks, System, MINIMUM_CPU_UPDATE_INTERVAL};
use thiserror::Error;

use super::primary_iface::resolve_primary_iface;
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
    /// Received throughput on the primary interface, in bytes/second (rounded to
    /// whole bytes). `None` when no rate can be computed yet — the first sample,
    /// an interface change, a counter reset, or a non-positive interval — so a
    /// stale or wrong number is never reported.
    pub network_rx_bps: Option<f64>,
    /// Transmitted throughput on the primary interface, in bytes/second. `None`
    /// under the same conditions as [`network_rx_bps`](Self::network_rx_bps).
    pub network_tx_bps: Option<f64>,
    /// Top processes by CPU rank.
    pub processes: Vec<ProcessSample>,
}

/// Per-second byte rate from two cumulative counter readings on the same
/// interface. `None` on the first sample (no `prev`), an interface change, a
/// counter reset/wrap (`cur < prev`), or a non-positive interval — never a
/// wrong number. The rate is rounded to whole bytes/second so it stores on the
/// lossless integer path and live-stream averages equal reconnect-backfill.
#[must_use]
pub(crate) fn byte_rate(prev: Option<(&str, u64)>, cur: (&str, u64), dt_secs: f64) -> Option<f64> {
    let (prev_iface, prev_bytes) = prev?;
    if prev_iface != cur.0 || cur.1 < prev_bytes || dt_secs <= 0.0 {
        return None;
    }
    Some(((cur.1 - prev_bytes) as f64 / dt_secs).round())
}

/// The previous primary-interface counter snapshot, held between samples so the
/// next sample can difference against it into a rate.
#[derive(Debug, Clone)]
struct PrevNet {
    iface: String,
    rx: u64,
    tx: u64,
    at: Instant,
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
    prev_net: Option<PrevNet>,
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
            prev_net: None,
        })
    }

    /// Enable or disable full-cmdline hashing for audited on-demand paths.
    pub fn with_cmdline_hash(mut self, enabled: bool) -> Self {
        self.include_cmdline_hash = enabled;
        self
    }

    /// Difference the primary interface's cumulative counters against the
    /// previous snapshot into rx/tx byte-rates, then record the current snapshot
    /// for the next call. Resets the snapshot (yielding `None`) when the primary
    /// interface cannot be resolved or is no longer tracked.
    fn compute_net_rates(&mut self, now: Instant) -> (Option<f64>, Option<f64>) {
        let Some(iface) = resolve_primary_iface(&self.networks) else {
            self.prev_net = None;
            return (None, None);
        };
        let Some((cur_rx, cur_tx)) = self
            .networks
            .iter()
            .find(|(name, _)| name.as_str() == iface)
            .map(|(_, data)| (data.total_received(), data.total_transmitted()))
        else {
            self.prev_net = None;
            return (None, None);
        };
        let rates = match &self.prev_net {
            Some(prev) => {
                let dt = now.duration_since(prev.at).as_secs_f64();
                (
                    byte_rate(
                        Some((prev.iface.as_str(), prev.rx)),
                        (iface.as_str(), cur_rx),
                        dt,
                    ),
                    byte_rate(
                        Some((prev.iface.as_str(), prev.tx)),
                        (iface.as_str(), cur_tx),
                        dt,
                    ),
                )
            }
            None => (None, None),
        };
        self.prev_net = Some(PrevNet {
            iface,
            rx: cur_rx,
            tx: cur_tx,
            at: now,
        });
        rates
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

        let (network_rx_bps, network_tx_bps) = self.compute_net_rates(Instant::now());

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
            network_rx_bps,
            network_tx_bps,
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

#[cfg(test)]
mod tests {
    use super::byte_rate;

    #[test]
    fn rate_is_delta_over_interval_rounded_to_whole_bytes() {
        // 2000 bytes over 2 s → 1000 B/s.
        assert_eq!(
            byte_rate(Some(("eth0", 1_000)), ("eth0", 3_000), 2.0),
            Some(1_000.0)
        );
        // Fractional interval rounds to the nearest whole byte/second.
        assert_eq!(
            byte_rate(Some(("eth0", 0)), ("eth0", 1_000), 3.0),
            Some(333.0)
        );
    }

    #[test]
    fn first_sample_has_no_previous_so_no_rate() {
        assert_eq!(byte_rate(None, ("eth0", 5_000), 1.0), None);
    }

    #[test]
    fn interface_change_yields_no_rate() {
        // The primary interface moved (eth0 → wlan0); the counters are not
        // comparable, so no rate is emitted this tick.
        assert_eq!(
            byte_rate(Some(("eth0", 1_000)), ("wlan0", 9_000), 1.0),
            None
        );
    }

    #[test]
    fn counter_reset_or_wrap_yields_no_rate() {
        // cur < prev (reboot / counter wrap) must never produce a negative or
        // huge rate — it yields None.
        assert_eq!(byte_rate(Some(("eth0", 9_000)), ("eth0", 100), 1.0), None);
    }

    #[test]
    fn non_positive_interval_yields_no_rate() {
        assert_eq!(byte_rate(Some(("eth0", 0)), ("eth0", 1_000), 0.0), None);
        assert_eq!(byte_rate(Some(("eth0", 0)), ("eth0", 1_000), -1.0), None);
    }
}
