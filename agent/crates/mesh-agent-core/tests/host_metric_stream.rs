//! Integration coverage for the live host-metric 60 s windower — the emitter
//! that streams `cpu.total`/`mem.used_percent`/`disk.used_percent`/`net.rx_bps`/
//! `net.tx_bps`/`disk.mounts_critical`, each with the window maximum where a
//! spike is the signal, to central VictoriaMetrics on the same 60 s cadence
//! reconnect-backfill uses, so live and backfilled points land in one series.

use mesh_agent_core::ml::host_metric_stream::HostMetricWindower;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_protocol::ControlMessage;

/// Build a host sample with the given resource readings (disk as the worst
/// mount's used percentage, net as byte/second rates) and no processes.
fn sample(cpu: f32, mem: f32, disk: f32, rx: u64, tx: u64) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: Some(disk),
        disk_mounts_critical: Some(0),
        network_rx_bps: Some(rx as f64),
        network_tx_bps: Some(tx as f64),
        // Stall vitals are the kernel's own 60 s pressure averages. They are
        // derived from the gauges here only so each carries a distinct value
        // that changes across a window — every divisor is a power of two, so
        // the fixture values are exact in both f32 and f64.
        stall_cpu_some: Some(cpu / 8.0),
        stall_mem_some: Some(mem / 8.0),
        stall_mem_full: Some(mem / 16.0),
        stall_io_some: Some(disk / 8.0),
        stall_io_full: Some(disk / 16.0),
        // The disk-performance vitals are derived from the same gauges, at
        // the milli resolution they publish at, and average like every other
        // instantaneous reading rather than publishing a latest reading.
        disk_await_ms: Some(disk / 4.0),
        disk_queue_depth: Some(cpu / 32.0),
        processes: Vec::new(),
    }
}

/// Pull the (name, avg) dim pairs out of an emitted window, in order.
fn window_dims(msg: &ControlMessage) -> (i64, Vec<(String, f64)>) {
    match msg {
        ControlMessage::AgentMetricWindow {
            ts,
            tenant_id,
            dims,
        } => {
            assert!(tenant_id.is_empty(), "the agent never asserts a tenant");
            (*ts, dims.iter().map(|d| (d.name.clone(), d.avg)).collect())
        }
        other => panic!("expected AgentMetricWindow, got {other:?}"),
    }
}

/// Samples inside one 60 s window never emit; the window closes only when a
/// later-window sample arrives, and it is stamped at the window start and
/// carries the per-dim average — and, where the dim has one, the largest
/// reading — of exactly the samples in that window.
#[test]
fn closes_a_window_only_when_a_later_sample_arrives() {
    let mut w = HostMetricWindower::new();

    // Three samples in the 120..180 window — none close it.
    assert!(w.push(120, &sample(10.0, 40.0, 70.0, 1000, 2000)).is_none());
    assert!(w.push(133, &sample(20.0, 50.0, 72.0, 1200, 2200)).is_none());
    assert!(w.push(179, &sample(30.0, 60.0, 74.0, 1400, 2400)).is_none());

    // A sample in the next window closes the 120-window, stamped at 120.
    let emitted = w
        .push(180, &sample(99.0, 99.0, 99.0, 9999, 9999))
        .expect("a later-window sample closes the prior window");
    let (ts, dims) = window_dims(&emitted);
    assert_eq!(ts, 120, "window is stamped at its start");
    assert_eq!(
        dims,
        vec![
            ("cpu.total".to_string(), 20.0),        // mean(10,20,30)
            ("cpu.total.max".to_string(), 30.0),    // the minute's peak
            ("mem.used_percent".to_string(), 50.0), // mean(40,50,60)
            ("mem.used_percent.max".to_string(), 60.0),
            ("disk.used_percent".to_string(), 72.0), // mean(70,72,74)
            ("net.rx_bps".to_string(), 1200.0),      // mean(1000,1200,1400)
            ("net.rx_bps.max".to_string(), 1400.0),
            ("net.tx_bps".to_string(), 2200.0), // mean(2000,2200,2400)
            ("net.tx_bps.max".to_string(), 2400.0),
            ("disk.mounts_critical".to_string(), 0.0),
            // A stall vital publishes the window's last reading, not its mean:
            // the kernel already averaged each reading over the trailing 60 s.
            ("stall.cpu.some".to_string(), 3.75), // last of (1.25, 2.5, 3.75)
            ("stall.mem.some".to_string(), 7.5),  // last of (5.0, 6.25, 7.5)
            ("stall.mem.full".to_string(), 3.75),
            ("stall.io.some".to_string(), 9.25), // last of (8.75, 9.0, 9.25)
            ("stall.io.full".to_string(), 4.625),
            // Service time and queue depth are instantaneous readings like the
            // gauges above, so they average — and the service time ships the
            // minute's peak beside its mean.
            ("disk.await_ms".to_string(), 18.0), // mean(17.5, 18.0, 18.5)
            ("disk.await_ms.max".to_string(), 18.5),
            ("disk.queue_depth".to_string(), 0.625), // mean(0.3125, 0.625, 0.9375)
        ],
    );
}

/// A host whose mounts report no capacity streams **no** disk *capacity* dim at
/// all. A zero would read as "every volume is empty" — the same
/// one-name-two-meanings mistake the worst-mount reduction exists to fix — so an
/// unmeasurable disk is absent from the window while every other dim still
/// ships. The disk *performance* dims are among those: capacity is a property of
/// a mount and service time is a property of a device, so a host with nothing
/// measurable mounted still reports how fast its disks are.
#[test]
fn a_host_with_no_measurable_mount_streams_no_disk_capacity_dim() {
    let mut w = HostMetricWindower::new();
    let diskless = MetricSample {
        disk_used_percent: None,
        disk_mounts_critical: None,
        ..sample(10.0, 20.0, 0.0, 100, 200)
    };
    assert!(w.push(420, &diskless).is_none());
    assert!(w.push(425, &diskless).is_none());

    let (_, dims) = window_dims(&w.flush().expect("open window flushes"));
    let names: Vec<&str> = dims.iter().map(|(name, _)| name.as_str()).collect();
    assert_eq!(
        names,
        vec![
            "cpu.total",
            "cpu.total.max",
            "mem.used_percent",
            "mem.used_percent.max",
            "net.rx_bps",
            "net.rx_bps.max",
            "net.tx_bps",
            "net.tx_bps.max",
            "stall.cpu.some",
            "stall.mem.some",
            "stall.mem.full",
            "stall.io.some",
            "stall.io.full",
            "disk.await_ms",
            "disk.await_ms.max",
            "disk.queue_depth",
        ],
        "neither capacity dim rides a window with nothing to report"
    );
}

/// The critical-mount count rides the window as its own dim, averaged over the
/// window like every other reading: a count that changes mid-window reports the
/// share of the window it held, not a rounded-away integer.
#[test]
fn the_critical_mount_count_streams_as_its_own_dim() {
    let mut w = HostMetricWindower::new();
    let with_critical = |count: u32| MetricSample {
        disk_mounts_critical: Some(count),
        ..sample(10.0, 20.0, 91.0, 100, 200)
    };
    // Three samples: two mounts critical, then one, then one.
    assert!(w.push(540, &with_critical(2)).is_none());
    assert!(w.push(543, &with_critical(1)).is_none());
    assert!(w.push(549, &with_critical(1)).is_none());

    let (_, dims) = window_dims(&w.flush().expect("open window flushes"));
    let by_name = |name: &str| {
        dims.iter()
            .find(|(dim, _)| dim == name)
            .map(|(_, avg)| *avg)
    };
    assert_eq!(by_name("disk.mounts_critical"), Some(4.0 / 3.0));
    assert_eq!(by_name("disk.used_percent"), Some(91.0));
}

/// `flush` emits the still-open partial window (used by tests / never for a
/// production partial), stamped at the window start with the samples so far.
#[test]
fn flush_emits_the_open_partial_window() {
    let mut w = HostMetricWindower::new();
    assert!(w.push(240, &sample(10.0, 10.0, 10.0, 100, 200)).is_none());
    assert!(w.push(245, &sample(30.0, 30.0, 30.0, 300, 400)).is_none());

    let (ts, dims) = window_dims(&w.flush().expect("open window flushes"));
    assert_eq!(ts, 240);
    assert_eq!(dims[0], ("cpu.total".to_string(), 20.0));
    assert_eq!(dims[1], ("cpu.total.max".to_string(), 30.0));
    assert_eq!(dims[5], ("net.rx_bps".to_string(), 200.0));

    // After a flush the accumulator is empty.
    assert!(w.flush().is_none(), "nothing left after a flush");
}

/// `reset` discards the partial accumulator so no window spans a maintenance
/// interval — nothing is emitted for the discarded samples.
#[test]
fn reset_discards_the_partial_window() {
    let mut w = HostMetricWindower::new();
    assert!(w.push(300, &sample(50.0, 50.0, 50.0, 500, 600)).is_none());
    w.reset();
    // A later-window sample now closes nothing (the pre-reset window is gone).
    assert!(w.push(371, &sample(60.0, 60.0, 60.0, 700, 800)).is_none());
    assert!(w.flush().is_some(), "only the post-reset window survives");
}
