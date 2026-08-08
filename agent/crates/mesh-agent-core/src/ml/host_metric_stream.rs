//! Live host-metric streaming to central VictoriaMetrics.
//!
//! The 1 s sampler already computes every host-resource reading; this module
//! folds those readings into 60 s [`ControlMessage::AgentMetricWindow`]s and
//! hands them to the control loop. Each window carries a per-dim average and,
//! for the gauges where a spike is the signal, the window's maximum: averaging
//! is what destroys a stall, not the sample rate, so a 5 s freeze inside a
//! minute that reads 26.7 % on average reads 100 % on the maximum.
//!
//! The fold is byte-identical to the reconnect-backfill roll-up
//! ([`super::backfill::roll_to_60s`]): both key a bucket by
//! [`super::backfill::window_start_60s`], report `sum/n`, and take the largest
//! raw reading, so a live point and a later gap-filled point for the same
//! `(dim, ts)` are equal and land in one central series. The net dims are
//! primary-interface throughput in bytes/second and the disk dims are the
//! worst-mount reduction; a sample carrying no reading for a dim — an
//! uncomputable net rate, or a host with no measurable mount — is skipped for
//! that dim alone (per-dim counts), exactly as the local store leaves a gap that
//! backfill then rolls over the same seconds.

use mesh_protocol::{ControlMessage, MetricDim};

use super::backfill::window_start_60s;
use super::sampler::MetricSample;
use super::store_sink::{sample_dim_values, series_dim_name, series_max_dim_name, BACKFILL_SERIES};

/// The number of host-resource series streamed per window, in [`BACKFILL_SERIES`]
/// order (`cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bps`,
/// `net.tx_bps`, `disk.mounts_critical`). Readings come from
/// [`sample_dim_values`], the same mapping
/// [`super::store_sink::LocalStoreSink::record`] persists, so the live average
/// and the backfilled average fold identical values. A `None` entry is skipped
/// from that dim's average. Four of these series also ship a maximum, so a
/// window carries up to ten dims.
const DIMS: usize = BACKFILL_SERIES.len();

/// Folds 1 s host samples into 60 s metric windows. Feed every sample through
/// [`push`](Self::push); it returns a closed window whenever a sample crosses
/// into a later 60 s bucket. A partial (still-open) window is never emitted on
/// its own — [`reset`](Self::reset) discards it across a maintenance interval,
/// and reconnect-backfill later fills any window that never closed.
#[derive(Debug, Default)]
pub struct HostMetricWindower {
    /// The start timestamp of the currently-accumulating window, or `None` when
    /// no sample has been folded since construction/reset/close.
    window: Option<i64>,
    /// Running per-dim sums for the open window, in [`BACKFILL_SERIES`] order.
    sums: [f64; DIMS],
    /// Largest reading seen this window per dim, in the same order. Meaningful
    /// only where `counts` is non-zero.
    maxima: [f64; DIMS],
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
    /// sample is the first of a later 60 s bucket, otherwise `None`. The closed
    /// window is stamped at its start second and carries the per-dim average and
    /// maximum of exactly the samples that fell in it.
    pub fn push(&mut self, ts: i64, sample: &MetricSample) -> Option<ControlMessage> {
        let bucket = window_start_60s(ts);
        let closed = match self.window {
            Some(open) if open != bucket => self.close(),
            _ => None,
        };
        let values = sample_dim_values(sample);
        for (((sum, max), count), v) in self
            .sums
            .iter_mut()
            .zip(self.maxima.iter_mut())
            .zip(self.counts.iter_mut())
            .zip(values)
        {
            if let Some(v) = v {
                *sum += v;
                if *count == 0 || v > *max {
                    *max = v;
                }
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
        self.maxima = [0.0; DIMS];
        self.counts = [0; DIMS];
    }

    /// Build the metric-window message for the open window and clear the
    /// accumulator. `None` when no samples are buffered. Each dim is averaged
    /// over its own sample count and a dim with no readings this window (e.g. a
    /// net rate that never resolved) is omitted, along with its maximum. The
    /// server assigns the authoritative tenant, so `tenant_id` is left empty.
    fn close(&mut self) -> Option<ControlMessage> {
        let start = self.window?;
        let mut dims = Vec::with_capacity(DIMS);
        for (((&series, sum), max), count) in BACKFILL_SERIES
            .iter()
            .zip(self.sums)
            .zip(self.maxima)
            .zip(self.counts)
        {
            if count == 0 {
                continue;
            }
            if let Some(name) = series_dim_name(series) {
                dims.push(MetricDim {
                    name: name.to_string(),
                    avg: sum / f64::from(count),
                });
            }
            if let Some(name) = series_max_dim_name(series) {
                dims.push(MetricDim {
                    name: name.to_string(),
                    avg: max,
                });
            }
        }
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
    use crate::ml::backfill::roll_to_60s;
    use edge_tsdb::Sample;

    /// Reads the dims of a closed window by name.
    fn dim_of(msg: &ControlMessage, name: &str) -> Option<f64> {
        match msg {
            ControlMessage::AgentMetricWindow { dims, .. } => {
                dims.iter().find(|d| d.name == name).map(|d| d.avg)
            }
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        }
    }

    /// The whole justification for shipping extrema: a 5 s freeze inside a 60 s
    /// window barely moves the average and pins the maximum. Without the maximum
    /// the central store cannot tell this minute from an idle one.
    #[test]
    fn a_five_second_stall_survives_as_the_maximum_and_not_the_average() {
        let mut w = HostMetricWindower::new();
        let sample = |cpu: f32| MetricSample {
            cpu_total_percent: cpu,
            memory_used_percent: 20.0,
            disk_used_percent: Some(30.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(0.0),
            network_tx_bps: Some(0.0),
            processes: Vec::new(),
        };
        // A minute that reads 20 % except for five consecutive pinned seconds.
        let base = 1_700_000_040; // a 60 s boundary
        for i in 0..60 {
            let cpu = if (30..35).contains(&i) { 100.0 } else { 20.0 };
            assert!(
                w.push(base + i, &sample(cpu)).is_none(),
                "the window stays open for its whole minute"
            );
        }
        let closed = w
            .push(base + 60, &sample(20.0))
            .expect("the next minute closes it");

        let avg = dim_of(&closed, "cpu.total").expect("the average ships");
        let max = dim_of(&closed, "cpu.total.max").expect("the maximum ships");
        assert!(
            (avg - (55.0 * 20.0 + 5.0 * 100.0) / 60.0).abs() < 1e-9,
            "the average reads {avg}, indistinguishable from noise"
        );
        assert!(
            (avg - 26.7).abs() < 0.05,
            "the average lands near 26.7 %, not near 100 %"
        );
        assert_eq!(max, 100.0, "the maximum recovers the freeze");
    }

    /// Drives the windower over a sample sequence and asserts the emitted
    /// per-dim averages and maxima are byte-identical to [`roll_to_60s`] on the
    /// same values — the invariant that keeps live and reconnect-backfill in one
    /// series, extended to the maxima so a max-of-averages on either side shows
    /// up here.
    #[test]
    fn live_windows_equal_backfill_roll_to_60s() {
        // A sequence spanning three 60 s buckets with uneven, non-round readings
        // so a rounding divergence would surface. Net counters climb (cumulative),
        // the critical-mount count steps, and one stretch carries no disk reading
        // at all so the disk dims fold over a different sample count than the
        // rest — a divergence between the two paths' per-dim counts shows up here.
        let seq: Vec<(i64, MetricSample)> = (0..150)
            .map(|i| {
                let ts = 1_000 + i;
                let measurable = !(72..95).contains(&i);
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
                    sample_dim_values(s)[dim_idx].map(|v| (Sample::new(*ts, v), false))
                })
                .collect();
            let rolled = roll_to_60s(&raw);

            // Look the dim up by name, not by position: a window that carried no
            // reading for it omits it entirely, and backfill omits the same
            // bucket, so both sides must agree on the absence too.
            let live_avg: Vec<(i64, f64)> = live
                .iter()
                .filter_map(|msg| match msg {
                    ControlMessage::AgentMetricWindow { ts, .. } => {
                        dim_of(msg, name).map(|v| (*ts, v))
                    }
                    other => panic!("expected AgentMetricWindow, got {other:?}"),
                })
                .collect();
            let rolled_avg: Vec<(i64, f64)> = rolled.iter().map(|p| (p.0, p.1)).collect();
            assert_eq!(
                live_avg, rolled_avg,
                "live windows must equal roll_to_60s for dim {name}"
            );

            let Some(max_name) = series_max_dim_name(series) else {
                continue;
            };
            let live_max: Vec<(i64, f64)> = live
                .iter()
                .filter_map(|msg| match msg {
                    ControlMessage::AgentMetricWindow { ts, .. } => {
                        dim_of(msg, max_name).map(|v| (*ts, v))
                    }
                    other => panic!("expected AgentMetricWindow, got {other:?}"),
                })
                .collect();
            let rolled_max: Vec<(i64, f64)> = rolled.iter().map(|p| (p.0, p.2)).collect();
            assert_eq!(
                live_max, rolled_max,
                "live windows must equal roll_to_60s for dim {max_name}"
            );
        }
    }

    /// A sample landing exactly on a 60 s boundary opens a new window, so two
    /// consecutive windows are stamped exactly 60 s apart — the central cadence,
    /// and comfortably clear of the server's per-message-type ingest floor.
    #[test]
    fn consecutive_windows_are_sixty_seconds_apart() {
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
        // 1_700_000_040 is a 60 s boundary, so the whole minute after it folds
        // into one window.
        assert!(w.push(1_700_000_040, &s).is_none());
        assert!(
            w.push(1_700_000_099, &s).is_none(),
            "a partial window never emits on its own"
        );
        let first = w.push(1_700_000_100, &s).expect("boundary closes window");
        let second = w
            .push(1_700_000_160, &s)
            .expect("next boundary closes window");
        let ts = |m: &ControlMessage| match m {
            ControlMessage::AgentMetricWindow { ts, .. } => *ts,
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        };
        assert_eq!(
            ts(&second) - ts(&first),
            60,
            "windows are exactly 60 s apart"
        );
    }

    /// A window whose first net rate is `None` (the first sample of a process, or
    /// an interface change) averages net over only the samples that carried a
    /// reading, while cpu/mem/disk average over every sample. The maximum follows
    /// the same per-dim count, so a dim that never resolved ships neither.
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
        // Two samples in the first 60 s bucket: net None then net 100/200.
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
            .push(1_700_000_060, &base)
            .expect("boundary closes window");
        // cpu averaged over both samples (10, 10) → 10.
        assert_eq!(dim_of(&closed, "cpu.total"), Some(10.0));
        // net averaged over the single sample that carried a rate → 100 / 1, and
        // its maximum is that same lone reading rather than a zero from the
        // sample that never resolved.
        assert_eq!(dim_of(&closed, "net.rx_bps"), Some(100.0));
        assert_eq!(dim_of(&closed, "net.rx_bps.max"), Some(100.0));
        assert_eq!(dim_of(&closed, "net.tx_bps"), Some(200.0));
        assert_eq!(dim_of(&closed, "net.tx_bps.max"), Some(200.0));
    }

    /// The window ships the full vocabulary and nothing else, in contract order.
    #[test]
    fn a_full_window_ships_the_ten_dim_contract_in_order() {
        let mut w = HostMetricWindower::new();
        let s = MetricSample {
            cpu_total_percent: 1.0,
            memory_used_percent: 2.0,
            disk_used_percent: Some(3.0),
            disk_mounts_critical: Some(1),
            network_rx_bps: Some(4.0),
            network_tx_bps: Some(5.0),
            processes: Vec::new(),
        };
        assert!(w.push(1_700_000_000, &s).is_none());
        let closed = w.push(1_700_000_060, &s).expect("boundary closes window");
        let names: Vec<String> = match &closed {
            ControlMessage::AgentMetricWindow { dims, .. } => {
                dims.iter().map(|d| d.name.clone()).collect()
            }
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        };
        assert_eq!(names, crate::ml::store_sink::central_dim_names());
    }
}
