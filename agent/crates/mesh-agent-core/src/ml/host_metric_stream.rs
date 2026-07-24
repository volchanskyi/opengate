//! Live host-metric streaming to central VictoriaMetrics.
//!
//! The 1 s sampler already computes every host-resource reading; this module
//! folds those readings into 10 s-average [`ControlMessage::AgentMetricWindow`]s
//! and hands them to the control loop. The averaging is byte-identical to the
//! reconnect-backfill roll-up ([`super::backfill::roll_to_10s`]): both key a
//! bucket by [`super::backfill::window_start_10s`] and report `sum/n`, so a
//! live point and a later gap-filled point for the same `(dim, ts)` are equal
//! and land in one central series. Net bytes are cumulative, exactly as backfill
//! writes them.

use mesh_protocol::{ControlMessage, MetricDim};

use super::backfill::window_start_10s;
use super::sampler::MetricSample;
use super::store_sink::{series_dim_name, BACKFILL_SERIES};

/// The number of host-resource series streamed per window, in [`BACKFILL_SERIES`]
/// order (`cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bytes`,
/// `net.tx_bytes`).
const DIMS: usize = BACKFILL_SERIES.len();

/// The per-dim readings of one sample, in [`BACKFILL_SERIES`] order — the same
/// mapping [`super::store_sink::LocalStoreSink::record`] persists, so the live
/// average and the backfilled average fold identical values.
fn sample_values(sample: &MetricSample) -> [f64; DIMS] {
    [
        f64::from(sample.cpu_total_percent),
        f64::from(sample.memory_used_percent),
        f64::from(sample.disk_used_percent),
        sample.network_rx_bytes as f64,
        sample.network_tx_bytes as f64,
    ]
}

/// Folds 1 s host samples into 10 s-average metric windows. Feed every sample
/// through [`push`](Self::push); it returns a closed window whenever a sample
/// crosses into a later 10 s bucket. A partial (still-open) window is never
/// emitted on its own — [`reset`](Self::reset) discards it across a maintenance
/// interval, and reconnect-backfill later fills any window that never closed.
#[derive(Debug, Default)]
pub struct HostMetricWindower {
    /// The start timestamp of the currently-accumulating window, or `None` when
    /// no sample has been folded since construction/reset/close.
    window: Option<i64>,
    /// Running per-dim sums for the open window, in [`BACKFILL_SERIES`] order.
    sums: [f64; DIMS],
    /// Number of samples folded into the open window.
    count: u32,
}

impl HostMetricWindower {
    /// A fresh windower with no open window.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Fold one 1 s sample stamped `ts`. Returns the just-closed window when this
    /// sample is the first of a later 10 s bucket, otherwise `None`. The closed
    /// window is stamped at its start second and carries the per-dim average of
    /// exactly the samples that fell in it.
    pub fn push(&mut self, ts: i64, sample: &MetricSample) -> Option<ControlMessage> {
        let bucket = window_start_10s(ts);
        let closed = match self.window {
            Some(open) if open != bucket => self.close(),
            _ => None,
        };
        let values = sample_values(sample);
        for (slot, v) in self.sums.iter_mut().zip(values) {
            *slot += v;
        }
        self.count += 1;
        self.window = Some(bucket);
        closed
    }

    /// Emit the currently-open partial window (if any), leaving the windower
    /// empty. Production never flushes a partial; this exists for a clean
    /// end-of-stream in tests and callers that deliberately close the tail.
    pub fn flush(&mut self) -> Option<ControlMessage> {
        self.close()
    }

    /// Discard the open partial window without emitting it, so no window spans a
    /// maintenance interval.
    pub fn reset(&mut self) {
        self.window = None;
        self.sums = [0.0; DIMS];
        self.count = 0;
    }

    /// Build the metric-window message for the open window and clear the
    /// accumulator. `None` when no samples are buffered. The server assigns the
    /// authoritative org, so `org_id` is left empty.
    fn close(&mut self) -> Option<ControlMessage> {
        let start = self.window?;
        if self.count == 0 {
            return None;
        }
        let n = f64::from(self.count);
        let dims = BACKFILL_SERIES
            .iter()
            .zip(self.sums)
            .filter_map(|(&series, sum)| {
                series_dim_name(series).map(|name| MetricDim {
                    name: name.to_string(),
                    avg: sum / n,
                })
            })
            .collect();
        self.reset();
        Some(ControlMessage::AgentMetricWindow {
            ts: start,
            org_id: String::new(),
            dims,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ml::backfill::roll_to_10s;
    use edge_tsdb::Sample;

    /// Drives the windower over a sample sequence and asserts the emitted
    /// per-dim averages are byte-identical to [`roll_to_10s`] on the same values
    /// — the invariant that keeps live and reconnect-backfill in one series.
    #[test]
    fn live_windows_equal_backfill_roll_to_10s() {
        // A sequence spanning three 10 s buckets with uneven, non-round readings
        // so a rounding divergence would surface. Net counters climb (cumulative).
        let seq: Vec<(i64, MetricSample)> = (0..25)
            .map(|i| {
                let ts = 1_000 + i;
                let s = MetricSample {
                    cpu_total_percent: 1.0 + (i as f32) * 0.37,
                    memory_used_percent: 20.0 + (i as f32) * 1.11,
                    disk_used_percent: 55.5,
                    network_rx_bytes: 1_000 + (i as u64) * 512,
                    network_tx_bytes: 2_000 + (i as u64) * 256,
                    processes: Vec::new(),
                };
                (ts, s)
            })
            .collect();

        // Live path: fold every sample, then flush the tail so all buckets emit.
        let mut w = HostMetricWindower::new();
        let mut live: Vec<ControlMessage> =
            seq.iter().filter_map(|(ts, s)| w.push(*ts, s)).collect();
        live.extend(w.flush());

        // Backfill path: roll each dim's raw samples independently.
        for (dim_idx, &series) in BACKFILL_SERIES.iter().enumerate() {
            let name = series_dim_name(series).unwrap();
            let raw: Vec<(Sample, bool)> = seq
                .iter()
                .map(|(ts, s)| (Sample::new(*ts, sample_values(s)[dim_idx]), false))
                .collect();
            let rolled = roll_to_10s(&raw);

            let live_points: Vec<(i64, f64)> = live
                .iter()
                .map(|msg| match msg {
                    ControlMessage::AgentMetricWindow { ts, dims, .. } => (*ts, dims[dim_idx].avg),
                    other => panic!("expected AgentMetricWindow, got {other:?}"),
                })
                .collect();

            assert_eq!(
                live_points, rolled,
                "live windows must equal roll_to_10s for dim {name}"
            );
        }
    }

    /// A sample landing exactly on a 10 s boundary opens a new window, so two
    /// consecutive windows are stamped exactly 10 s apart — clearing the server's
    /// per-message-type ingest floor (`ts - last >= 10`).
    #[test]
    fn consecutive_windows_are_ten_seconds_apart() {
        let mut w = HostMetricWindower::new();
        let s = MetricSample {
            cpu_total_percent: 5.0,
            memory_used_percent: 5.0,
            disk_used_percent: 5.0,
            network_rx_bytes: 0,
            network_tx_bytes: 0,
            processes: Vec::new(),
        };
        assert!(w.push(1_700_000_000, &s).is_none());
        let first = w.push(1_700_000_010, &s).expect("boundary closes window");
        let second = w
            .push(1_700_000_020, &s)
            .expect("next boundary closes window");
        let ts = |m: &ControlMessage| match m {
            ControlMessage::AgentMetricWindow { ts, .. } => *ts,
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        };
        assert_eq!(
            ts(&second) - ts(&first),
            10,
            "windows are exactly 10 s apart"
        );
    }
}
