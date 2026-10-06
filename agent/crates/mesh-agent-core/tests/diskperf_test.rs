//! Disk-performance reader tests over fixture filesystem roots with an injected clock, so the
//! host's `/proc` and `/sys` are never read.

use std::fs;
use std::path::Path;
use std::time::{Duration, Instant};

use mesh_agent_core::ml::diskperf::{DiskPerfReader, DiskPerfReading, DiskPerfSupport};
use tempfile::TempDir;

/// One `/proc/diskstats` line in the 20-column shape of a current kernel.
/// Fields the reader does not use carry distinctive constants, so an off-by-one parse is visible.
fn line(
    name: &str,
    reads: u64,
    ms_read: u64,
    writes: u64,
    ms_write: u64,
    weighted_ms: u64,
) -> String {
    format!(
        "   8       0 {name} {reads} 111 222 {ms_read} {writes} 333 444 {ms_write} 0 555 {weighted_ms} 1 2 3 4 5 6\n"
    )
}

/// The same reading in the 14-column shape of an older kernel.
fn legacy_line(
    name: &str,
    reads: u64,
    ms_read: u64,
    writes: u64,
    ms_write: u64,
    weighted_ms: u64,
) -> String {
    format!(
        "   8       0 {name} {reads} 111 222 {ms_read} {writes} 333 444 {ms_write} 0 555 {weighted_ms}\n"
    )
}

fn put(root: &Path, rel: &str, contents: &str) {
    let path = root.join(rel);
    fs::create_dir_all(path.parent().expect("a fixture file has a parent"))
        .expect("create the fixture directory");
    fs::write(&path, contents).expect("write the fixture file");
}

fn block_devices(root: &Path, names: &[&str]) {
    for name in names {
        fs::create_dir_all(root.join("sys/block").join(name)).expect("create the block device dir");
    }
}

fn host_root(names: &[&str]) -> TempDir {
    let dir = tempfile::tempdir().expect("a temp fixture root");
    put(dir.path(), "proc/self/cgroup", "0::/\n");
    put(dir.path(), "proc/diskstats", "");
    block_devices(dir.path(), names);
    dir
}

/// Drives the reader over two `/proc/diskstats` contents one second apart and returns the second.
fn two_readings(root: &Path, before: &str, after: &str) -> (Option<f32>, Option<f32>) {
    let mut reader = DiskPerfReader::for_root(root);
    let t0 = Instant::now();
    put(root, "proc/diskstats", before);
    let first = reader.read(t0);
    assert_eq!(
        (first.await_ms, first.queue_depth),
        (None, None),
        "the first reading has no predecessor, so it establishes the baseline"
    );
    put(root, "proc/diskstats", after);
    let second = reader.read(t0 + Duration::from_secs(1));
    (second.await_ms, second.queue_depth)
}

#[test]
fn service_time_and_queue_depth_come_from_two_readings_a_second_apart() {
    let root = host_root(&["nvme0n1"]);

    let reading = two_readings(
        root.path(),
        &line("nvme0n1", 1_000, 10_000, 500, 6_000, 40_000),
        &line("nvme0n1", 1_060, 12_500, 540, 7_500, 42_800),
    );

    assert_eq!(reading, (Some(40.0), Some(2.8)));
}

#[test]
fn both_kernel_column_counts_parse_to_the_same_reading() {
    let root = host_root(&["sda"]);
    let modern = two_readings(
        root.path(),
        &line("sda", 1_000, 10_000, 500, 6_000, 40_000),
        &line("sda", 1_060, 12_500, 540, 7_500, 42_800),
    );

    let root = host_root(&["sda"]);
    let legacy = two_readings(
        root.path(),
        &legacy_line("sda", 1_000, 10_000, 500, 6_000, 40_000),
        &legacy_line("sda", 1_060, 12_500, 540, 7_500, 42_800),
    );

    assert_eq!(modern, (Some(40.0), Some(2.8)));
    assert_eq!(legacy, modern, "the same counters read the same either way");
}

#[test]
fn the_queue_depth_field_is_the_weighted_one_not_its_neighbour() {
    let root = host_root(&["sda"]);

    let (_, queue) = two_readings(
        root.path(),
        &line("sda", 0, 0, 0, 0, 0),
        &line("sda", 10, 100, 10, 100, 2_800),
    );

    assert_eq!(queue, Some(2.8), "555 here would mean the wrong column");
}

#[test]
fn only_whole_block_devices_are_measured() {
    let root = host_root(&["nvme0n1", "vda", "dm-0", "loop0", "ram0", "zram0"]);

    let reader = DiskPerfReader::for_root(root.path());

    assert_eq!(
        reader.devices(),
        vec!["dm-0".to_string(), "nvme0n1".to_string(), "vda".to_string()],
        "each whole device once; no partition, no loop, ram or zram device"
    );
}

#[test]
fn a_partition_never_enters_the_reduction() {
    let root = host_root(&["nvme0n1"]);

    let (await_ms, _) = two_readings(
        root.path(),
        &(line("nvme0n1", 1_000, 10_000, 0, 0, 0) + &line("nvme0n1p1", 1_000, 10_000, 0, 0, 0)),
        &(line("nvme0n1", 1_100, 10_500, 0, 0, 100)
            + &line("nvme0n1p1", 1_100, 110_000, 0, 0, 100)),
    );

    assert_eq!(
        await_ms,
        Some(5.0),
        "the whole device's 5 ms, never the partition line's 1 000 ms"
    );
}

#[test]
fn the_worst_device_is_chosen_independently_for_each_vital() {
    let root = host_root(&["nvme0n1", "sdb"]);

    let (await_ms, queue) = two_readings(
        root.path(),
        &(line("nvme0n1", 0, 0, 0, 0, 0) + &line("sdb", 0, 0, 0, 0, 0)),
        // nvme0n1: 100 I/Os, 4 000 ms → 40 ms each, 300 ms of queue time.
        // sdb: 1 000 I/Os, 2 000 ms → 2 ms each, 28 000 ms of queue time.
        &(line("nvme0n1", 60, 2_500, 40, 1_500, 300) + &line("sdb", 600, 1_200, 400, 800, 28_000)),
    );

    assert_eq!(await_ms, Some(40.0), "the wearing device's service time");
    assert_eq!(
        queue,
        Some(28.0),
        "the backup's queue, from the other device"
    );
}

#[test]
fn a_mapper_device_can_win_the_reduction() {
    let root = host_root(&["dm-0", "sda"]);

    let (await_ms, _) = two_readings(
        root.path(),
        &(line("dm-0", 0, 0, 0, 0, 0) + &line("sda", 0, 0, 0, 0, 0)),
        &(line("dm-0", 100, 8_000, 0, 0, 10) + &line("sda", 100, 200, 0, 0, 10)),
    );

    assert_eq!(await_ms, Some(80.0), "the mapper device's own latency");
}

#[test]
fn a_counter_reset_yields_no_reading_then_rebaselines() {
    let root = host_root(&["sda"]);
    let mut reader = DiskPerfReader::for_root(root.path());
    let t0 = Instant::now();

    put(
        root.path(),
        "proc/diskstats",
        &line("sda", 9_000, 90_000, 1_000, 10_000, 500_000),
    );
    let baseline = reader.read(t0);
    assert_eq!(
        baseline,
        DiskPerfReading::default(),
        "the first read is a baseline"
    );
    put(
        root.path(),
        "proc/diskstats",
        &line("sda", 10, 100, 5, 50, 20),
    );
    let on_reset = reader.read(t0 + Duration::from_secs(1));
    put(
        root.path(),
        "proc/diskstats",
        &line("sda", 110, 400, 5, 50, 1_020),
    );
    let after = reader.read(t0 + Duration::from_secs(2));

    assert_eq!((on_reset.await_ms, on_reset.queue_depth), (None, None));
    assert_eq!(after.await_ms, Some(3.0));
    assert_eq!(after.queue_depth, Some(1.0));
}

#[test]
fn a_device_that_disappeared_is_dropped_and_the_rest_still_report() {
    let root = host_root(&["sda", "sdb"]);

    let (await_ms, queue) = two_readings(
        root.path(),
        &(line("sda", 0, 0, 0, 0, 0) + &line("sdb", 0, 0, 0, 0, 0)),
        &line("sda", 100, 700, 0, 0, 1_500),
    );

    assert_eq!(await_ms, Some(7.0), "only the device still present");
    assert_eq!(queue, Some(1.5));
}

#[test]
fn a_device_that_just_appeared_carries_no_rate_yet() {
    let root = host_root(&["sda", "sdb"]);

    let (await_ms, queue) = two_readings(
        root.path(),
        &line("sda", 0, 0, 0, 0, 0),
        &(line("sda", 100, 700, 0, 0, 1_500) + &line("sdb", 9_999, 999_999, 0, 0, 999_999)),
    );

    assert_eq!(
        await_ms,
        Some(7.0),
        "the new device's counters are a baseline"
    );
    assert_eq!(queue, Some(1.5));
}

#[test]
fn an_idle_disk_has_no_service_time_but_a_real_queue_depth() {
    let root = host_root(&["sda"]);

    let (await_ms, queue) = two_readings(
        root.path(),
        &line("sda", 1_000, 10_000, 500, 6_000, 40_000),
        &line("sda", 1_000, 10_000, 500, 6_000, 40_000),
    );

    assert_eq!(await_ms, None, "no I/O completed, so nothing was serviced");
    assert_eq!(queue, Some(0.0), "an empty queue is a reading, not a gap");
}

#[test]
fn an_idle_device_does_not_dilute_a_busy_ones_service_time() {
    let root = host_root(&["sda", "sdb"]);

    let (await_ms, _) = two_readings(
        root.path(),
        &(line("sda", 100, 1_000, 0, 0, 0) + &line("sdb", 50, 50, 0, 0, 0)),
        &(line("sda", 200, 4_000, 0, 0, 0) + &line("sdb", 50, 50, 0, 0, 0)),
    );

    assert_eq!(await_ms, Some(30.0), "the busy device's own 3 000 ms / 100");
}

#[test]
fn a_vm_guest_reports_the_latency_it_observes_on_its_virtual_disk() {
    let root = host_root(&["vda"]);

    let (await_ms, queue) = two_readings(
        root.path(),
        &line("vda", 0, 0, 0, 0, 0),
        &line("vda", 200, 18_000, 100, 9_000, 31_000),
    );

    assert_eq!(
        await_ms,
        Some(90.0),
        "guest-observed service time is the signal"
    );
    assert_eq!(queue, Some(31.0));
}

#[test]
fn a_containerized_agent_reports_unsupported_and_never_the_hosts_figures() {
    let root = host_root(&["sda"]);
    put(
        root.path(),
        "proc/self/cgroup",
        "0::/system.slice/docker-9f3c.scope\n",
    );

    let mut reader = DiskPerfReader::for_root(root.path());
    let t0 = Instant::now();
    put(root.path(), "proc/diskstats", &line("sda", 0, 0, 0, 0, 0));
    let baseline = reader.read(t0);
    assert_eq!(
        baseline,
        DiskPerfReading::default(),
        "the first read is a baseline"
    );
    put(
        root.path(),
        "proc/diskstats",
        &line("sda", 100, 4_000, 0, 0, 2_800),
    );
    let reading = reader.read(t0 + Duration::from_secs(1));

    assert_eq!(reader.support(), DiskPerfSupport::Unsupported);
    assert!(reader.paths().is_none(), "no source resolved");
    assert!(reader.devices().is_empty(), "no device is measured");
    assert_eq!(
        reading.await_ms, None,
        "the host's 40 ms is not this agent's"
    );
    assert_eq!(
        reading.queue_depth, None,
        "the host's 2.8 is not this agent's"
    );
}

#[test]
fn an_agent_at_the_root_cgroup_measures_the_host() {
    let root = host_root(&["sda"]);

    let reader = DiskPerfReader::for_root(root.path());
    let paths = reader.paths().expect("the host source resolved");

    assert_eq!(reader.support(), DiskPerfSupport::Supported);
    assert_eq!(paths.diskstats, root.path().join("proc/diskstats"));
    assert_eq!(paths.sys_block, root.path().join("sys/block"));
}

#[test]
fn a_cgroup_v1_hierarchy_measures_the_host() {
    let root = host_root(&["sda"]);
    put(
        root.path(),
        "proc/self/cgroup",
        "12:pids:/user.slice\n11:memory:/user.slice\n",
    );

    let reader = DiskPerfReader::for_root(root.path());

    assert_eq!(reader.support(), DiskPerfSupport::Supported);
    assert_eq!(reader.devices(), vec!["sda".to_string()]);
}

#[test]
fn a_host_without_diskstats_is_unsupported() {
    let root = tempfile::tempdir().expect("a temp fixture root");
    put(root.path(), "proc/self/cgroup", "0::/\n");
    block_devices(root.path(), &["sda"]);

    let mut reader = DiskPerfReader::for_root(root.path());
    let reading = reader.read(Instant::now());

    assert_eq!(reader.support(), DiskPerfSupport::Unsupported);
    assert!(reader.paths().is_none());
    assert_eq!(reading.await_ms, None);
    assert_eq!(reading.queue_depth, None);
}

#[test]
fn a_host_without_a_block_device_listing_is_unsupported() {
    let root = tempfile::tempdir().expect("a temp fixture root");
    put(root.path(), "proc/self/cgroup", "0::/\n");
    put(root.path(), "proc/diskstats", &line("sda", 1, 2, 3, 4, 5));

    let mut reader = DiskPerfReader::for_root(root.path());

    assert_eq!(reader.support(), DiskPerfSupport::Unsupported);
    assert_eq!(reader.read(Instant::now()).await_ms, None);
}

#[test]
fn a_root_with_no_proc_at_all_is_unsupported() {
    let root = tempfile::tempdir().expect("a temp fixture root");

    let mut reader = DiskPerfReader::for_root(root.path());

    assert_eq!(reader.support(), DiskPerfSupport::Unsupported);
    assert_eq!(reader.read(Instant::now()).queue_depth, None);
}

#[test]
fn a_malformed_line_costs_only_itself() {
    let broken = [
        "   8       0 sdb 1 2 3\n",
        "   8       0 sdb 1 2 3 x 5 6 7 8 9 10 11\n",
        "major minor name reads\n",
        "\n",
        "   8       0 sdb\n",
    ];

    for text in broken {
        let root = host_root(&["sda", "sdb"]);
        let (await_ms, queue) = two_readings(
            root.path(),
            &(line("sda", 0, 0, 0, 0, 0) + text),
            &(line("sda", 100, 700, 0, 0, 1_500) + text),
        );

        assert_eq!(await_ms, Some(7.0), "unparsable line: {text:?}");
        assert_eq!(queue, Some(1.5), "unparsable line: {text:?}");
    }
}

#[test]
fn a_zero_length_interval_yields_no_queue_depth() {
    let root = host_root(&["sda"]);
    let mut reader = DiskPerfReader::for_root(root.path());
    let t0 = Instant::now();

    put(root.path(), "proc/diskstats", &line("sda", 0, 0, 0, 0, 0));
    let baseline = reader.read(t0);
    assert_eq!(
        baseline,
        DiskPerfReading::default(),
        "the first read is a baseline"
    );
    put(
        root.path(),
        "proc/diskstats",
        &line("sda", 100, 4_000, 0, 0, 2_800),
    );
    let reading = reader.read(t0);

    assert_eq!(reading.await_ms, Some(40.0));
    assert_eq!(reading.queue_depth, None, "no wall time to divide by");
}

#[test]
fn service_time_keeps_its_sub_millisecond_resolution() {
    let root = host_root(&["nvme0n1"]);

    let (await_ms, queue) = two_readings(
        root.path(),
        &line("nvme0n1", 0, 0, 0, 0, 0),
        &line("nvme0n1", 128, 16, 0, 0, 375),
    );

    assert_eq!(await_ms, Some(0.125));
    assert_eq!(queue, Some(0.375), "the queue is fractional too");
}
