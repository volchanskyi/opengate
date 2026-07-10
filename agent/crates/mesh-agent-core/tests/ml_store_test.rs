//! Edge-Sentinel sampler → local store sink (WS-14b).
//!
//! Proves the sampler path persists raw metric samples with their inline anomaly
//! bit into the graduated `LocalTsdb`, that the rollups are queryable, and that
//! detection can read past context from a stable MVCC snapshot while the sampler
//! keeps writing.

use edge_tsdb::store::Tier;
use edge_tsdb::Durability;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_agent_core::ml::store_sink::{LocalStoreSink, SERIES_CPU, SERIES_DISK, SERIES_MEM};

fn sample(cpu: f32, mem: f32, disk: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: disk,
        network_rx_bytes: 1_000,
        network_tx_bytes: 2_000,
        processes: Vec::new(),
    }
}

#[test]
fn records_raw_and_anomaly_bits_and_rolls_up() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 20).unwrap();
    for i in 0..120i64 {
        let anomaly = i % 17 == 0;
        sink.record(
            1_000 + i,
            &sample(20.0 + (i % 5) as f32, 55.5, 30.25),
            anomaly,
        )
        .unwrap();
    }
    sink.flush(Durability::Full).unwrap();

    let cpu = sink
        .store()
        .range_raw(SERIES_CPU, i64::MIN, i64::MAX)
        .unwrap();
    assert_eq!(cpu.len(), 120);
    // The inline anomaly bit is persisted alongside the raw sample.
    for (i, (_s, a)) in cpu.iter().enumerate() {
        assert_eq!(*a, i as i64 % 17 == 0, "anomaly bit at {i}");
    }
    // Centi-precision percentages are recovered losslessly (fixed-point ×100).
    let mem = sink
        .store()
        .range_raw(SERIES_MEM, i64::MIN, i64::MAX)
        .unwrap();
    assert!((mem[0].0.value - 55.5).abs() < 1e-6);
    let disk = sink
        .store()
        .range_raw(SERIES_DISK, i64::MIN, i64::MAX)
        .unwrap();
    assert!((disk[0].0.value - 30.25).abs() < 1e-6);

    // Rollups are queryable (min/max/last/avg the central avg-only VM cannot give).
    let t1 = sink
        .store()
        .range_tier(SERIES_CPU, Tier::T1, i64::MIN, i64::MAX)
        .unwrap();
    assert!(!t1.is_empty());
    assert_eq!(t1[0].max, 24.0);
    assert_eq!(t1[0].min, 20.0);
}

#[test]
fn detection_reads_past_context_from_a_stable_snapshot() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 10).unwrap();
    for i in 0..100i64 {
        sink.record(1_000 + i, &sample(30.0, 40.0, 50.0), false)
            .unwrap();
    }
    sink.flush(Durability::Full).unwrap();

    // Detection opens a snapshot of the past context...
    let snap = sink.snapshot().unwrap();
    assert_eq!(
        snap.range_raw(SERIES_CPU, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        100
    );

    // ...while the sampler keeps recording and flushing.
    for i in 100..200i64 {
        sink.record(1_000 + i, &sample(30.0, 40.0, 50.0), false)
            .unwrap();
    }
    sink.flush(Durability::Full).unwrap();

    // The snapshot is a stable view; a fresh read sees the new samples.
    assert_eq!(
        snap.range_raw(SERIES_CPU, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        100
    );
    assert_eq!(
        sink.store()
            .range_raw(SERIES_CPU, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        200
    );
}
