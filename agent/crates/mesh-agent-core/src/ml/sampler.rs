use std::cmp::Ordering;
use std::collections::{HashMap, VecDeque};
use std::time::{Duration, Instant};

use sysinfo::{Disks, Networks, System, ThreadKind, MINIMUM_CPU_UPDATE_INTERVAL};
use thiserror::Error;

use super::diskperf::DiskPerfReader;
use super::pressure::PressureReader;
use super::primary_iface::resolve_primary_iface;
use super::redact::cmdline_hash;

/// One ranked process entry from a host sample.
#[derive(Debug, Clone, PartialEq)]
pub struct ProcessSample {
    /// Stable rank within this sample; rank is the future series key.
    pub rank: u8,
    /// Executable basename, never the full command line.
    pub basename: String,
    /// Optional hash of the full command line for audited on-demand flows.
    pub cmdline_hash: Option<String>,
    /// The operating system's identifier for the process.
    pub pid: u32,
    /// Share of the whole host's processors since the previous sample, 0–100; `None` on a
    /// process's first sample.
    pub cpu_share: Option<f64>,
    /// Resident memory, in bytes.
    pub mem: f64,
}

/// Host-level metric snapshot consumed by the local detector.
#[derive(Debug, Clone, PartialEq)]
pub struct MetricSample {
    /// Global CPU usage percentage.
    pub cpu_total_percent: f32,
    /// Used memory percentage.
    pub memory_used_percent: f32,
    /// Used percentage of the fullest mounted filesystem; `None` when no mount reports capacity.
    pub disk_used_percent: Option<f32>,
    /// Mounts at or above [`MOUNT_CRITICAL_PERCENT`] used; `None` exactly when
    /// `disk_used_percent` is `None`.
    pub disk_mounts_critical: Option<u32>,
    /// Received bytes/second on the primary interface; `None` until a rate can be computed.
    pub network_rx_bps: Option<f64>,
    /// Transmitted bytes/second on the primary interface; `None` under the same conditions.
    pub network_tx_bps: Option<f64>,
    /// Percent of the last 60 s some task was stalled on CPU; `None` on a host without
    /// pressure information, as are the four stall fields below.
    pub stall_cpu_some: Option<f32>,
    /// Percent of the last 60 s some task was stalled on memory.
    pub stall_mem_some: Option<f32>,
    /// Percent of the last 60 s every runnable task was stalled on memory.
    pub stall_mem_full: Option<f32>,
    /// Percent of the last 60 s some task was stalled on I/O.
    pub stall_io_some: Option<f32>,
    /// Percent of the last 60 s every runnable task was stalled on I/O.
    pub stall_io_full: Option<f32>,
    /// Average milliseconds per I/O on the slowest block device; `None` without a reading.
    pub disk_await_ms: Option<f32>,
    /// Average I/Os outstanding on the most backed-up block device; `None` without a reading.
    pub disk_queue_depth: Option<f32>,
    /// Top processes by CPU rank.
    pub processes: Vec<ProcessSample>,
}

/// Per-second byte rate between two counter readings; `None` without a comparable
/// predecessor. The rate is rounded to whole bytes so it takes the lossless integer path.
#[must_use]
pub(crate) fn byte_rate(prev: Option<(&str, u64)>, cur: (&str, u64), dt_secs: f64) -> Option<f64> {
    let (prev_iface, prev_bytes) = prev?;
    if prev_iface != cur.0 || cur.1 < prev_bytes || dt_secs <= 0.0 {
        return None;
    }
    Some(((cur.1 - prev_bytes) as f64 / dt_secs).round())
}

/// Percentage of `total` that `used` occupies; a zero total yields `0.0`.
#[must_use]
pub(crate) fn used_percent(used: u64, total: u64) -> f32 {
    if total == 0 {
        return 0.0;
    }
    (used as f32 / total as f32) * 100.0
}

/// Percentage of a mount's capacity in use. Free is saturated against total, so a mount
/// reporting more free space than size (network and virtual filesystems) yields 0%.
#[must_use]
pub(crate) fn disk_used_percent(total: u64, free: u64) -> f32 {
    used_percent(total.saturating_sub(free), total)
}

/// The used percentage at which a mount is counted critical.
pub const MOUNT_CRITICAL_PERCENT: f32 = 90.0;

/// The host-wide disk reading reduced from every mount: how full the fullest one
/// is, and how many are at or above [`MOUNT_CRITICAL_PERCENT`].
#[derive(Debug, Clone, Copy, PartialEq)]
pub(crate) struct DiskReduction {
    /// The fullest mount's used percentage.
    pub worst_used_percent: f32,
    /// Mounts at or above [`MOUNT_CRITICAL_PERCENT`] used.
    pub mounts_critical: u32,
}

/// Reduces per-mount `(total, free)` capacity to the fullest mount's used percentage and the
/// critical-mount count. Zero-total mounts are skipped; `None` when no mount reports capacity.
#[must_use]
pub(crate) fn disk_reduction(mounts: impl Iterator<Item = (u64, u64)>) -> Option<DiskReduction> {
    let mut worst: Option<f32> = None;
    let mut mounts_critical = 0u32;
    for (total, free) in mounts {
        if total == 0 {
            continue;
        }
        let used = disk_used_percent(total, free);
        if used >= MOUNT_CRITICAL_PERCENT {
            mounts_critical += 1;
        }
        worst = Some(worst.map_or(used, |w| w.max(used)));
    }
    worst.map(|worst_used_percent| DiskReduction {
        worst_used_percent,
        mounts_critical,
    })
}

/// Share of the whole host's processors a process used between two readings, 0–100: processor
/// time gained over wall time times cores. `None` without a previous reading, cores or wall time.
#[must_use]
pub(crate) fn cpu_share(
    prev_ms: Option<u64>,
    cur_ms: u64,
    wall: Duration,
    cores: usize,
) -> Option<f64> {
    let gained = cur_ms.checked_sub(prev_ms?)?;
    let capacity_ms = wall.as_secs_f64() * 1_000.0 * cores as f64;
    if capacity_ms <= 0.0 {
        return None;
    }
    Some((gained as f64 / capacity_ms * 100.0).clamp(0.0, 100.0))
}

/// Busiest first; a process without a reading after every process with one; then by process id.
#[must_use]
pub(crate) fn busiest_first(left: (Option<f64>, u32), right: (Option<f64>, u32)) -> Ordering {
    let by_share = match (left.0, right.0) {
        (Some(l), Some(r)) => r.total_cmp(&l),
        (Some(_), None) => Ordering::Less,
        (None, Some(_)) => Ordering::Greater,
        (None, None) => Ordering::Equal,
    };
    by_share.then(left.1.cmp(&right.1))
}

/// A process across samples: a reused process id starts at a different time.
type ProcessKey = (u32, u64);

/// Each live process's accumulated processor time, in milliseconds, at the previous sample.
#[derive(Debug, Default)]
struct CpuTimes {
    at: Option<Instant>,
    by_process: HashMap<ProcessKey, u64>,
}

impl CpuTimes {
    /// Keeps only this sample's readings, so an exited process drops out, and returns each
    /// process's share since the previous sample, in the order given.
    fn advance(
        &mut self,
        now: Instant,
        cores: usize,
        readings: impl IntoIterator<Item = (ProcessKey, u64)>,
    ) -> Vec<Option<f64>> {
        let wall = self.at.map(|at| now.saturating_duration_since(at));
        let previous = std::mem::take(&mut self.by_process);
        let shares = readings
            .into_iter()
            .map(|(key, cur_ms)| {
                self.by_process.insert(key, cur_ms);
                wall.and_then(|wall| cpu_share(previous.get(&key).copied(), cur_ms, wall, cores))
            })
            .collect();
        self.at = Some(now);
        shares
    }
}

/// Rank of the process at `index` in the CPU-sorted list; 1-based because rank is the series key.
#[must_use]
pub(crate) fn process_rank(index: usize) -> u8 {
    (index + 1) as u8
}

/// The process identity that leaves the host: the executable's basename, else the process name.
#[must_use]
pub(crate) fn basename_of(exe: Option<&std::path::Path>, name: &std::ffi::OsStr) -> String {
    exe.and_then(std::path::Path::file_name)
        .unwrap_or(name)
        .to_string_lossy()
        .to_string()
}

/// A primary-interface reading: the interface name and its cumulative
/// received/transmitted byte counters at one point in time.
type NetReading = (String, u64, u64);

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
    cpu_times: CpuTimes,
    pressure: PressureReader,
    diskperf: DiskPerfReader,
}

impl SysinfoSampler {
    /// Create a sampler that records top processes by rank only, resolving its pressure
    /// source from the agent's cgroup in a container and from `/proc/pressure` otherwise.
    pub fn new(top_processes: usize) -> Result<Self, SamplerError> {
        if top_processes > u8::MAX as usize {
            return Err(SamplerError::TopNTooLarge);
        }
        let root = std::path::Path::new("/");
        Ok(Self {
            system: System::new_all(),
            networks: Networks::new_with_refreshed_list(),
            top_processes,
            include_cmdline_hash: false,
            prev_net: None,
            cpu_times: CpuTimes::default(),
            pressure: PressureReader::for_root(root),
            diskperf: DiskPerfReader::for_root(root),
        })
    }

    /// Enable or disable full-cmdline hashing for audited on-demand paths.
    pub fn with_cmdline_hash(mut self, enabled: bool) -> Self {
        self.include_cmdline_hash = enabled;
        self
    }

    /// Differences a reading against the previous snapshot into rx/tx rates and records it.
    /// An absent reading clears the snapshot so no rate spans a gap.
    fn net_rates(
        &mut self,
        reading: Option<NetReading>,
        now: Instant,
    ) -> (Option<f64>, Option<f64>) {
        let Some((iface, cur_rx, cur_tx)) = reading else {
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
        let processes_at = Instant::now();
        self.networks.refresh(true);

        let memory_used_percent =
            used_percent(self.system.used_memory(), self.system.total_memory());

        let disks = Disks::new_with_refreshed_list();
        let disk = disk_reduction(
            disks
                .iter()
                .map(|mount| (mount.total_space(), mount.available_space())),
        );

        let reading = resolve_primary_iface(&self.networks).and_then(|iface| {
            self.networks
                .iter()
                .find(|(name, _)| name.as_str() == iface)
                .map(|(_, data)| (iface, data.total_received(), data.total_transmitted()))
        });
        // Network and disk rates difference against the same instant.
        let now = Instant::now();
        let (network_rx_bps, network_tx_bps) = self.net_rates(reading, now);
        let disk_perf = self.diskperf.read(now);

        // A thread is listed beside its process and carries the whole process's memory.
        let running: Vec<_> = self
            .system
            .processes()
            .values()
            .filter(|process| process.thread_kind() != Some(ThreadKind::Userland))
            .collect();
        let shares = self.cpu_times.advance(
            processes_at,
            self.system.cpus().len(),
            running.iter().map(|process| {
                (
                    (process.pid().as_u32(), process.start_time()),
                    process.accumulated_cpu_time(),
                )
            }),
        );
        let mut ranked: Vec<_> = running.into_iter().zip(shares).collect();
        ranked.sort_by(|(left, left_share), (right, right_share)| {
            busiest_first(
                (*left_share, left.pid().as_u32()),
                (*right_share, right.pid().as_u32()),
            )
        });
        let processes = ranked
            .into_iter()
            .take(self.top_processes)
            .enumerate()
            .map(|(index, (process, cpu_share))| {
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
                    rank: process_rank(index),
                    basename: basename_of(process.exe(), process.name()),
                    cmdline_hash,
                    pid: process.pid().as_u32(),
                    cpu_share,
                    mem: process.memory() as f64,
                }
            })
            .collect();

        let stall = self.pressure.read();

        Ok(MetricSample {
            cpu_total_percent: self.system.global_cpu_usage(),
            memory_used_percent,
            disk_used_percent: disk.map(|d| d.worst_used_percent),
            disk_mounts_critical: disk.map(|d| d.mounts_critical),
            network_rx_bps,
            network_tx_bps,
            stall_cpu_some: stall.cpu_some,
            stall_mem_some: stall.mem_some,
            stall_mem_full: stall.mem_full,
            stall_io_some: stall.io_some,
            stall_io_full: stall.io_full,
            disk_await_ms: disk_perf.await_ms,
            disk_queue_depth: disk_perf.queue_depth,
            processes,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsStr;
    use std::path::Path;

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
    fn idle_interface_reports_zero_not_unknown() {
        assert_eq!(
            byte_rate(Some(("eth0", 9_000)), ("eth0", 9_000), 5.0),
            Some(0.0)
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

    #[test]
    fn used_percent_is_the_used_share_of_total() {
        assert_eq!(used_percent(2_048, 8_192), 25.0);
        assert_eq!(used_percent(8_192, 8_192), 100.0);
        assert_eq!(used_percent(0, 8_192), 0.0);
    }

    #[test]
    fn used_percent_of_an_unreported_total_is_zero_not_nan() {
        let pct = used_percent(0, 0);
        assert_eq!(pct, 0.0);
        assert!(!pct.is_nan());
        // A nonzero "used" against a zero total is still an absent reading.
        assert_eq!(used_percent(500, 0), 0.0);
    }

    #[test]
    fn disk_used_percent_is_the_non_free_share() {
        // 400 GB of a 500 GB pool free → 20% used.
        assert_eq!(disk_used_percent(500, 400), 20.0);
        assert_eq!(disk_used_percent(500, 0), 100.0);
        assert_eq!(disk_used_percent(500, 500), 0.0);
    }

    #[test]
    fn disk_used_percent_clamps_free_above_total() {
        assert_eq!(disk_used_percent(500, 900), 0.0);
        assert_eq!(disk_used_percent(0, 100), 0.0);
    }

    #[test]
    fn fs01_reports_the_volume_that_is_about_to_fill() {
        let mounts = [
            (120_000_000_000u64, 2_400_000_000u64),
            (2_000_000_000_000, 1_800_000_000_000),
        ];

        let reduced = disk_reduction(mounts.into_iter()).expect("both mounts report capacity");

        assert!(
            (reduced.worst_used_percent - 98.0).abs() < 0.05,
            "the full volume must be reported, got {}",
            reduced.worst_used_percent
        );
        assert_eq!(reduced.mounts_critical, 1);
    }

    #[test]
    fn a_mount_exactly_on_the_threshold_is_critical() {
        let on_it = disk_used_percent(1_000, 100);
        assert_eq!(
            on_it, MOUNT_CRITICAL_PERCENT,
            "fixture sits on the boundary"
        );
        let under = disk_used_percent(1_000_000, 100_001);
        assert!(under < MOUNT_CRITICAL_PERCENT, "fixture sits just under it");

        assert_eq!(
            disk_reduction([(1_000u64, 100u64)].into_iter())
                .expect("one mount")
                .mounts_critical,
            1
        );
        assert_eq!(
            disk_reduction([(1_000_000u64, 100_001u64)].into_iter())
                .expect("one mount")
                .mounts_critical,
            0
        );
    }

    #[test]
    fn every_mount_past_the_threshold_is_counted() {
        let mounts = [
            (1_000u64, 5u64), // 99.5 %
            (1_000, 60),      // 94 %
            (1_000, 500),     // 50 %
            (1_000, 20),      // 98 %
        ];

        let reduced = disk_reduction(mounts.into_iter()).expect("mounts report capacity");

        assert_eq!(reduced.worst_used_percent, 99.5);
        assert_eq!(reduced.mounts_critical, 3);
    }

    #[test]
    fn a_mount_reporting_no_capacity_takes_part_in_neither_number() {
        let mounts = [(0u64, 0u64), (1_000, 500), (0, 500)];

        let reduced = disk_reduction(mounts.into_iter()).expect("one mount reports capacity");

        assert_eq!(reduced.worst_used_percent, 50.0);
        assert_eq!(reduced.mounts_critical, 0);
    }

    #[test]
    fn a_mount_with_more_free_than_total_reads_as_empty() {
        let mounts = [(500u64, 900u64), (1_000, 250)];

        let reduced = disk_reduction(mounts.into_iter()).expect("mounts report capacity");

        assert_eq!(reduced.worst_used_percent, 75.0);
        assert_eq!(reduced.mounts_critical, 0);
    }

    #[test]
    fn a_host_with_no_measurable_mount_has_no_reading() {
        let none: [(u64, u64); 0] = [];
        assert_eq!(disk_reduction(none.into_iter()), None);
        // Mounts that all report zero capacity are the same case: nothing was
        // measured, so there is nothing to report.
        assert_eq!(disk_reduction([(0u64, 0u64), (0, 128)].into_iter()), None);
    }

    #[test]
    fn nine_busy_cores_of_twenty_four_read_as_their_share_of_the_host() {
        let share = cpu_share(Some(1_000), 10_000, Duration::from_secs(1), 24);
        assert_eq!(share, Some(37.5));
    }

    #[test]
    fn one_core_for_the_whole_interval_reads_as_one_core_of_the_host() {
        let share = cpu_share(Some(0), 1_200, Duration::from_millis(1_200), 8);
        assert_eq!(share, Some(12.5));
    }

    #[test]
    fn a_share_never_exceeds_the_whole_host() {
        assert_eq!(
            cpu_share(Some(0), 30_000, Duration::from_secs(1), 24),
            Some(100.0)
        );
    }

    #[test]
    fn a_share_needs_a_previous_reading_cores_and_elapsed_time() {
        assert_eq!(cpu_share(None, 500, Duration::from_secs(1), 4), None);
        assert_eq!(cpu_share(Some(0), 500, Duration::from_secs(1), 0), None);
        assert_eq!(cpu_share(Some(0), 500, Duration::ZERO, 4), None);
        assert_eq!(cpu_share(Some(900), 500, Duration::from_secs(1), 4), None);
        assert_eq!(
            cpu_share(Some(500), 500, Duration::from_secs(1), 4),
            Some(0.0)
        );
    }

    #[test]
    fn the_busiest_measured_process_ranks_first_and_unmeasured_ones_last() {
        let mut rows = vec![
            (None, 3),
            (Some(10.0), 9),
            (None, 1),
            (Some(50.0), 7),
            (Some(10.0), 2),
        ];
        rows.sort_by(|l, r| busiest_first(*l, *r));
        assert_eq!(
            rows,
            vec![
                (Some(50.0), 7),
                (Some(10.0), 2),
                (Some(10.0), 9),
                (None, 1),
                (None, 3)
            ]
        );
    }

    #[test]
    fn a_process_has_no_share_until_its_second_sample() {
        let mut times = CpuTimes::default();
        let start = Instant::now();

        let first = times.advance(start, 4, [((10, 100), 1_000)]);
        let second = times.advance(
            start + Duration::from_secs(1),
            4,
            [((10, 100), 3_000), ((11, 105), 50)],
        );

        assert_eq!(first, vec![None]);
        assert_eq!(second, vec![Some(50.0), None]);
    }

    #[test]
    fn a_reused_process_id_starts_over() {
        let mut times = CpuTimes::default();
        let start = Instant::now();
        times.advance(start, 4, [((10, 100), 1_000)]);

        let reused = times.advance(start + Duration::from_secs(1), 4, [((10, 250), 1_200)]);

        assert_eq!(reused, vec![None]);
    }

    #[test]
    fn an_exited_process_is_forgotten() {
        let mut times = CpuTimes::default();
        let start = Instant::now();
        times.advance(start, 4, [((10, 100), 1_000), ((20, 100), 1_000)]);
        times.advance(start + Duration::from_secs(1), 4, [((20, 100), 1_400)]);

        assert_eq!(times.by_process.len(), 1);
        let back = times.advance(start + Duration::from_secs(2), 4, [((10, 100), 5_000)]);
        assert_eq!(back, vec![None]);
    }

    #[test]
    fn process_rank_is_one_based() {
        assert_eq!(process_rank(0), 1);
        assert_eq!(process_rank(1), 2);
        assert_eq!(process_rank(254), 255);
    }

    #[test]
    fn basename_prefers_the_executable_file_name() {
        assert_eq!(
            basename_of(
                Some(Path::new("/usr/sbin/nginx")),
                OsStr::new("nginx: worker")
            ),
            "nginx"
        );
    }

    #[test]
    fn basename_falls_back_to_the_process_name() {
        assert_eq!(basename_of(None, OsStr::new("kthreadd")), "kthreadd");
        assert_eq!(
            basename_of(Some(Path::new("/")), OsStr::new("init")),
            "init"
        );
    }

    #[test]
    fn basename_never_returns_the_full_path() {
        let basename = basename_of(
            Some(Path::new("/home/ivan/secret-project/build/agent")),
            OsStr::new("agent"),
        );
        assert_eq!(basename, "agent");
        assert!(!basename.contains('/'));
    }

    #[test]
    fn first_reading_establishes_the_baseline_without_rates() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let now = Instant::now();

        assert_eq!(
            sampler.net_rates(Some(("eth0".into(), 1_000, 2_000)), now),
            (None, None)
        );
    }

    #[test]
    fn second_reading_differences_both_directions_over_the_interval() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let start = Instant::now();
        sampler.net_rates(Some(("eth0".into(), 1_000, 2_000)), start);

        // +2000 rx and +8000 tx over 2 s → 1000 and 4000 B/s.
        let rates = sampler.net_rates(
            Some(("eth0".into(), 3_000, 10_000)),
            start + Duration::from_secs(2),
        );

        assert_eq!(rates, (Some(1_000.0), Some(4_000.0)));
    }

    #[test]
    fn each_reading_rebaselines_for_the_next() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let start = Instant::now();
        sampler.net_rates(Some(("eth0".into(), 0, 0)), start);
        sampler.net_rates(
            Some(("eth0".into(), 1_000, 1_000)),
            start + Duration::from_secs(1),
        );

        let rates = sampler.net_rates(
            Some(("eth0".into(), 2_000, 2_000)),
            start + Duration::from_secs(2),
        );

        assert_eq!(rates, (Some(1_000.0), Some(1_000.0)));
    }

    #[test]
    fn a_changed_interface_reports_nothing_then_rebaselines() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let start = Instant::now();
        sampler.net_rates(Some(("eth0".into(), 1_000, 1_000)), start);

        let on_change = sampler.net_rates(
            Some(("wlan0".into(), 50, 50)),
            start + Duration::from_secs(1),
        );
        let after = sampler.net_rates(
            Some(("wlan0".into(), 550, 1_050)),
            start + Duration::from_secs(2),
        );

        assert_eq!(on_change, (None, None));
        assert_eq!(after, (Some(500.0), Some(1_000.0)));
    }

    #[test]
    fn an_absent_reading_drops_the_baseline() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let start = Instant::now();
        sampler.net_rates(Some(("eth0".into(), 1_000, 1_000)), start);

        let gap = sampler.net_rates(None, start + Duration::from_secs(1));
        let resumed = sampler.net_rates(
            Some(("eth0".into(), 9_000, 9_000)),
            start + Duration::from_secs(2),
        );

        assert_eq!(gap, (None, None));
        assert_eq!(resumed, (None, None));
    }

    #[test]
    fn a_counter_reset_reports_nothing_then_rebaselines() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let start = Instant::now();
        sampler.net_rates(Some(("eth0".into(), 9_000, 9_000)), start);

        let on_reset = sampler.net_rates(
            Some(("eth0".into(), 100, 100)),
            start + Duration::from_secs(1),
        );
        let after = sampler.net_rates(
            Some(("eth0".into(), 600, 1_100)),
            start + Duration::from_secs(2),
        );

        assert_eq!(on_reset, (None, None));
        assert_eq!(after, (Some(500.0), Some(1_000.0)));
    }

    #[test]
    fn an_idle_link_reports_zero_in_both_directions() {
        let mut sampler = SysinfoSampler::new(0).expect("top-N 0 is valid");
        let start = Instant::now();
        sampler.net_rates(Some(("eth0".into(), 4_096, 8_192)), start);

        let rates = sampler.net_rates(
            Some(("eth0".into(), 4_096, 8_192)),
            start + Duration::from_secs(5),
        );

        assert_eq!(rates, (Some(0.0), Some(0.0)));
    }

    #[test]
    fn the_busiest_processes_come_back_with_the_numbers_behind_their_rank() {
        let mut sampler = SysinfoSampler::new(3).expect("top-N 3 is valid");
        let sample = sampler.sample().expect("the host can be sampled");

        assert!(
            (1..=3).contains(&sample.processes.len()),
            "a running host has processes, and no more than were asked for"
        );
        assert!(
            sample.processes.windows(2).all(|w| busiest_first(
                (w[0].cpu_share, w[0].pid),
                (w[1].cpu_share, w[1].pid)
            ) != Ordering::Greater),
            "busiest first"
        );
        for process in &sample.processes {
            assert!(process.pid > 0, "{} names its process", process.basename);
            assert_eq!(
                process.cpu_share, None,
                "the first sample has nothing to compare"
            );
            assert!(process.mem.is_finite() && process.mem >= 0.0);
        }
    }

    #[test]
    fn top_process_count_must_fit_in_a_rank_byte() {
        assert!(matches!(
            SysinfoSampler::new(u8::MAX as usize + 1),
            Err(SamplerError::TopNTooLarge)
        ));
        assert!(SysinfoSampler::new(u8::MAX as usize).is_ok());
    }
}
