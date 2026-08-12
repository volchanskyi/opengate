use std::collections::VecDeque;
use std::time::{Duration, Instant};

use sysinfo::{Disks, Networks, System, MINIMUM_CPU_UPDATE_INTERVAL};
use thiserror::Error;

use super::diskperf::DiskPerfReader;
use super::pressure::PressureReader;
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
    /// Used percentage of the **fullest mounted filesystem**. `None` when no
    /// mount reports capacity, which is an unmeasurable disk rather than an
    /// empty one — see [`disk_reduction`].
    pub disk_used_percent: Option<f32>,
    /// How many mounts are at or above [`MOUNT_CRITICAL_PERCENT`] used. `None`
    /// under the same condition as
    /// [`disk_used_percent`](Self::disk_used_percent), so the pair is always
    /// present together or absent together.
    pub disk_mounts_critical: Option<u32>,
    /// Received throughput on the primary interface, in bytes/second (rounded to
    /// whole bytes). `None` when no rate can be computed yet — the first sample,
    /// an interface change, a counter reset, or a non-positive interval — so a
    /// stale or wrong number is never reported.
    pub network_rx_bps: Option<f64>,
    /// Transmitted throughput on the primary interface, in bytes/second. `None`
    /// under the same conditions as [`network_rx_bps`](Self::network_rx_bps).
    pub network_tx_bps: Option<f64>,
    /// Percent of the last 60 s some task was stalled on CPU. `None` on a host
    /// whose kernel publishes no pressure information — never a zero, which
    /// would read as "never stalled" on a host that cannot measure stalling at
    /// all. The same holds for the four vitals below.
    pub stall_cpu_some: Option<f32>,
    /// Percent of the last 60 s some task was stalled on memory.
    pub stall_mem_some: Option<f32>,
    /// Percent of the last 60 s every runnable task was stalled on memory.
    pub stall_mem_full: Option<f32>,
    /// Percent of the last 60 s some task was stalled on I/O.
    pub stall_io_some: Option<f32>,
    /// Percent of the last 60 s every runnable task was stalled on I/O.
    pub stall_io_full: Option<f32>,
    /// Average service time of one I/O on the slowest block device, in
    /// milliseconds. `None` when no such reading exists — the first sample after
    /// start, a device whose counters wrapped, a second in which no I/O
    /// completed, a containerized agent, or a host without `/proc/diskstats`.
    pub disk_await_ms: Option<f32>,
    /// Average number of I/Os outstanding on the most backed-up block device.
    /// `None` under the same conditions, except that a second in which nothing
    /// queued is a real reading of zero.
    pub disk_queue_depth: Option<f32>,
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

/// Percentage of `total` that `used` occupies. A zero total means the platform
/// reported no capacity yet, which is an absent reading rather than a full or
/// empty resource, so it yields `0.0` instead of a NaN division.
#[must_use]
pub(crate) fn used_percent(used: u64, total: u64) -> f32 {
    if total == 0 {
        return 0.0;
    }
    (used as f32 / total as f32) * 100.0
}

/// Percentage of aggregate disk capacity in use, from total and free bytes.
/// Free is saturated against total so a mount whose free space exceeds its
/// reported size (network and virtual filesystems do report this) yields 0%
/// rather than wrapping into a nonsense figure.
#[must_use]
pub(crate) fn disk_used_percent(total: u64, free: u64) -> f32 {
    used_percent(total.saturating_sub(free), total)
}

/// The used percentage at which a mount is counted critical. A volume this full
/// has run out of comfortable headroom, so an operator wants it named while
/// there is still room to act.
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

/// Reduce per-mount `(total, free)` capacity to the fullest mount's used
/// percentage and the count of mounts at or above [`MOUNT_CRITICAL_PERCENT`].
///
/// The fullest mount is what makes the reading actionable. Pooling every mount's
/// bytes and dividing once answers a question nobody asks: a 120 GB system
/// volume at 98 % beside a 2 TB data volume at 10 % pools to ~15 %, so the volume
/// that is about to fill is invisible to any threshold, and a small OS volume
/// beside large data volumes is the normal shape of a server.
///
/// A mount reporting zero total is capacity the platform did not report, so it
/// takes part in neither number rather than reading as 100 % full. `None` when
/// no mount reports capacity at all — a host with nothing measurable mounted has
/// no disk reading, which is a different thing from empty disks.
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

/// Rank of the process at `index` in the CPU-sorted list. Ranks are 1-based:
/// rank 1 is the busiest process, and rank is the series key the detector uses,
/// so it must never be 0.
#[must_use]
pub(crate) fn process_rank(index: usize) -> u8 {
    (index + 1) as u8
}

/// The process identity that leaves the host: the executable's basename from
/// its path when the platform reports one, else the process name. Never the
/// path and never the command line.
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
    pressure: PressureReader,
    diskperf: DiskPerfReader,
}

impl SysinfoSampler {
    /// Create a sampler that records top processes by rank only.
    ///
    /// The two Linux-only sources are resolved once here, from the real
    /// filesystem root. Pressure comes from the agent's own cgroup when it runs
    /// inside a container and the host's `/proc/pressure` otherwise, and nothing
    /// at all on a host whose kernel publishes no pressure information. Disk
    /// performance comes from `/proc/diskstats`, which is not namespaced, so a
    /// containerized agent resolves no source rather than reporting its
    /// neighbours' I/O as its own.
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
            pressure: PressureReader::for_root(root),
            diskperf: DiskPerfReader::for_root(root),
        })
    }

    /// Enable or disable full-cmdline hashing for audited on-demand paths.
    pub fn with_cmdline_hash(mut self, enabled: bool) -> Self {
        self.include_cmdline_hash = enabled;
        self
    }

    /// Difference a primary-interface reading against the previous snapshot into
    /// rx/tx byte-rates, then record it for the next call. An absent reading —
    /// no primary interface resolves, or the resolved one is no longer tracked —
    /// clears the snapshot, so the next reading starts a fresh pair rather than
    /// being differenced across a gap of unknown length.
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
        // Both rate-shaped readings are differenced against the same instant, so
        // a slow sample never gives the network and the disk different ideas of
        // how long the interval was.
        let now = Instant::now();
        let (network_rx_bps, network_tx_bps) = self.net_rates(reading, now);
        let disk_perf = self.diskperf.read(now);

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
                    rank: process_rank(index),
                    basename: basename_of(process.exe(), process.name()),
                    cmdline_hash,
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

    /// An idle interface is a measurement, not a gap. Unchanged counters mean
    /// zero bytes moved in the interval, so the rate is `Some(0.0)` — reporting
    /// `None` instead would make a quiet link indistinguishable from a link
    /// whose rate could not be computed, and would break the "silent host"
    /// signal that a flat zero line gives an investigator.
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

    /// A host that reports no capacity has not been read yet. Dividing by it
    /// would emit NaN, which serialises as a null the detector cannot band.
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

    /// Network and virtual mounts do report more free space than size. That is
    /// a full-looking 0% at worst, never an underflow.
    #[test]
    fn disk_used_percent_clamps_free_above_total() {
        assert_eq!(disk_used_percent(500, 900), 0.0);
        assert_eq!(disk_used_percent(0, 100), 0.0);
    }

    /// FS01: a 120 GB system volume at 98 % beside a 2 TB data volume at 10 %.
    /// Pooling the bytes reports ~15 % and hides the volume that is about to
    /// fill; the fullest mount reports 98 %, which is what a threshold can act
    /// on, and one mount is counted critical.
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

    /// The threshold is inclusive: a mount sitting exactly on it is critical,
    /// and one a hair below is not.
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

    /// Every critical mount is counted, not just the worst one — three volumes
    /// past the line is a different morning from one.
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

    /// A mount whose platform reported no capacity is unmeasured, not full. It
    /// must neither win the worst-mount comparison nor be counted critical —
    /// pseudo-filesystems report this routinely and would otherwise alarm every
    /// host in the fleet.
    #[test]
    fn a_mount_reporting_no_capacity_takes_part_in_neither_number() {
        let mounts = [(0u64, 0u64), (1_000, 500), (0, 500)];

        let reduced = disk_reduction(mounts.into_iter()).expect("one mount reports capacity");

        assert_eq!(reduced.worst_used_percent, 50.0);
        assert_eq!(reduced.mounts_critical, 0);
    }

    /// Network and virtual mounts do report more free space than size. Clamped
    /// to 0 % by [`disk_used_percent`], such a mount is the emptiest possible
    /// one — never a wrapped value that wins the worst-mount comparison.
    #[test]
    fn a_mount_with_more_free_than_total_reads_as_empty() {
        let mounts = [(500u64, 900u64), (1_000, 250)];

        let reduced = disk_reduction(mounts.into_iter()).expect("mounts report capacity");

        assert_eq!(reduced.worst_used_percent, 75.0);
        assert_eq!(reduced.mounts_critical, 0);
    }

    /// A host with nothing mounted has no disk reading. Reporting 0 % would say
    /// its volumes are empty, which is a claim about disks it does not have.
    #[test]
    fn a_host_with_no_measurable_mount_has_no_reading() {
        let none: [(u64, u64); 0] = [];
        assert_eq!(disk_reduction(none.into_iter()), None);
        // Mounts that all report zero capacity are the same case: nothing was
        // measured, so there is nothing to report.
        assert_eq!(disk_reduction([(0u64, 0u64), (0, 128)].into_iter()), None);
    }

    /// Rank is the series key: the busiest process is rank 1, never rank 0.
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

    /// A kernel thread has no executable path, and some platforms report a
    /// directory-only path; both fall back to the reported process name rather
    /// than to an empty identity.
    #[test]
    fn basename_falls_back_to_the_process_name() {
        assert_eq!(basename_of(None, OsStr::new("kthreadd")), "kthreadd");
        assert_eq!(
            basename_of(Some(Path::new("/")), OsStr::new("init")),
            "init"
        );
    }

    /// The full path never leaves the host — only the last component does.
    #[test]
    fn basename_never_returns_the_full_path() {
        let basename = basename_of(
            Some(Path::new("/home/ivan/secret-project/build/agent")),
            OsStr::new("agent"),
        );
        assert_eq!(basename, "agent");
        assert!(!basename.contains('/'));
    }

    /// A reading with no predecessor establishes the baseline: rates are
    /// unknown, not zero, because nothing has been differenced yet.
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

    /// Each reading becomes the next one's baseline, so a steady link reports a
    /// steady rate rather than an ever-growing one.
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

    /// The primary interface moving is a new measurement series. The old
    /// counters are not comparable, so this tick reports nothing — and the new
    /// interface becomes the baseline for the next one.
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

    /// Losing the primary interface drops the baseline. Keeping it would
    /// difference the next reading across a gap of unknown length and report a
    /// rate averaged over a window that never happened.
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

    /// A reboot resets the kernel counters. Differencing across it would report
    /// a huge negative-turned-nonsense rate, so the tick reports nothing.
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

    /// An idle link is a measurement: unchanged counters mean zero bytes moved,
    /// which must stay distinguishable from "no rate available".
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
    fn top_process_count_must_fit_in_a_rank_byte() {
        assert!(matches!(
            SysinfoSampler::new(u8::MAX as usize + 1),
            Err(SamplerError::TopNTooLarge)
        ));
        assert!(SysinfoSampler::new(u8::MAX as usize).is_ok());
    }
}
