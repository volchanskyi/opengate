//! Edge-Sentinel sampler → local store sink.
//!
//! Bridges a [`MetricSample`] (plus its ensemble anomaly verdict) into the
//! graduated agent-local [`LocalTsdb`]: the sovereign copy of min/max/last + 1 s
//! raw that central `avg`-only VictoriaMetrics does not keep. Each host metric
//! dimension is a fixed series; percentage gauges use ×100 fixed-point (lossless
//! to centi precision), while byte counters ride the adaptive integer path.
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
/// Cumulative received network bytes.
pub const SERIES_NET_RX: SeriesId = 3;
/// Cumulative transmitted network bytes.
pub const SERIES_NET_TX: SeriesId = 4;

/// Fixed-point scale for percentage gauges: centi precision, lossless.
const PERCENT_SCALE: i64 = 100;

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
    pub fn record(
        &mut self,
        ts: i64,
        sample: &MetricSample,
        anomaly: bool,
    ) -> Result<(), TsdbError> {
        let dims = [
            (SERIES_CPU, f64::from(sample.cpu_total_percent)),
            (SERIES_MEM, f64::from(sample.memory_used_percent)),
            (SERIES_DISK, f64::from(sample.disk_used_percent)),
            (SERIES_NET_RX, sample.network_rx_bytes as f64),
            (SERIES_NET_TX, sample.network_tx_bytes as f64),
        ];
        for (series, value) in dims {
            self.store.append(series, Sample::new(ts, value), anomaly)?;
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
