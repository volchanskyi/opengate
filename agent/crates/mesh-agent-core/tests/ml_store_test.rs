//! Sampler to local store sink: raw samples with anomaly bits, rollups and snapshot reads
//! while the sampler keeps writing.

use edge_tsdb::store::Tier;
use edge_tsdb::Durability;
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_agent_core::ml::store_sink::{
    LocalStoreSink, SERIES_CPU, SERIES_DISK, SERIES_DISK_AWAIT_MS, SERIES_DISK_MOUNTS_CRITICAL,
    SERIES_DISK_QUEUE_DEPTH, SERIES_MEM, SERIES_NET_RX, SERIES_NET_TX, SERIES_STALL_CPU_SOME,
    SERIES_STALL_IO_FULL, SERIES_STALL_IO_SOME, SERIES_STALL_MEM_FULL, SERIES_STALL_MEM_SOME,
};

fn sample(cpu: f32, mem: f32, disk: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: Some(disk),
        disk_mounts_critical: Some(0),
        network_rx_bps: Some(1_000.0),
        network_tx_bps: Some(2_000.0),
        stall_cpu_some: Some(0.5),
        stall_mem_some: Some(0.25),
        stall_mem_full: Some(0.125),
        stall_io_some: Some(1.5),
        stall_io_full: Some(0.75),
        disk_await_ms: Some(0.125),
        disk_queue_depth: Some(2.375),
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
    for (i, (_s, a)) in cpu.iter().enumerate() {
        assert_eq!(*a, i as i64 % 17 == 0, "anomaly bit at {i}");
    }
    // Percentages are stored as fixed-point hundredths and read back exactly.
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

    let t1 = sink
        .store()
        .range_tier(SERIES_CPU, Tier::T1, i64::MIN, i64::MAX)
        .unwrap();
    assert!(!t1.is_empty());
    assert_eq!(t1[0].max, 24.0);
    assert_eq!(t1[0].min, 20.0);
}

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
            stall_cpu_some: Some(77.0),
            stall_mem_some: Some(88.0),
            stall_mem_full: Some(99.0),
            stall_io_some: Some(12.0),
            stall_io_full: Some(13.0),
            disk_await_ms: Some(14.0),
            disk_queue_depth: Some(15.0),
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
    assert_eq!(stored(SERIES_STALL_CPU_SOME), Some(77.0));
    assert_eq!(stored(SERIES_STALL_MEM_SOME), Some(88.0));
    assert_eq!(stored(SERIES_STALL_MEM_FULL), Some(99.0));
    assert_eq!(stored(SERIES_STALL_IO_SOME), Some(12.0));
    assert_eq!(stored(SERIES_STALL_IO_FULL), Some(13.0));
    assert_eq!(stored(SERIES_DISK_AWAIT_MS), Some(14.0));
    assert_eq!(stored(SERIES_DISK_QUEUE_DEPTH), Some(15.0));
}

#[test]
fn sub_millisecond_service_time_survives_the_store() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 1).unwrap();
    sink.record(9_000, &sample(10.0, 20.0, 30.0), false)
        .unwrap();
    sink.flush(Durability::Full).unwrap();

    let stored = |series| {
        sink.store()
            .range_raw(series, i64::MIN, i64::MAX)
            .unwrap()
            .first()
            .map(|(s, _)| s.value)
    };
    assert_eq!(stored(SERIES_DISK_AWAIT_MS), Some(0.125));
    assert_eq!(stored(SERIES_DISK_QUEUE_DEPTH), Some(2.375));
}

#[test]
fn a_sample_without_disk_performance_writes_no_row() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 1).unwrap();
    let mut s = sample(10.0, 20.0, 30.0);
    s.disk_await_ms = None;
    s.disk_queue_depth = None;
    sink.record(5_000, &s, false).unwrap();
    sink.flush(Durability::Full).unwrap();

    for series in [SERIES_DISK_AWAIT_MS, SERIES_DISK_QUEUE_DEPTH] {
        assert!(
            sink.store()
                .range_raw(series, i64::MIN, i64::MAX)
                .unwrap()
                .is_empty(),
            "series {series} has no row without a reading"
        );
    }
    assert_eq!(
        sink.store()
            .range_raw(SERIES_DISK, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        1
    );
}

#[test]
fn a_sample_without_pressure_writes_no_stall_row() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 1).unwrap();
    let mut s = sample(10.0, 20.0, 30.0);
    s.stall_cpu_some = None;
    s.stall_mem_some = None;
    s.stall_mem_full = None;
    s.stall_io_some = None;
    s.stall_io_full = None;
    sink.record(5_000, &s, false).unwrap();
    sink.flush(Durability::Full).unwrap();

    for series in [
        SERIES_STALL_CPU_SOME,
        SERIES_STALL_MEM_SOME,
        SERIES_STALL_MEM_FULL,
        SERIES_STALL_IO_SOME,
        SERIES_STALL_IO_FULL,
    ] {
        assert!(
            sink.store()
                .range_raw(series, i64::MIN, i64::MAX)
                .unwrap()
                .is_empty(),
            "series {series} has no row without a reading"
        );
    }
    assert_eq!(
        sink.store()
            .range_raw(SERIES_CPU, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        1
    );
}

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

#[test]
fn an_unmeasurable_disk_leaves_a_gap_rather_than_a_zero() {
    let dir = tempfile::tempdir().unwrap();
    let mut sink = LocalStoreSink::open(dir.path(), 8 * 1024 * 1024, 4).unwrap();
    for i in 0..10i64 {
        let mut s = sample(20.0, 30.0, 40.0);
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

    let snap = sink.snapshot().unwrap();
    assert_eq!(
        snap.range_raw(SERIES_CPU, i64::MIN, i64::MAX)
            .unwrap()
            .len(),
        100
    );

    for i in 100..200i64 {
        sink.record(1_000 + i, &sample(30.0, 40.0, 50.0), false)
            .unwrap();
    }
    sink.flush(Durability::Full).unwrap();

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
