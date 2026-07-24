//! Integration coverage for the live host-metric 10 s windower — the emitter
//! that streams `cpu.total`/`mem.used_percent`/`disk.used_percent`/`net.rx_bytes`/
//! `net.tx_bytes` to central VictoriaMetrics on the same 10 s cadence
//! reconnect-backfill uses, so live and backfilled points land in one series.

use mesh_agent_core::ml::host_metric_stream::HostMetricWindower;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_protocol::ControlMessage;

/// Build a host sample with the given resource readings and no processes.
fn sample(cpu: f32, mem: f32, disk: f32, rx: u64, tx: u64) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: disk,
        network_rx_bytes: rx,
        network_tx_bytes: tx,
        processes: Vec::new(),
    }
}

/// Pull the (name, avg) dim pairs out of an emitted window, in order.
fn window_dims(msg: &ControlMessage) -> (i64, Vec<(String, f64)>) {
    match msg {
        ControlMessage::AgentMetricWindow { ts, org_id, dims } => {
            assert!(org_id.is_empty(), "the agent never asserts an org");
            (*ts, dims.iter().map(|d| (d.name.clone(), d.avg)).collect())
        }
        other => panic!("expected AgentMetricWindow, got {other:?}"),
    }
}

/// Samples inside one 10 s window never emit; the window closes only when a
/// later-window sample arrives, and it is stamped at the window start and
/// carries the per-dim average of exactly the samples in that window.
#[test]
fn closes_a_window_only_when_a_later_sample_arrives() {
    let mut w = HostMetricWindower::new();

    // Three samples in the 100..110 window — none close it.
    assert!(w.push(100, &sample(10.0, 40.0, 70.0, 1000, 2000)).is_none());
    assert!(w.push(103, &sample(20.0, 50.0, 72.0, 1200, 2200)).is_none());
    assert!(w.push(109, &sample(30.0, 60.0, 74.0, 1400, 2400)).is_none());

    // A sample in the next window closes the 100-window, stamped at 100.
    let emitted = w
        .push(110, &sample(99.0, 99.0, 99.0, 9999, 9999))
        .expect("a later-window sample closes the prior window");
    let (ts, dims) = window_dims(&emitted);
    assert_eq!(ts, 100, "window is stamped at its start");
    assert_eq!(
        dims,
        vec![
            ("cpu.total".to_string(), 20.0),         // mean(10,20,30)
            ("mem.used_percent".to_string(), 50.0),  // mean(40,50,60)
            ("disk.used_percent".to_string(), 72.0), // mean(70,72,74)
            ("net.rx_bytes".to_string(), 1200.0),    // mean(1000,1200,1400)
            ("net.tx_bytes".to_string(), 2200.0),    // mean(2000,2200,2400)
        ],
    );
}

/// `flush` emits the still-open partial window (used by tests / never for a
/// production partial), stamped at the window start with the samples so far.
#[test]
fn flush_emits_the_open_partial_window() {
    let mut w = HostMetricWindower::new();
    assert!(w.push(200, &sample(10.0, 10.0, 10.0, 100, 200)).is_none());
    assert!(w.push(205, &sample(30.0, 30.0, 30.0, 300, 400)).is_none());

    let (ts, dims) = window_dims(&w.flush().expect("open window flushes"));
    assert_eq!(ts, 200);
    assert_eq!(dims[0], ("cpu.total".to_string(), 20.0));
    assert_eq!(dims[3], ("net.rx_bytes".to_string(), 200.0));

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
    assert!(w.push(311, &sample(60.0, 60.0, 60.0, 700, 800)).is_none());
    assert!(w.flush().is_some(), "only the post-reset window survives");
}
