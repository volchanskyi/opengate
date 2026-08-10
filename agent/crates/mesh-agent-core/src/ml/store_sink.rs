//! Edge-Sentinel sampler → local store sink.
//!
//! Bridges a [`MetricSample`] (plus its ensemble anomaly verdict) into the
//! graduated agent-local [`LocalTsdb`]: the sovereign copy of min/max/last + 1 s
//! raw that central `avg`-only VictoriaMetrics does not keep. Each host metric
//! dimension is a fixed series; percentage gauges use ×100 fixed-point (lossless
//! to centi precision), while the net-rate gauges are rounded to whole
//! bytes/second and ride the adaptive integer path (lossless), so a live 60 s
//! average and a reconnect-backfilled average of the same seconds agree exactly.
//!
//! Writes are buffered and flushed on a cadence — never fsync-per-sample — so the
//! sampler stays inside the agent's <1 % CPU budget. Detection reads its recent
//! context from an MVCC [`snapshot`](LocalTsdb::snapshot) that is unaffected by
//! the sampler's concurrent writes.

use std::path::Path;

use edge_tsdb::store::TsdbSnapshot;
use edge_tsdb::{LocalTsdb, Sample, SeriesId, TsdbConfig, TsdbError};

pub use edge_tsdb::Durability;

use super::sampler::MetricSample;

/// Global CPU usage percentage.
pub const SERIES_CPU: SeriesId = 0;
/// Used-memory percentage.
pub const SERIES_MEM: SeriesId = 1;
/// Used-disk percentage.
pub const SERIES_DISK: SeriesId = 2;
/// Received throughput on the primary interface, bytes/second.
pub const SERIES_NET_RX: SeriesId = 3;
/// Transmitted throughput on the primary interface, bytes/second.
pub const SERIES_NET_TX: SeriesId = 4;
/// Count of mounted filesystems at or above the critical-usage threshold.
pub const SERIES_DISK_MOUNTS_CRITICAL: SeriesId = 5;
/// Percent of the last 60 s some task was stalled on CPU.
pub const SERIES_STALL_CPU_SOME: SeriesId = 6;
/// Percent of the last 60 s some task was stalled on memory.
pub const SERIES_STALL_MEM_SOME: SeriesId = 7;
/// Percent of the last 60 s every runnable task was stalled on memory.
pub const SERIES_STALL_MEM_FULL: SeriesId = 8;
/// Percent of the last 60 s some task was stalled on I/O.
pub const SERIES_STALL_IO_SOME: SeriesId = 9;
/// Percent of the last 60 s every runnable task was stalled on I/O.
pub const SERIES_STALL_IO_FULL: SeriesId = 10;

/// Fixed-point scale for percentage gauges: centi precision, lossless.
const PERCENT_SCALE: i64 = 100;
/// Fixed-point scale for whole-number counts: unit precision, exact.
const COUNT_SCALE: i64 = 1;

/// Every host-metric series the backfill/telemetry path carries, in a stable
/// order. The single source of truth paired with [`series_dim_name`]. Ids are a
/// persistence contract — an agent upgrading in place reads rows it wrote under
/// the old ones — so a series is appended here, never renumbered or reused.
pub const BACKFILL_SERIES: [SeriesId; 11] = [
    SERIES_CPU,
    SERIES_MEM,
    SERIES_DISK,
    SERIES_NET_RX,
    SERIES_NET_TX,
    SERIES_DISK_MOUNTS_CRITICAL,
    SERIES_STALL_CPU_SOME,
    SERIES_STALL_MEM_SOME,
    SERIES_STALL_MEM_FULL,
    SERIES_STALL_IO_SOME,
    SERIES_STALL_IO_FULL,
];

/// How a 60 s bucket reduces the 1 s samples of one series into the single
/// number it publishes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum WindowReduction {
    /// The mean of the bucket's readings. Each sample is an instantaneous
    /// reading of the resource, so the minute's average is the mean of the
    /// readings taken in it.
    Mean,
    /// The bucket's latest reading. The kernel has already averaged a stall
    /// vital over the trailing 60 s, so the last reading of a 60 s bucket *is*
    /// that bucket's average — while the mean of sixty overlapping 60 s
    /// averages spreads the minute across two and damps exactly the stall the
    /// vital exists to show.
    Last,
}

/// How `series` reduces over a 60 s bucket. Shared by the live windower and
/// reconnect-backfill so a live point and a gap-filled point for the same
/// `(dim, ts)` are the same number.
#[must_use]
pub fn series_reduction(series: SeriesId) -> WindowReduction {
    match series {
        SERIES_STALL_CPU_SOME
        | SERIES_STALL_MEM_SOME
        | SERIES_STALL_MEM_FULL
        | SERIES_STALL_IO_SOME
        | SERIES_STALL_IO_FULL => WindowReduction::Last,
        _ => WindowReduction::Mean,
    }
}

/// The stable central dimension label for a local series, or `None` for an
/// unknown series id. This label becomes the VM `dim=` label, so live telemetry
/// and reconnect backfill land in the *same* series — keep the three mappings
/// ([`series_dim_name`] / [`series_max_dim_name`] / [`dim_series`]) in lockstep.
#[must_use]
pub fn series_dim_name(series: SeriesId) -> Option<&'static str> {
    match series {
        SERIES_CPU => Some("cpu.total"),
        SERIES_MEM => Some("mem.used_percent"),
        SERIES_DISK => Some("disk.used_percent"),
        SERIES_NET_RX => Some("net.rx_bps"),
        SERIES_NET_TX => Some("net.tx_bps"),
        SERIES_DISK_MOUNTS_CRITICAL => Some("disk.mounts_critical"),
        SERIES_STALL_CPU_SOME => Some("stall.cpu.some"),
        SERIES_STALL_MEM_SOME => Some("stall.mem.some"),
        SERIES_STALL_MEM_FULL => Some("stall.mem.full"),
        SERIES_STALL_IO_SOME => Some("stall.io.some"),
        SERIES_STALL_IO_FULL => Some("stall.io.full"),
        _ => None,
    }
}

/// The central label for a series' per-window **maximum**, or `None` for a
/// series that ships no maximum.
///
/// Averaging is what destroys a stall, not the sample rate: over a 60 s window
/// at 1 Hz, five seconds pinned at 100 % move a 20 % average to 26.7 % — noise
/// — while the maximum reads 100. So the four gauges where a spike *is* the
/// signal each ship a companion maximum beside their average. `disk.used_percent`
/// moves too slowly for a within-minute peak to mean anything, and
/// `disk.mounts_critical` is already a threshold count, so neither has one. The
/// stall vitals ship none either: the kernel already reduced a whole minute of
/// stalling into each reading, so a maximum over a minute of those readings
/// answers no question the reading itself does not.
#[must_use]
pub fn series_max_dim_name(series: SeriesId) -> Option<&'static str> {
    match series {
        SERIES_CPU => Some("cpu.total.max"),
        SERIES_MEM => Some("mem.used_percent.max"),
        SERIES_NET_RX => Some("net.rx_bps.max"),
        SERIES_NET_TX => Some("net.tx_bps.max"),
        _ => None,
    }
}

/// Every central dim name, in the order a window emits them: each series'
/// average followed by its maximum where it has one. This is the whole
/// agent-side vocabulary of `opengate_edge_metric_avg`, and the count the
/// server's allowlist and the central cardinality cap are measured against.
#[must_use]
pub fn central_dim_names() -> Vec<&'static str> {
    BACKFILL_SERIES
        .iter()
        .flat_map(|&series| {
            series_dim_name(series)
                .into_iter()
                .chain(series_max_dim_name(series))
        })
        .collect()
}

/// The local series id for a central dimension label, or `None` if unknown.
/// Inverse of [`series_dim_name`] and [`series_max_dim_name`] — a `.max` label
/// resolves to the series it summarizes, because a maximum is a reduction over
/// that series rather than a series of its own.
#[must_use]
pub fn dim_series(name: &str) -> Option<SeriesId> {
    match name {
        "cpu.total" | "cpu.total.max" => Some(SERIES_CPU),
        "mem.used_percent" | "mem.used_percent.max" => Some(SERIES_MEM),
        "disk.used_percent" => Some(SERIES_DISK),
        "net.rx_bps" | "net.rx_bps.max" => Some(SERIES_NET_RX),
        "net.tx_bps" | "net.tx_bps.max" => Some(SERIES_NET_TX),
        "disk.mounts_critical" => Some(SERIES_DISK_MOUNTS_CRITICAL),
        "stall.cpu.some" => Some(SERIES_STALL_CPU_SOME),
        "stall.mem.some" => Some(SERIES_STALL_MEM_SOME),
        "stall.mem.full" => Some(SERIES_STALL_MEM_FULL),
        "stall.io.some" => Some(SERIES_STALL_IO_SOME),
        "stall.io.full" => Some(SERIES_STALL_IO_FULL),
        _ => None,
    }
}

/// One sample's readings in [`BACKFILL_SERIES`] order — the single ordered
/// mapping from a [`MetricSample`] to the dims that leave the sampler. The local
/// store ([`LocalStoreSink::record`]) and the live stream share it, so the two
/// can never disagree about which reading is which series. A `None` is a reading
/// this sample does not carry — a net rate before it can be computed, or the
/// disk reduction on a host with no measurable mount, or every stall vital on a
/// host whose kernel publishes no pressure information — and leaves a gap rather
/// than writing a wrong number.
#[must_use]
pub(crate) fn sample_dim_values(sample: &MetricSample) -> [Option<f64>; BACKFILL_SERIES.len()] {
    [
        Some(f64::from(sample.cpu_total_percent)),
        Some(f64::from(sample.memory_used_percent)),
        sample.disk_used_percent.map(f64::from),
        sample.network_rx_bps,
        sample.network_tx_bps,
        sample.disk_mounts_critical.map(f64::from),
        sample.stall_cpu_some.map(f64::from),
        sample.stall_mem_some.map(f64::from),
        sample.stall_mem_full.map(f64::from),
        sample.stall_io_some.map(f64::from),
        sample.stall_io_full.map(f64::from),
    ]
}

/// A cadence-buffered writer from the sampler into the local store.
pub struct LocalStoreSink {
    store: LocalTsdb,
    commit_every: usize,
    since_commit: usize,
}

impl LocalStoreSink {
    /// Open (creating/migrating) the store under `path`, capped at `cap_bytes`,
    /// flushing durably every `commit_every` samples (the bounded-loss window).
    pub fn open(path: &Path, cap_bytes: u64, commit_every: usize) -> Result<Self, TsdbError> {
        let mut store = LocalTsdb::open(
            path,
            TsdbConfig {
                cap_bytes,
                ..TsdbConfig::default()
            },
        )?;
        store.set_scale(SERIES_CPU, PERCENT_SCALE);
        store.set_scale(SERIES_MEM, PERCENT_SCALE);
        store.set_scale(SERIES_DISK, PERCENT_SCALE);
        store.set_scale(SERIES_DISK_MOUNTS_CRITICAL, COUNT_SCALE);
        store.set_scale(SERIES_STALL_CPU_SOME, PERCENT_SCALE);
        store.set_scale(SERIES_STALL_MEM_SOME, PERCENT_SCALE);
        store.set_scale(SERIES_STALL_MEM_FULL, PERCENT_SCALE);
        store.set_scale(SERIES_STALL_IO_SOME, PERCENT_SCALE);
        store.set_scale(SERIES_STALL_IO_FULL, PERCENT_SCALE);
        Ok(Self {
            store,
            commit_every: commit_every.max(1),
            since_commit: 0,
        })
    }

    /// Report currently-free host-disk bytes so the cap backs off under host
    /// pressure (the sampler feeds this from `sysinfo`).
    pub fn set_host_free_bytes(&mut self, free: Option<u64>) {
        self.store.set_host_free_bytes(free);
    }

    /// Append one host sample across every metric series, stamping each with the
    /// window's `anomaly` verdict, and flush durably on the configured cadence.
    /// A series is appended only when the sample carries that reading; an absent
    /// one — a net rate before it can be computed, or a disk reduction on a host
    /// with no measurable mount — leaves a gap rather than writing a wrong
    /// number, and reconnect-backfill rolls the same gaps.
    pub fn record(
        &mut self,
        ts: i64,
        sample: &MetricSample,
        anomaly: bool,
    ) -> Result<(), TsdbError> {
        for (series, value) in BACKFILL_SERIES.into_iter().zip(sample_dim_values(sample)) {
            if let Some(value) = value {
                self.store.append(series, Sample::new(ts, value), anomaly)?;
            }
        }
        self.since_commit += 1;
        if self.since_commit >= self.commit_every {
            self.flush(Durability::Full)?;
        }
        Ok(())
    }

    /// Force a durable (or fast) flush of buffered samples now.
    pub fn flush(&mut self, durability: Durability) -> Result<(), TsdbError> {
        self.store.commit(durability)?;
        self.since_commit = 0;
        Ok(())
    }

    /// A stable MVCC snapshot of the store for detection/backfill context reads.
    pub fn snapshot(&self) -> Result<TsdbSnapshot, TsdbError> {
        self.store.snapshot()
    }

    /// Borrow the underlying store (range queries, cursor, cold-tier compaction).
    pub fn store(&self) -> &LocalTsdb {
        &self.store
    }

    /// Mutably borrow the underlying store (cursor advance, purge, compaction).
    pub fn store_mut(&mut self) -> &mut LocalTsdb {
        &mut self.store
    }
}

#[cfg(test)]
mod tests {
    use super::{
        central_dim_names, dim_series, series_dim_name, series_max_dim_name, series_reduction,
        SeriesId, WindowReduction, BACKFILL_SERIES, SERIES_CPU, SERIES_DISK,
        SERIES_DISK_MOUNTS_CRITICAL, SERIES_MEM, SERIES_NET_RX, SERIES_NET_TX,
        SERIES_STALL_CPU_SOME, SERIES_STALL_IO_FULL, SERIES_STALL_IO_SOME, SERIES_STALL_MEM_FULL,
        SERIES_STALL_MEM_SOME,
    };

    #[test]
    fn dim_name_and_series_are_inverse_and_total() {
        // Every backfill series has a stable label, and the label resolves back
        // to the same series — live telemetry and backfill must agree on the map.
        for series in BACKFILL_SERIES {
            let name = series_dim_name(series).expect("every backfill series has a label");
            assert_eq!(dim_series(name), Some(series), "round-trips for {name}");
        }
        assert_eq!(BACKFILL_SERIES.len(), 11);
    }

    /// Each series appears exactly once. A duplicate would double-count the dim
    /// in every live window and every backfill batch.
    #[test]
    fn each_series_appears_once_in_the_backfill_set() {
        let mut seen = BACKFILL_SERIES.to_vec();
        seen.sort_unstable();
        seen.dedup();
        assert_eq!(seen.len(), BACKFILL_SERIES.len());
    }

    /// Ids key rows in the on-disk store, so an agent upgrading in place reads
    /// its existing history under the ids it wrote. Pinning them here makes a
    /// renumber a test failure rather than a silent history loss.
    #[test]
    fn series_ids_are_a_persistence_contract() {
        assert_eq!(
            [
                SERIES_CPU,
                SERIES_MEM,
                SERIES_DISK,
                SERIES_NET_RX,
                SERIES_NET_TX,
                SERIES_DISK_MOUNTS_CRITICAL,
                SERIES_STALL_CPU_SOME,
                SERIES_STALL_MEM_SOME,
                SERIES_STALL_MEM_FULL,
                SERIES_STALL_IO_SOME,
                SERIES_STALL_IO_FULL,
            ],
            [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
        );
    }

    /// A `.max` label resolves back to the series it summarizes, and the four
    /// gauges that ship a maximum are exactly the four where a within-window
    /// spike is the signal.
    #[test]
    fn max_labels_resolve_to_the_series_they_summarize() {
        for series in BACKFILL_SERIES {
            if let Some(name) = series_max_dim_name(series) {
                assert_eq!(dim_series(name), Some(series), "round-trips for {name}");
                let base = series_dim_name(series).expect("a maximum implies an average");
                assert_eq!(
                    name,
                    format!("{base}.max"),
                    "a maximum is named for its average"
                );
            }
        }
        let with_max: Vec<SeriesId> = BACKFILL_SERIES
            .into_iter()
            .filter(|&s| series_max_dim_name(s).is_some())
            .collect();
        assert_eq!(
            with_max,
            vec![SERIES_CPU, SERIES_MEM, SERIES_NET_RX, SERIES_NET_TX]
        );
    }

    /// The emitted vocabulary is exactly fifteen names, each appearing once, in
    /// series order with every maximum beside its average. The central series
    /// cap is counted against this list, so a name added here is a deliberate
    /// spend of the remaining headroom.
    #[test]
    fn the_central_dim_vocabulary_is_fifteen_distinct_names() {
        let names = central_dim_names();
        assert_eq!(
            names,
            vec![
                "cpu.total",
                "cpu.total.max",
                "mem.used_percent",
                "mem.used_percent.max",
                "disk.used_percent",
                "net.rx_bps",
                "net.rx_bps.max",
                "net.tx_bps",
                "net.tx_bps.max",
                "disk.mounts_critical",
                "stall.cpu.some",
                "stall.mem.some",
                "stall.mem.full",
                "stall.io.some",
                "stall.io.full",
            ]
        );
        let mut sorted = names.clone();
        sorted.sort_unstable();
        sorted.dedup();
        assert_eq!(sorted.len(), names.len(), "no dim is emitted twice");
    }

    #[test]
    fn unknown_series_and_dim_have_no_mapping() {
        assert_eq!(series_dim_name(999), None);
        assert_eq!(series_max_dim_name(999), None);
        assert_eq!(series_max_dim_name(SERIES_DISK), None);
        assert_eq!(dim_series("nope.unknown"), None);
        assert_eq!(dim_series("disk.used_percent.max"), None);
    }

    /// The contract carries no `stall.cpu.full`: the kernel defines it as always
    /// zero, and a constant is not worth one of the twenty-four series a device
    /// may occupy.
    #[test]
    fn cpu_full_is_not_part_of_the_stall_vocabulary() {
        assert_eq!(dim_series("stall.cpu.full"), None);
        assert!(!central_dim_names().contains(&"stall.cpu.full"));
    }

    /// A stall vital is already the kernel's own 60 s average, so its bucket
    /// publishes the last reading in it; every other series is an instantaneous
    /// gauge whose bucket publishes the mean. Averaging sixty overlapping 60 s
    /// averages would spread a stall across two minutes and damp it in both.
    #[test]
    fn stall_vitals_publish_their_last_reading_and_the_gauges_their_mean() {
        for series in [
            SERIES_STALL_CPU_SOME,
            SERIES_STALL_MEM_SOME,
            SERIES_STALL_MEM_FULL,
            SERIES_STALL_IO_SOME,
            SERIES_STALL_IO_FULL,
        ] {
            assert_eq!(series_reduction(series), WindowReduction::Last);
        }
        for series in [
            SERIES_CPU,
            SERIES_MEM,
            SERIES_DISK,
            SERIES_NET_RX,
            SERIES_NET_TX,
            SERIES_DISK_MOUNTS_CRITICAL,
        ] {
            assert_eq!(series_reduction(series), WindowReduction::Mean);
        }
    }
}
