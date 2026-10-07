//! Disk performance vitals from `/proc/diskstats`: the worst device's average I/O service time
//! and queue depth. A containerized agent reads nothing, since the counters are host-wide.

use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::time::Instant;

use super::cgroup::in_container;

/// Fixed-point scale for the disk-performance vitals: milli precision, because service time
/// is sub-millisecond on NVMe. Readings are quantized to it as they are produced.
pub const DISK_PERF_SCALE: i64 = 1_000;

/// Whitespace-separated fields before the per-device statistics: major number,
/// minor number, device name.
const STAT_OFFSET: usize = 3;

/// Position of each statistic this reader uses; fields parse from the left, so kernels
/// writing eleven or seventeen statistics read alike.
const READS_COMPLETED: usize = 0;
const MS_READING: usize = 3;
const WRITES_COMPLETED: usize = 4;
const MS_WRITING: usize = 7;
/// The eleventh statistic, time-weighted I/O, from which queue depth derives.
const WEIGHTED_MS: usize = 10;
/// The statistics a line must carry to be read at all.
const REQUIRED_STATS: usize = WEIGHTED_MS + 1;

/// Whether this host publishes disk-performance counters the agent may read as its own.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum DiskPerfSupport {
    /// The host publishes both sources and the agent reads them.
    Supported,
    /// No source resolved: a containerized agent, or a kernel lacking `/proc/diskstats`
    /// or `/sys/block/`.
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

/// One read of the two disk-performance vitals; a `None` is a vital not produced this second.
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
    /// What accumulated between `self` and a later reading, or `None` when any counter
    /// went backwards after a reboot or wrap.
    fn since(self, before: Self) -> Option<Self> {
        Some(Self {
            ios: self.ios.checked_sub(before.ios)?,
            service_ms: self.service_ms.checked_sub(before.service_ms)?,
            weighted_ms: self.weighted_ms.checked_sub(before.weighted_ms)?,
        })
    }
}

/// Every measured device's counters at one instant; queue depth is time-weighted, so the
/// instant is part of the reading.
#[derive(Debug, Clone)]
struct Snapshot {
    at: Instant,
    devices: BTreeMap<String, Counters>,
}

/// Reads the disk-performance vitals from this host's own counters, resolving the source
/// once and differencing each [`read`](Self::read) against the previous one.
#[derive(Debug)]
pub struct DiskPerfReader {
    /// The resolved source, or `None` on a host that publishes none.
    paths: Option<DiskPerfPaths>,
    /// The previous reading that the next one differences against.
    prev: Option<Snapshot>,
}

impl DiskPerfReader {
    /// Resolves the disk-performance source under `root`; production passes `/`.
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

    /// The whole block devices measured right now, sorted; listed fresh on every call.
    #[must_use]
    pub fn devices(&self) -> Vec<String> {
        self.paths
            .as_ref()
            .map(|paths| whole_devices(&paths.sys_block).into_iter().collect())
            .unwrap_or_default()
    }

    /// Reads both vitals for the interval ending at `now`; the first call only sets the
    /// baseline and reports nothing.
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

/// The disk-performance source rooted at `root`. A container resolves nothing because
/// `/proc/diskstats` is host-wide, and `/sys/block/` is required to tell disks from partitions.
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

/// The whole block devices under `sys_block`: every entry except pseudo-devices.
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

/// Whether a block-device name is a memory or file pseudo-device; `dm-*` and `md*` layers
/// add user-visible latency and stay measured.
fn is_pseudo_device(name: &str) -> bool {
    name.starts_with("loop") || name.starts_with("ram") || name.starts_with("zram")
}

/// Parses `/proc/diskstats` into counters for `measured` devices, skipping lines too short
/// to carry the statistics needed.
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

/// The three derived counters of one device, or `None` for a short line or a non-numeric field.
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

/// Reduces two snapshots to the worst device per vital, chosen independently.
/// A device present in only one snapshot contributes nothing.
fn reduce(prev: &Snapshot, current: &Snapshot) -> DiskPerfReading {
    let elapsed_ms = current.at.saturating_duration_since(prev.at).as_secs_f64() * 1_000.0;
    let mut worst_await: Option<f64> = None;
    let mut worst_queue: Option<f64> = None;

    for (name, now) in &current.devices {
        let Some(delta) = prev.devices.get(name).and_then(|&before| now.since(before)) else {
            continue;
        };
        // A disk that served no I/O has no service time.
        if delta.ios > 0 {
            let await_ms = delta.service_ms as f64 / delta.ios as f64;
            worst_await = Some(worst_await.map_or(await_ms, |worst: f64| worst.max(await_ms)));
        }
        // An empty queue is a reading of zero; it is absent only without elapsed time.
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

/// Rounds a reading to the resolution the vital publishes at.
fn quantize(value: f64) -> f32 {
    let scale = DISK_PERF_SCALE as f64;
    ((value * scale).round() / scale) as f32
}

#[cfg(test)]
mod tests {
    use super::{is_pseudo_device, parse_diskstats, quantize, read_counters, Counters};
    use std::collections::BTreeSet;

    /// A real kernel's `/proc/diskstats` line for an NVMe device.
    const REFERENCE_LINE: &str =
        " 259       0 nvme0n1 336103 39377 25844842 90448 1116492 461544 68563152 664985 0 502556 785166 12 0 4 6 1027 1176";

    fn measured(names: &[&str]) -> BTreeSet<String> {
        names.iter().map(|n| (*n).to_string()).collect()
    }

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

    #[test]
    fn an_unmeasured_device_is_skipped() {
        let devices = parse_diskstats(REFERENCE_LINE, &measured(&["sda"]));

        assert!(devices.is_empty());
    }

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

    #[test]
    fn only_the_pseudo_devices_are_filtered_out() {
        for name in ["loop0", "loop12", "ram0", "zram0"] {
            assert!(is_pseudo_device(name), "{name} is a pseudo-device");
        }
        for name in ["nvme0n1", "vda", "xvda", "sda", "dm-0", "md0"] {
            assert!(!is_pseudo_device(name), "{name} is storage");
        }
    }

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

    #[test]
    fn a_reading_is_quantized_to_the_scale_it_publishes_at() {
        assert_eq!(quantize(0.125), 0.125);
        assert_eq!(quantize(40.5), 40.5);
        assert_eq!(quantize(2.0 / 3.0), 0.667);
    }
}
