//! Live host-metric streaming to central VictoriaMetrics.
//!
//! The 1 s sampler already computes every host-resource reading; this module
//! folds those readings into 10 s-average [`ControlMessage::AgentMetricWindow`]s
//! and hands them to the control loop. The averaging is byte-identical to the
//! reconnect-backfill roll-up ([`super::backfill::roll_to_10s`]): both key a
//! bucket by [`super::backfill::window_start_10s`] and report `sum/n`, so a
//! live point and a later gap-filled point for the same `(dim, ts)` are equal
//! and land in one central series. The net dims are primary-interface
//! throughput in bytes/second and the disk dims are the worst-mount reduction; a
//! sample carrying no reading for a dim — an uncomputable net rate, or a host
//! with no measurable mount — is skipped for that dim alone (per-dim counts),
//! exactly as the local store leaves a gap that backfill then rolls over the
//! same seconds.

use mesh_protocol::{ControlMessage, MetricDim};

use super::backfill::window_start_10s;
use super::sampler::MetricSample;
use super::store_sink::{series_dim_name, BACKFILL_SERIES};

/// The number of host-resource series streamed per window, in [`BACKFILL_SERIES`]
/// order (`cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bps`,
/// `net.tx_bps`, `disk.mounts_critical`).
const DIMS: usize = BACKFILL_SERIES.len();

/// The per-dim readings of one sample, in [`BACKFILL_SERIES`] order — the same
/// mapping [`super::store_sink::LocalStoreSink::record`] persists, so the live
/// average and the backfilled average fold identical values. A `None` entry (a
/// net rate before it can be computed, or the disk reduction on a host with no
/// measurable mount) is skipped from that dim's average.
fn sample_values(sample: &MetricSample) -> [Option<f64>; DIMS] {
    [
        Some(f64::from(sample.cpu_total_percent)),
        Some(f64::from(sample.memory_used_percent)),
        sample.disk_used_percent.map(f64::from),
        sample.network_rx_bps,
        sample.network_tx_bps,
        sample.disk_mounts_critical.map(f64::from),
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
    /// Per-dim count of folded samples — a dim whose value was `None` (an
    /// uncomputable net rate) is not counted, so its average divides only the
    /// samples that actually carried a reading.
    counts: [u32; DIMS],
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
        for ((sum, count), v) in self.sums.iter_mut().zip(self.counts.iter_mut()).zip(values) {
            if let Some(v) = v {
                *sum += v;
                *count += 1;
            }
        }
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
        self.counts = [0; DIMS];
    }

    /// Build the metric-window message for the open window and clear the
    /// accumulator. `None` when no samples are buffered. Each dim is averaged
    /// over its own sample count and a dim with no readings this window (e.g. a
    /// net rate that never resolved) is omitted. The server assigns the
    /// authoritative tenant, so `tenant_id` is left empty.
    fn close(&mut self) -> Option<ControlMessage> {
        let start = self.window?;
        let dims = BACKFILL_SERIES
            .iter()
            .zip(self.sums)
            .zip(self.counts)
            .filter_map(|((&series, sum), count)| {
                if count == 0 {
                    return None;
                }
                series_dim_name(series).map(|name| MetricDim {
                    name: name.to_string(),
                    avg: sum / f64::from(count),
                })
            })
            .collect::<Vec<_>>();
        self.reset();
        if dims.is_empty() {
            return None;
        }
        Some(ControlMessage::AgentMetricWindow {
            ts: start,
            tenant_id: String::new(),
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
        // so a rounding divergence would surface. Net counters climb (cumulative),
        // the critical-mount count steps, and one stretch carries no disk reading
        // at all so the disk dims fold over a different sample count than the
        // rest — a divergence between the two paths' per-dim counts shows up here.
        let seq: Vec<(i64, MetricSample)> = (0..25)
            .map(|i| {
                let ts = 1_000 + i;
                let measurable = !(12..15).contains(&i);
                let s = MetricSample {
                    cpu_total_percent: 1.0 + (i as f32) * 0.37,
                    memory_used_percent: 20.0 + (i as f32) * 1.11,
                    disk_used_percent: measurable.then_some(55.5 + (i as f32) * 1.75),
                    disk_mounts_critical: measurable.then_some(u32::from(i as u8 % 3)),
                    // Net dims are whole-byte/second rates.
                    network_rx_bps: Some(1_000.0 + (i as f64) * 512.0),
                    network_tx_bps: Some(2_000.0 + (i as f64) * 256.0),
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
                .filter_map(|(ts, s)| {
                    sample_values(s)[dim_idx].map(|v| (Sample::new(*ts, v), false))
                })
                .collect();
            let rolled = roll_to_10s(&raw);

            // Look the dim up by name, not by position: a window that carried no
            // reading for it omits it entirely, and backfill omits the same
            // bucket, so both sides must agree on the absence too.
            let live_points: Vec<(i64, f64)> = live
                .iter()
                .filter_map(|msg| match msg {
                    ControlMessage::AgentMetricWindow { ts, dims, .. } => dims
                        .iter()
                        .find(|dim| dim.name == name)
                        .map(|dim| (*ts, dim.avg)),
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
            disk_used_percent: Some(5.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(0.0),
            network_tx_bps: Some(0.0),
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

    /// A window whose first net rate is `None` (the first sample of a process, or
    /// an interface change) averages net over only the samples that carried a
    /// reading, while cpu/mem/disk average over every sample.
    #[test]
    fn net_none_is_excluded_from_only_the_net_average() {
        let mut w = HostMetricWindower::new();
        let base = MetricSample {
            cpu_total_percent: 10.0,
            memory_used_percent: 20.0,
            disk_used_percent: Some(30.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: None,
            network_tx_bps: None,
            processes: Vec::new(),
        };
        // Two samples in the first 10 s bucket: net None then net 100/200.
        assert!(w.push(1_700_000_000, &base).is_none());
        assert!(w
            .push(
                1_700_000_001,
                &MetricSample {
                    network_rx_bps: Some(100.0),
                    network_tx_bps: Some(200.0),
                    ..base.clone()
                }
            )
            .is_none());
        // Crossing into the next bucket closes the window.
        let closed = w
            .push(1_700_000_010, &base)
            .expect("boundary closes window");
        let dims = match closed {
            ControlMessage::AgentMetricWindow { dims, .. } => dims,
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        };
        let by_name = |name: &str| dims.iter().find(|d| d.name == name).map(|d| d.avg);
        // cpu averaged over both samples (10, 10) → 10.
        assert_eq!(by_name("cpu.total"), Some(10.0));
        // net averaged over the single sample that carried a rate → 100 / 1.
        assert_eq!(by_name("net.rx_bps"), Some(100.0));
        assert_eq!(by_name("net.tx_bps"), Some(200.0));
    }
}
