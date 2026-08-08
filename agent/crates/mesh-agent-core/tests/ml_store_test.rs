//! Edge-Sentinel sampler → local store sink (WS-14b).
//!
//! Proves the sampler path persists raw metric samples with their inline anomaly
//! bit into the graduated `LocalTsdb`, that the rollups are queryable, and that
//! detection can read past context from a stable MVCC snapshot while the sampler
//! keeps writing.

use edge_tsdb::store::Tier;
use edge_tsdb::Durability;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_agent_core::ml::store_sink::{
    LocalStoreSink, SERIES_CPU, SERIES_DISK, SERIES_DISK_MOUNTS_CRITICAL, SERIES_MEM,
    SERIES_NET_RX, SERIES_NET_TX,
};

fn sample(cpu: f32, mem: f32, disk: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: Some(disk),
        disk_mounts_critical: Some(0),
        network_rx_bps: Some(1_000.0),
        network_tx_bps: Some(2_000.0),
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

/// Every series stores its own field's reading. The sampler-to-series mapping is
/// positional, so two entries swapped in it would file each reading under the
/// other's name — a defect no averaging or round-trip test can see, because both
/// sides stay self-consistent. Each field therefore carries a value no other
/// field has, and each series is read back and matched to the one it owns.
#[test]
fn each_series_stores_the_field_it_names() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 1).unwrap();
    sink.record(
        4_000,
        &MetricSample {
            cpu_total_percent: 11.0,
            memory_used_percent: 22.0,
            disk_used_percent: Some(33.0),
            disk_mounts_critical: Some(66),
            network_rx_bps: Some(44.0),
            network_tx_bps: Some(55.0),
            processes: Vec::new(),
        },
        false,
    )
    .unwrap();
    sink.flush(Durability::Full).unwrap();

    let stored = |series| {
        sink.store()
            .range_raw(series, i64::MIN, i64::MAX)
            .unwrap()
            .first()
            .map(|(s, _)| s.value)
    };
    assert_eq!(stored(SERIES_CPU), Some(11.0));
    assert_eq!(stored(SERIES_MEM), Some(22.0));
    assert_eq!(stored(SERIES_DISK), Some(33.0));
    assert_eq!(stored(SERIES_NET_RX), Some(44.0));
    assert_eq!(stored(SERIES_NET_TX), Some(55.0));
    assert_eq!(stored(SERIES_DISK_MOUNTS_CRITICAL), Some(66.0));
}

/// The critical-mount count is a whole number, and it comes back out of the
/// store as the same whole number at every value the reduction can produce —
/// a count quantized to something coarser would report a mount set the host
/// never had.
#[test]
fn the_critical_mount_count_round_trips_exactly() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 4).unwrap();
    let counts: Vec<u32> = vec![0, 1, 2, 3, 7, 12, 64, 255];
    for (i, count) in counts.iter().enumerate() {
        let mut s = sample(20.0, 30.0, 91.0);
        s.disk_mounts_critical = Some(*count);
        sink.record(2_000 + i as i64, &s, false).unwrap();
    }
    sink.flush(Durability::Full).unwrap();

    let stored = sink
        .store()
        .range_raw(SERIES_DISK_MOUNTS_CRITICAL, i64::MIN, i64::MAX)
        .unwrap();
    let read_back: Vec<u32> = stored.iter().map(|(s, _)| s.value as u32).collect();
    assert_eq!(read_back, counts);
    for ((s, _), want) in stored.iter().zip(&counts) {
        assert_eq!(s.value, f64::from(*want), "count {want} is exact, not near");
    }
}

/// A host with no measurable mount writes no disk rows at all. A 0 would be
/// indistinguishable from an empty volume on every later read — the rollups, the
/// backfill drain, and the deep-history pull all read this tier.
#[test]
fn an_unmeasurable_disk_leaves_a_gap_rather_than_a_zero() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 4).unwrap();
    for i in 0..10i64 {
        let mut s = sample(20.0, 30.0, 40.0);
        // The middle three samples find nothing mounted.
        if (4..7).contains(&i) {
            s.disk_used_percent = None;
            s.disk_mounts_critical = None;
        }
        sink.record(3_000 + i, &s, false).unwrap();
    }
    sink.flush(Durability::Full).unwrap();

    for series in [SERIES_DISK, SERIES_DISK_MOUNTS_CRITICAL] {
        let stamps: Vec<i64> = sink
            .store()
            .range_raw(series, i64::MIN, i64::MAX)
            .unwrap()
            .iter()
            .map(|(s, _)| s.ts)
            .collect();
        assert_eq!(
            stamps,
            vec![3_000, 3_001, 3_002, 3_003, 3_007, 3_008, 3_009],
            "series {series} skips the unmeasurable seconds"
        );
    }
    // Every other series kept recording through the gap.
    assert_eq!(
        sink.store()
            .range_raw(SERIES_CPU, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        10
    );
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
