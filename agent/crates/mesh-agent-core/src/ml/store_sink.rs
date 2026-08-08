//! Edge-Sentinel sampler → local store sink.
//!
//! Bridges a [`MetricSample`] (plus its ensemble anomaly verdict) into the
//! graduated agent-local [`LocalTsdb`]: the sovereign copy of min/max/last + 1 s
//! raw that central `avg`-only VictoriaMetrics does not keep. Each host metric
//! dimension is a fixed series; percentage gauges use ×100 fixed-point (lossless
//! to centi precision), while the net-rate gauges are rounded to whole
//! bytes/second and ride the adaptive integer path (lossless), so a live 10 s
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

/// Fixed-point scale for percentage gauges: centi precision, lossless.
const PERCENT_SCALE: i64 = 100;
/// Fixed-point scale for whole-number counts: unit precision, exact.
const COUNT_SCALE: i64 = 1;

/// Every host-metric series the backfill/telemetry path carries, in a stable
/// order. The single source of truth paired with [`series_dim_name`]. Ids are a
/// persistence contract — an agent upgrading in place reads rows it wrote under
/// the old ones — so a series is appended here, never renumbered or reused.
pub const BACKFILL_SERIES: [SeriesId; 6] = [
    SERIES_CPU,
    SERIES_MEM,
    SERIES_DISK,
    SERIES_NET_RX,
    SERIES_NET_TX,
    SERIES_DISK_MOUNTS_CRITICAL,
];

/// The stable central dimension label for a local series, or `None` for an
/// unknown series id. This label becomes the VM `dim=` label, so live telemetry
/// and reconnect backfill land in the *same* series — keep the two mappings
/// ([`series_dim_name`] / [`dim_series`]) in lockstep.
#[must_use]
pub fn series_dim_name(series: SeriesId) -> Option<&'static str> {
    match series {
        SERIES_CPU => Some("cpu.total"),
        SERIES_MEM => Some("mem.used_percent"),
        SERIES_DISK => Some("disk.used_percent"),
        SERIES_NET_RX => Some("net.rx_bps"),
        SERIES_NET_TX => Some("net.tx_bps"),
        SERIES_DISK_MOUNTS_CRITICAL => Some("disk.mounts_critical"),
        _ => None,
    }
}

/// The local series id for a central dimension label, or `None` if unknown.
/// Inverse of [`series_dim_name`]; used to resolve an on-demand deep-history
/// pull's `dim` back to a local series.
#[must_use]
pub fn dim_series(name: &str) -> Option<SeriesId> {
    match name {
        "cpu.total" => Some(SERIES_CPU),
        "mem.used_percent" => Some(SERIES_MEM),
        "disk.used_percent" => Some(SERIES_DISK),
        "net.rx_bps" => Some(SERIES_NET_RX),
        "net.tx_bps" => Some(SERIES_NET_TX),
        "disk.mounts_critical" => Some(SERIES_DISK_MOUNTS_CRITICAL),
        _ => None,
    }
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
        let dims = [
            (SERIES_CPU, Some(f64::from(sample.cpu_total_percent))),
            (SERIES_MEM, Some(f64::from(sample.memory_used_percent))),
            (SERIES_DISK, sample.disk_used_percent.map(f64::from)),
            (SERIES_NET_RX, sample.network_rx_bps),
            (SERIES_NET_TX, sample.network_tx_bps),
            (
                SERIES_DISK_MOUNTS_CRITICAL,
                sample.disk_mounts_critical.map(f64::from),
            ),
        ];
        for (series, value) in dims {
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
        dim_series, series_dim_name, BACKFILL_SERIES, SERIES_CPU, SERIES_DISK,
        SERIES_DISK_MOUNTS_CRITICAL, SERIES_MEM, SERIES_NET_RX, SERIES_NET_TX,
    };

    #[test]
    fn dim_name_and_series_are_inverse_and_total() {
        // Every backfill series has a stable label, and the label resolves back
        // to the same series — live telemetry and backfill must agree on the map.
        for series in BACKFILL_SERIES {
            let name = series_dim_name(series).expect("every backfill series has a label");
            assert_eq!(dim_series(name), Some(series), "round-trips for {name}");
        }
        assert_eq!(BACKFILL_SERIES.len(), 6);
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
            ],
            [0, 1, 2, 3, 4, 5]
        );
    }

    #[test]
    fn unknown_series_and_dim_have_no_mapping() {
        assert_eq!(series_dim_name(999), None);
        assert_eq!(dim_series("nope.unknown"), None);
    }
}
