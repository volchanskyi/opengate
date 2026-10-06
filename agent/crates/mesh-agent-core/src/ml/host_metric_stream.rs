//! Folds 1 s host samples into 60 s [`ControlMessage::AgentMetricWindow`]s for the control loop.
//! The fold equals [`super::backfill::roll_to_60s`], so a live and a backfilled point agree.

use mesh_protocol::{ControlMessage, MetricDim};

use super::backfill::window_start_60s;
use super::sampler::MetricSample;
use super::store_sink::{
    sample_dim_values, series_dim_name, series_max_dim_name, series_reduction, WindowReduction,
    BACKFILL_SERIES,
};

/// The number of host-resource series streamed per window, in [`BACKFILL_SERIES`] order.
const DIMS: usize = BACKFILL_SERIES.len();

/// Folds 1 s host samples into 60 s metric windows, closing one when a later bucket starts.
#[derive(Debug, Default)]
pub struct HostMetricWindower {
    window: Option<i64>,
    sums: [f64; DIMS],
    /// Largest reading per dim, meaningful only where `counts` is non-zero.
    maxima: [f64; DIMS],
    /// Latest reading per dim by timestamp, which is what a stall vital publishes.
    lasts: [(i64, f64); DIMS],
    /// Folded samples per dim; a `None` reading is not counted, so averages divide by readings.
    counts: [u32; DIMS],
}

impl HostMetricWindower {
    /// Creates a windower with no open window.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Folds one 1 s sample, returning the closed window when `ts` starts a later 60 s bucket.
    pub fn push(&mut self, ts: i64, sample: &MetricSample) -> Option<ControlMessage> {
        let bucket = window_start_60s(ts);
        let closed = match self.window {
            Some(open) if open != bucket => self.close(),
            _ => None,
        };
        let values = sample_dim_values(sample);
        for ((((sum, max), last), count), v) in self
            .sums
            .iter_mut()
            .zip(self.maxima.iter_mut())
            .zip(self.lasts.iter_mut())
            .zip(self.counts.iter_mut())
            .zip(values)
        {
            if let Some(v) = v {
                *sum += v;
                if *count == 0 || v > *max {
                    *max = v;
                }
                if *count == 0 || ts >= last.0 {
                    *last = (ts, v);
                }
                *count += 1;
            }
        }
        self.window = Some(bucket);
        closed
    }

    /// Emits the open partial window, if any, leaving the windower empty.
    pub fn flush(&mut self) -> Option<ControlMessage> {
        self.close()
    }

    /// Discards the open partial window, so no window spans a maintenance interval.
    pub fn reset(&mut self) {
        self.window = None;
        self.sums = [0.0; DIMS];
        self.maxima = [0.0; DIMS];
        self.lasts = [(0, 0.0); DIMS];
        self.counts = [0; DIMS];
    }

    /// Builds the message for the open window and clears it; a dim without readings is omitted.
    /// The server assigns the authoritative tenant, so `tenant_id` is empty.
    fn close(&mut self) -> Option<ControlMessage> {
        let start = self.window?;
        let mut dims = Vec::with_capacity(DIMS);
        for ((((&series, sum), max), last), count) in BACKFILL_SERIES
            .iter()
            .zip(self.sums)
            .zip(self.maxima)
            .zip(self.lasts)
            .zip(self.counts)
        {
            if count == 0 {
                continue;
            }
            if let Some(name) = series_dim_name(series) {
                dims.push(MetricDim {
                    name: name.to_string(),
                    avg: match series_reduction(series) {
                        WindowReduction::Mean => sum / f64::from(count),
                        WindowReduction::Last => last.1,
                    },
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

    fn dim_of(msg: &ControlMessage, name: &str) -> Option<f64> {
        match msg {
            ControlMessage::AgentMetricWindow { dims, .. } => {
                dims.iter().find(|d| d.name == name).map(|d| d.avg)
            }
            other => panic!("expected AgentMetricWindow, got {other:?}"),
        }
    }

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
            stall_cpu_some: None,
            stall_mem_some: None,
            stall_mem_full: None,
            stall_io_some: None,
            stall_io_full: None,
            disk_await_ms: None,
            disk_queue_depth: None,
            processes: Vec::new(),
        };
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

    #[test]
    fn live_windows_equal_backfill_roll_to_60s() {
        let seq: Vec<(i64, MetricSample)> = (0..150)
            .map(|i| {
                let ts = 1_000 + i;
                let measurable = !(72..95).contains(&i);
                let s = MetricSample {
                    cpu_total_percent: 1.0 + (i as f32) * 0.37,
                    memory_used_percent: 20.0 + (i as f32) * 1.11,
                    disk_used_percent: measurable.then_some(55.5 + (i as f32) * 1.75),
                    disk_mounts_critical: measurable.then_some(u32::from(i as u8 % 3)),
                    network_rx_bps: Some(1_000.0 + (i as f64) * 512.0),
                    network_tx_bps: Some(2_000.0 + (i as f64) * 256.0),
                    stall_cpu_some: measurable.then_some((i as f32) * 0.13),
                    stall_mem_some: measurable.then_some((i as f32) * 0.07),
                    stall_mem_full: measurable.then_some((i as f32) * 0.03),
                    stall_io_some: measurable.then_some((i as f32) * 0.21),
                    stall_io_full: measurable.then_some((i as f32) * 0.11),
                    disk_await_ms: measurable.then_some(0.125 + (i as f32) * 0.5),
                    disk_queue_depth: Some((i as f32) * 0.25),
                    processes: Vec::new(),
                };
                (ts, s)
            })
            .collect();

        let mut w = HostMetricWindower::new();
        let mut live: Vec<ControlMessage> =
            seq.iter().filter_map(|(ts, s)| w.push(*ts, s)).collect();
        live.extend(w.flush());

        for (dim_idx, &series) in BACKFILL_SERIES.iter().enumerate() {
            let name = series_dim_name(series).unwrap();
            let raw: Vec<(Sample, bool)> = seq
                .iter()
                .filter_map(|(ts, s)| {
                    sample_dim_values(s)[dim_idx].map(|v| (Sample::new(*ts, v), false))
                })
                .collect();
            let rolled = roll_to_60s(&raw, series_reduction(series));

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
            stall_cpu_some: Some(0.0),
            stall_mem_some: Some(0.0),
            stall_mem_full: Some(0.0),
            stall_io_some: Some(0.0),
            stall_io_full: Some(0.0),
            disk_await_ms: Some(0.0),
            disk_queue_depth: Some(0.0),
            processes: Vec::new(),
        };
        // 1_700_000_040 is a 60 s boundary.
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
            stall_cpu_some: Some(1.0),
            stall_mem_some: Some(1.0),
            stall_mem_full: Some(1.0),
            stall_io_some: Some(1.0),
            stall_io_full: Some(1.0),
            disk_await_ms: Some(1.0),
            disk_queue_depth: Some(1.0),
            processes: Vec::new(),
        };
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
        let closed = w
            .push(1_700_000_060, &base)
            .expect("boundary closes window");
        assert_eq!(dim_of(&closed, "cpu.total"), Some(10.0));
        assert_eq!(dim_of(&closed, "net.rx_bps"), Some(100.0));
        assert_eq!(dim_of(&closed, "net.rx_bps.max"), Some(100.0));
        assert_eq!(dim_of(&closed, "net.tx_bps"), Some(200.0));
        assert_eq!(dim_of(&closed, "net.tx_bps.max"), Some(200.0));
    }

    #[test]
    fn a_full_window_ships_the_eighteen_dim_contract_in_order() {
        let mut w = HostMetricWindower::new();
        let s = MetricSample {
            cpu_total_percent: 1.0,
            memory_used_percent: 2.0,
            disk_used_percent: Some(3.0),
            disk_mounts_critical: Some(1),
            network_rx_bps: Some(4.0),
            network_tx_bps: Some(5.0),
            stall_cpu_some: Some(6.0),
            stall_mem_some: Some(7.0),
            stall_mem_full: Some(8.0),
            stall_io_some: Some(9.0),
            stall_io_full: Some(10.0),
            disk_await_ms: Some(11.0),
            disk_queue_depth: Some(12.0),
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
        assert_eq!(names.len(), 18);
    }

    #[test]
    fn a_five_second_io_stall_survives_as_the_maximum_and_not_the_average() {
        let mut w = HostMetricWindower::new();
        let sample = |await_ms: f32| MetricSample {
            cpu_total_percent: 20.0,
            memory_used_percent: 20.0,
            disk_used_percent: Some(30.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(0.0),
            network_tx_bps: Some(0.0),
            stall_cpu_some: None,
            stall_mem_some: None,
            stall_mem_full: None,
            stall_io_some: None,
            stall_io_full: None,
            disk_await_ms: Some(await_ms),
            disk_queue_depth: Some(1.0),
            processes: Vec::new(),
        };
        let base = 1_700_000_040; // a 60 s boundary
        for i in 0..60 {
            let await_ms = if (30..35).contains(&i) { 800.0 } else { 3.0 };
            assert!(w.push(base + i, &sample(await_ms)).is_none());
        }
        let closed = w
            .push(base + 60, &sample(3.0))
            .expect("the next minute closes it");

        let avg = dim_of(&closed, "disk.await_ms").expect("the average ships");
        assert!(
            (avg - (55.0 * 3.0 + 5.0 * 800.0) / 60.0).abs() < 1e-9,
            "the average reads {avg} ms, which no threshold would fire on"
        );
        assert!((avg - 69.4).abs() < 0.05);
        assert_eq!(
            dim_of(&closed, "disk.await_ms.max"),
            Some(800.0),
            "the maximum recovers the freeze"
        );
        assert_eq!(dim_of(&closed, "disk.queue_depth"), Some(1.0));
        assert_eq!(dim_of(&closed, "disk.queue_depth.max"), None);
    }

    #[test]
    fn a_containerized_agent_ships_no_disk_performance_dim() {
        let mut w = HostMetricWindower::new();
        let s = MetricSample {
            cpu_total_percent: 12.0,
            memory_used_percent: 34.0,
            disk_used_percent: Some(56.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(78.0),
            network_tx_bps: Some(90.0),
            stall_cpu_some: Some(1.0),
            stall_mem_some: Some(2.0),
            stall_mem_full: Some(3.0),
            stall_io_some: Some(44.0),
            stall_io_full: Some(41.0),
            disk_await_ms: None,
            disk_queue_depth: None,
            processes: Vec::new(),
        };
        assert!(w.push(1_700_000_000, &s).is_none());
        let closed = w.push(1_700_000_060, &s).expect("boundary closes window");

        for absent in ["disk.await_ms", "disk.await_ms.max", "disk.queue_depth"] {
            assert_eq!(dim_of(&closed, absent), None, "{absent} must not ship");
        }
        assert_eq!(
            dim_of(&closed, "stall.io.some"),
            Some(44.0),
            "the cgroup's own I/O stall is still measured"
        );
        assert_eq!(dim_of(&closed, "disk.used_percent"), Some(56.0));
    }

    #[test]
    fn a_host_without_pressure_ships_no_stall_dim() {
        let mut w = HostMetricWindower::new();
        let s = MetricSample {
            cpu_total_percent: 12.0,
            memory_used_percent: 34.0,
            disk_used_percent: Some(56.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(78.0),
            network_tx_bps: Some(90.0),
            stall_cpu_some: None,
            stall_mem_some: None,
            stall_mem_full: None,
            stall_io_some: None,
            stall_io_full: None,
            disk_await_ms: Some(1.5),
            disk_queue_depth: Some(2.5),
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
        assert!(
            !names.iter().any(|n| n.starts_with("stall.")),
            "no stall dim ships without pressure, got {names:?}"
        );
        assert_eq!(dim_of(&closed, "cpu.total"), Some(12.0));
        assert_eq!(dim_of(&closed, "cpu.total.max"), Some(12.0));
    }

    #[test]
    fn a_stall_vital_publishes_the_minutes_latest_reading() {
        let mut w = HostMetricWindower::new();
        let sample = |io: f32| MetricSample {
            cpu_total_percent: 5.0,
            memory_used_percent: 5.0,
            disk_used_percent: Some(5.0),
            disk_mounts_critical: Some(0),
            network_rx_bps: Some(0.0),
            network_tx_bps: Some(0.0),
            stall_cpu_some: Some(0.0),
            stall_mem_some: Some(0.0),
            stall_mem_full: Some(0.0),
            stall_io_some: Some(io),
            stall_io_full: Some(io / 2.0),
            disk_await_ms: None,
            disk_queue_depth: None,
            processes: Vec::new(),
        };
        let base = 1_700_000_040; // a 60 s boundary
        for i in 0..60 {
            let io = if i < 30 { 0.0 } else { (i - 29) as f32 * 2.0 };
            assert!(w.push(base + i, &sample(io)).is_none());
        }
        let closed = w
            .push(base + 60, &sample(0.0))
            .expect("the next minute closes it");

        assert_eq!(
            dim_of(&closed, "stall.io.some"),
            Some(60.0),
            "the minute publishes the kernel's latest reading"
        );
        assert_eq!(dim_of(&closed, "stall.io.full"), Some(30.0));
        assert_eq!(dim_of(&closed, "stall.io.some.max"), None);
    }
}
