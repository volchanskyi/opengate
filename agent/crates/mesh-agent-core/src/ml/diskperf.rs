//! Disk performance from `/proc/diskstats` — the three Linux-only disk vitals.
//!
//! `disk.used_percent` answers "is the disk full". Nothing answered "is it
//! slow", and those are different questions about different entities: capacity
//! is per mount, service time is per physical device. This reader supplies the
//! second question's answer, from the counters `iostat` derives its own from.
//!
//! Two vitals come out of it, plus the window maximum of the first:
//!
//! - **`disk.await_ms`** — `Δ(ms_read + ms_write) / Δ(reads + writes)`, the
//!   average time one I/O took. On an SSD or NVMe fleet this is *the* health
//!   signal: a wearing, thermally-throttled or garbage-collecting device shows
//!   here while capacity and throughput look entirely normal.
//! - **`disk.queue_depth`** — `Δ(weighted_ms) / Δ(wall_ms)`, `iostat`'s
//!   `avgqu-sz`: how many I/Os were outstanding on average. It keeps scaling
//!   where a utilization percentage saturates, which is why no `%util`-shaped
//!   vital exists — SSD and NVMe service many I/Os in parallel, so a busy-time
//!   percentage pins at 100 % with substantial headroom left and says nothing.
//!
//! **Worst device, per vital, independently.** The device with the highest
//! service time and the device with the deepest queue are routinely different
//! machines' worth of trouble on one host — a wearing system disk beside a data
//! disk taking a backup — and each vital answers its own question. A mean across
//! them describes neither. Per-device detail rides alert evidence rather than
//! central series, so this reduction costs no cardinality.
//!
//! **The device filter is membership of `/sys/block/`**, which excludes
//! partitions by construction, minus `loop*`, `ram*` and `zram*`. `dm-*` and
//! `md*` are included deliberately: LUKS and RAID overhead is latency the user
//! actually experiences, and worst-of selection cannot double-count the way
//! summing would.
//!
//! **A containerized agent reports nothing here.** `/proc/diskstats` is not
//! namespaced, so an agent inside a container reads host-wide figures and would
//! attribute its neighbours' I/O to itself. cgroup v2 `io.stat` carries bytes and
//! I/O counts but no service time, so there is no honest substitute to fall back
//! to; the vitals are absent and the container's genuine I/O-stall signal comes
//! from `io.pressure` through [`stall.io.*`](super::pressure) instead.
//!
//! Every path resolves under an injectable root, so the container, the kernel
//! without `/proc/diskstats` and every malformed line are ordinary fixture
//! directories rather than platforms nobody can test on. Production passes `/`.

use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::time::Instant;

use super::cgroup::in_container;

/// Fixed-point scale for the disk-performance vitals: milli precision. Service
/// time is sub-millisecond on a healthy NVMe and queue depth is fractional, so
/// neither survives the centi scale the percentage gauges use — 0.125 ms would
/// store as 0.13 and a 0.375-deep queue as 0.38. Readings are quantized to this
/// scale as they are produced, so the number the local store holds, the number a
/// live window averages, and the number alert evidence quotes are the same one.
pub const DISK_PERF_SCALE: i64 = 1_000;

/// Whitespace-separated fields before the per-device statistics: major number,
/// minor number, device name.
const STAT_OFFSET: usize = 3;

/// Position of each statistic this reader uses, counting from the first one.
/// Current kernels write seventeen and older ones eleven; both are parsed from
/// the left and any trailing field is ignored, so neither shape mis-parses.
const READS_COMPLETED: usize = 0;
const MS_READING: usize = 3;
const WRITES_COMPLETED: usize = 4;
const MS_WRITING: usize = 7;
/// The eleventh statistic — time-weighted I/O — from which queue depth comes.
/// Its neighbour at index 9 is total busy time, a plausible number that would
/// make a one-off error produce a believable wrong answer rather than an obvious
/// one.
const WEIGHTED_MS: usize = 10;
/// The statistics a line must carry to be read at all.
const REQUIRED_STATS: usize = WEIGHTED_MS + 1;

/// Whether this host publishes disk-performance counters the agent may read as
/// its own.
///
/// The state is reported rather than implied: coverage accounting distinguishes
/// a rule that is inactive from one the host cannot support, so a gap in the
/// fleet's disk-performance coverage is visible instead of reading as healthy.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum DiskPerfSupport {
    /// The host publishes both sources and the agent reads them.
    Supported,
    /// No source resolved — a containerized agent, or a kernel without
    /// `/proc/diskstats` or `/sys/block/`. The disk-performance vitals are
    /// absent for this host.
    Unsupported,
}

/// The two files a host reads: the per-device counters, and the listing that
/// says which of those devices are whole disks.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DiskPerfPaths {
    /// The kernel's per-device I/O counters.
    pub diskstats: PathBuf,
    /// The block-device listing that is the device filter.
    pub sys_block: PathBuf,
}

/// One read of the two disk-performance vitals. A `None` is a vital this host
/// did not produce this second — never a zero standing in for one.
#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct DiskPerfReading {
    /// Average service time per I/O on the slowest device, in milliseconds.
    pub await_ms: Option<f32>,
    /// Average number of I/Os outstanding on the most backed-up device.
    pub queue_depth: Option<f32>,
}

/// One device's cumulative counters at one instant.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
struct Counters {
    ios: u64,
    service_ms: u64,
    weighted_ms: u64,
}

impl Counters {
    /// What accumulated between `self` and a later reading, or `None` when any
    /// counter went backwards — a reboot or a wrap. Differencing across one would
    /// report a hugely negative service time or an astronomical queue, so the
    /// device contributes nothing to that sample and its new counters become the
    /// next baseline.
    fn since(self, before: Self) -> Option<Self> {
        Some(Self {
            ios: self.ios.checked_sub(before.ios)?,
            service_ms: self.service_ms.checked_sub(before.service_ms)?,
            weighted_ms: self.weighted_ms.checked_sub(before.weighted_ms)?,
        })
    }
}

/// Every measured device's counters at one instant, with the instant itself:
/// queue depth is time-weighted, so the wall clock is part of the reading.
#[derive(Debug, Clone)]
struct Snapshot {
    at: Instant,
    devices: BTreeMap<String, Counters>,
}

/// Reads the disk-performance vitals from this host's own counters.
///
/// The source is resolved once, at construction; each [`read`](Self::read) then
/// costs one file read plus a directory listing, and differences against the
/// previous reading.
#[derive(Debug)]
pub struct DiskPerfReader {
    /// The resolved source, or `None` on a host that publishes none the agent
    /// may claim as its own.
    paths: Option<DiskPerfPaths>,
    /// The previous reading, held so the next one can be differenced into rates.
    prev: Option<Snapshot>,
}

impl DiskPerfReader {
    /// Resolve this agent's disk-performance source under `root`. Production
    /// passes `/`; tests pass a fixture directory, which is how a container and a
    /// kernel without `/proc/diskstats` are exercised on a host that has both.
    #[must_use]
    pub fn for_root(root: &Path) -> Self {
        Self {
            paths: resolve(root),
            prev: None,
        }
    }

    /// Whether this host publishes disk-performance counters at all.
    #[must_use]
    pub fn support(&self) -> DiskPerfSupport {
        match self.paths {
            Some(_) => DiskPerfSupport::Supported,
            None => DiskPerfSupport::Unsupported,
        }
    }

    /// The resolved source files, or `None` when nothing resolved.
    #[must_use]
    pub fn paths(&self) -> Option<&DiskPerfPaths> {
        self.paths.as_ref()
    }

    /// The whole block devices measured right now, sorted. Read fresh rather
    /// than cached, so a volume attached after start is measured and one
    /// detached stops being.
    #[must_use]
    pub fn devices(&self) -> Vec<String> {
        self.paths
            .as_ref()
            .map(|paths| whole_devices(&paths.sys_block).into_iter().collect())
            .unwrap_or_default()
    }

    /// Read both vitals for the interval ending at `now`. The first call after
    /// construction establishes the baseline and reports nothing: both vitals are
    /// rates between two readings, and there is no honest value for the first.
    #[must_use]
    pub fn read(&mut self, now: Instant) -> DiskPerfReading {
        let Some(paths) = &self.paths else {
            return DiskPerfReading::default();
        };
        let text = fs::read_to_string(&paths.diskstats).unwrap_or_default();
        let current = Snapshot {
            at: now,
            devices: parse_diskstats(&text, &whole_devices(&paths.sys_block)),
        };
        let reading = self
            .prev
            .as_ref()
            .map_or_else(DiskPerfReading::default, |prev| reduce(prev, &current));
        self.prev = Some(current);
        reading
    }
}

/// The disk-performance source for an agent rooted at `root`, or `None` when
/// this host publishes none the agent may claim as its own.
///
/// A containerized agent resolves nothing: `/proc/diskstats` would be the host's
/// figures, its neighbours' I/O included. Both files are required — without
/// `/sys/block/` there is no way to tell a whole device from a partition, and
/// counting a partition beside the device it sits on is exactly the wrong-number
/// class this reader refuses.
fn resolve(root: &Path) -> Option<DiskPerfPaths> {
    if in_container(root) {
        return None;
    }
    let paths = DiskPerfPaths {
        diskstats: root.join("proc/diskstats"),
        sys_block: root.join("sys/block"),
    };
    (paths.diskstats.exists() && paths.sys_block.is_dir()).then_some(paths)
}

/// The whole block devices under `sys_block`: every entry of the listing, which
/// holds no partitions, minus the pseudo-devices whose latency is a property of
/// memory or of a file rather than of hardware the user waits for.
fn whole_devices(sys_block: &Path) -> BTreeSet<String> {
    let Ok(entries) = fs::read_dir(sys_block) else {
        return BTreeSet::new();
    };
    entries
        .flatten()
        .map(|entry| entry.file_name().to_string_lossy().into_owned())
        .filter(|name| !is_pseudo_device(name))
        .collect()
}

/// Whether a block-device name is a pseudo-device rather than storage the user
/// waits for. `dm-*` and `md*` are deliberately **not** here: the encryption and
/// RAID layers add latency a user experiences as surely as the platter under
/// them does.
fn is_pseudo_device(name: &str) -> bool {
    name.starts_with("loop") || name.starts_with("ram") || name.starts_with("zram")
}

/// Parse `/proc/diskstats` into per-device counters, keeping only `measured`
/// devices.
///
/// Fields are counted from the left and any trailing ones ignored, because the
/// column count is a property of the kernel version: current kernels append
/// discard and flush statistics that older ones never wrote. A line too short to
/// carry the statistics this reader needs is skipped — it costs only itself, and
/// never a half-parsed number.
fn parse_diskstats(text: &str, measured: &BTreeSet<String>) -> BTreeMap<String, Counters> {
    let mut devices = BTreeMap::new();
    for line in text.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        let Some(name) = fields.get(STAT_OFFSET - 1) else {
            continue;
        };
        if !measured.contains(*name) {
            continue;
        }
        let Some(counters) = read_counters(&fields[STAT_OFFSET..]) else {
            continue;
        };
        devices.insert((*name).to_string(), counters);
    }
    devices
}

/// The three derived counters of one device, or `None` when the line is too
/// short or any field this reader needs is not a number.
fn read_counters(stats: &[&str]) -> Option<Counters> {
    if stats.len() < REQUIRED_STATS {
        return None;
    }
    let at = |index: usize| stats.get(index)?.parse::<u64>().ok();
    Some(Counters {
        ios: at(READS_COMPLETED)?.checked_add(at(WRITES_COMPLETED)?)?,
        service_ms: at(MS_READING)?.checked_add(at(MS_WRITING)?)?,
        weighted_ms: at(WEIGHTED_MS)?,
    })
}

/// Reduce two snapshots to the worst device per vital, chosen independently.
///
/// A device present in only one of the two snapshots — hot-plugged, or detached
/// mid-window — contributes nothing: there is no interval to difference over,
/// and a rate computed across an unknown gap is a wrong number rather than a
/// missing one.
fn reduce(prev: &Snapshot, current: &Snapshot) -> DiskPerfReading {
    let elapsed_ms = current.at.saturating_duration_since(prev.at).as_secs_f64() * 1_000.0;
    let mut worst_await: Option<f64> = None;
    let mut worst_queue: Option<f64> = None;

    for (name, now) in &current.devices {
        let Some(delta) = prev.devices.get(name).and_then(|&before| now.since(before)) else {
            continue;
        };
        // A disk that served no I/O has no service time. Reporting 0 ms would
        // read as "instantaneous", the opposite of the truth, and would drag the
        // fleet's idea of normal service time toward zero on every quiet host.
        if delta.ios > 0 {
            let await_ms = delta.service_ms as f64 / delta.ios as f64;
            worst_await = Some(worst_await.map_or(await_ms, |worst: f64| worst.max(await_ms)));
        }
        // An empty queue *is* a measurement: nothing was waiting. It is absent
        // only when there is no wall time to divide by.
        if elapsed_ms > 0.0 {
            let depth = delta.weighted_ms as f64 / elapsed_ms;
            worst_queue = Some(worst_queue.map_or(depth, |worst: f64| worst.max(depth)));
        }
    }

    DiskPerfReading {
        await_ms: worst_await.map(quantize),
        queue_depth: worst_queue.map(quantize),
    }
}

/// Round a reading to the resolution the vital publishes at, so the value the
/// local store quantizes and the value a live window averages are the same one.
fn quantize(value: f64) -> f32 {
    let scale = DISK_PERF_SCALE as f64;
    ((value * scale).round() / scale) as f32
}

#[cfg(test)]
mod tests {
    use super::{is_pseudo_device, parse_diskstats, quantize, read_counters, Counters};
    use std::collections::BTreeSet;

    /// The reference host's own line, verbatim, with its expected counters
    /// computed by hand. The weighted field is the one a column mistake would
    /// silently get wrong, so it is pinned against a real kernel's output rather
    /// than a synthesized one.
    const REFERENCE_LINE: &str =
        " 259       0 nvme0n1 336103 39377 25844842 90448 1116492 461544 68563152 664985 0 502556 785166 12 0 4 6 1027 1176";

    fn measured(names: &[&str]) -> BTreeSet<String> {
        names.iter().map(|n| (*n).to_string()).collect()
    }

    /// Every field is read from its own position: 336 103 reads and 1 116 492
    /// writes, 90 448 ms reading and 664 985 ms writing, and 785 166 weighted ms
    /// — not the 502 556 of busy time sitting immediately before it.
    #[test]
    fn each_counter_comes_from_its_own_column() {
        let devices = parse_diskstats(REFERENCE_LINE, &measured(&["nvme0n1"]));

        assert_eq!(
            devices.get("nvme0n1"),
            Some(&Counters {
                ios: 336_103 + 1_116_492,
                service_ms: 90_448 + 664_985,
                weighted_ms: 785_166,
            })
        );
    }

    /// A device the filter did not admit is not parsed at all, however many
    /// lines `/proc/diskstats` carries for it.
    #[test]
    fn an_unmeasured_device_is_skipped() {
        let devices = parse_diskstats(REFERENCE_LINE, &measured(&["sda"]));

        assert!(devices.is_empty());
    }

    /// Every shape a line can take that is not eleven parsable statistics yields
    /// nothing for that device rather than a partial reading.
    #[test]
    fn a_line_that_cannot_be_read_yields_no_device() {
        for text in [
            "   8       0 sda",
            "   8       0 sda 1 2 3 4 5 6 7 8 9 10",
            "   8       0 sda 1 2 3 4 5 6 7 8 9 10 eleven",
            "   8       0 sda -1 2 3 4 5 6 7 8 9 10 11",
            "sda",
            "",
        ] {
            assert!(
                parse_diskstats(text, &measured(&["sda"])).is_empty(),
                "unreadable: {text:?}"
            );
        }
    }

    /// Field count grew with discard and flush statistics, so the eleven this
    /// reader needs are the same eleven in both shapes.
    #[test]
    fn trailing_kernel_fields_are_ignored() {
        let eleven = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"];
        let seventeen: Vec<&str> = eleven
            .iter()
            .copied()
            .chain(["12", "13", "14", "15", "16", "17"])
            .collect();

        assert_eq!(read_counters(&eleven), read_counters(&seventeen));
    }

    /// Loop, ram and zram devices are memory or a file wearing a block device's
    /// name; a mapper or RAID device is latency the user waits for.
    #[test]
    fn only_the_pseudo_devices_are_filtered_out() {
        for name in ["loop0", "loop12", "ram0", "zram0"] {
            assert!(is_pseudo_device(name), "{name} is a pseudo-device");
        }
        for name in ["nvme0n1", "vda", "xvda", "sda", "dm-0", "md0"] {
            assert!(!is_pseudo_device(name), "{name} is storage");
        }
    }

    /// A counter that went backwards has no honest delta — a wrap or a reboot
    /// happened between the readings.
    #[test]
    fn a_counter_going_backwards_has_no_delta() {
        let before = Counters {
            ios: 100,
            service_ms: 200,
            weighted_ms: 300,
        };
        let after = Counters {
            ios: 150,
            service_ms: 260,
            weighted_ms: 390,
        };

        assert_eq!(
            after.since(before),
            Some(Counters {
                ios: 50,
                service_ms: 60,
                weighted_ms: 90
            })
        );
        assert_eq!(before.since(after), None);
        assert_eq!(
            Counters {
                ios: 150,
                service_ms: 1,
                weighted_ms: 390
            }
            .since(before),
            None,
            "one field going backwards is enough"
        );
    }

    /// Readings are published at milli resolution: sub-millisecond service time
    /// survives, and a value below that resolution rounds rather than drifting.
    #[test]
    fn a_reading_is_quantized_to_the_scale_it_publishes_at() {
        assert_eq!(quantize(0.125), 0.125);
        assert_eq!(quantize(40.5), 40.5);
        assert_eq!(quantize(2.0 / 3.0), 0.667);
    }
}
