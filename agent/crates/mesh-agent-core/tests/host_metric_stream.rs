//! Integration coverage for the live host-metric 60 s windower, which emits per-dimension
//! averages (plus window maxima where a spike is the signal) on the backfill cadence.

use mesh_agent_core::ml::host_metric_stream::HostMetricWindower;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_protocol::ControlMessage;

fn sample(cpu: f32, mem: f32, disk: f32, rx: u64, tx: u64) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: Some(disk),
        disk_mounts_critical: Some(0),
        network_rx_bps: Some(rx as f64),
        network_tx_bps: Some(tx as f64),
        // Every divisor is a power of two, so the derived values are exact in f32 and f64.
        stall_cpu_some: Some(cpu / 8.0),
        stall_mem_some: Some(mem / 8.0),
        stall_mem_full: Some(mem / 16.0),
        stall_io_some: Some(disk / 8.0),
        stall_io_full: Some(disk / 16.0),
        disk_await_ms: Some(disk / 4.0),
        disk_queue_depth: Some(cpu / 32.0),
        processes: Vec::new(),
    }
}

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

#[test]
fn closes_a_window_only_when_a_later_sample_arrives() {
    let mut w = HostMetricWindower::new();

    assert!(w.push(120, &sample(10.0, 40.0, 70.0, 1000, 2000)).is_none());
    assert!(w.push(133, &sample(20.0, 50.0, 72.0, 1200, 2200)).is_none());
    assert!(w.push(179, &sample(30.0, 60.0, 74.0, 1400, 2400)).is_none());

    let emitted = w
        .push(180, &sample(99.0, 99.0, 99.0, 9999, 9999))
        .expect("a later-window sample closes the prior window");
    let (ts, dims) = window_dims(&emitted);
    assert_eq!(ts, 120, "window is stamped at its start");
    assert_eq!(
        dims,
        vec![
            ("cpu.total".to_string(), 20.0),
            ("cpu.total.max".to_string(), 30.0),
            ("mem.used_percent".to_string(), 50.0),
            ("mem.used_percent.max".to_string(), 60.0),
            ("disk.used_percent".to_string(), 72.0),
            ("net.rx_bps".to_string(), 1200.0),
            ("net.rx_bps.max".to_string(), 1400.0),
            ("net.tx_bps".to_string(), 2200.0),
            ("net.tx_bps.max".to_string(), 2400.0),
            ("disk.mounts_critical".to_string(), 0.0),
            // A stall vital publishes the window's last reading; the kernel already averages it.
            ("stall.cpu.some".to_string(), 3.75),
            ("stall.mem.some".to_string(), 7.5),
            ("stall.mem.full".to_string(), 3.75),
            ("stall.io.some".to_string(), 9.25),
            ("stall.io.full".to_string(), 4.625),
            ("disk.await_ms".to_string(), 18.0),
            ("disk.await_ms.max".to_string(), 18.5),
            ("disk.queue_depth".to_string(), 0.625),
        ],
    );
}

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

#[test]
fn the_critical_mount_count_streams_as_its_own_dim() {
    let mut w = HostMetricWindower::new();
    let with_critical = |count: u32| MetricSample {
        disk_mounts_critical: Some(count),
        ..sample(10.0, 20.0, 91.0, 100, 200)
    };
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

    assert!(w.flush().is_none(), "nothing left after a flush");
}

#[test]
fn reset_discards_the_partial_window() {
    let mut w = HostMetricWindower::new();
    assert!(w.push(300, &sample(50.0, 50.0, 50.0, 500, 600)).is_none());
    w.reset();
    assert!(w.push(371, &sample(60.0, 60.0, 60.0, 700, 800)).is_none());
    assert!(w.flush().is_some(), "only the post-reset window survives");
}
