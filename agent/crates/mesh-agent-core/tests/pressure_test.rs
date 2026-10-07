//! Kernel pressure-stall (PSI) reader tests.
//! Every test reads a fixture filesystem root, never the host's `/proc`.

use std::fs;
use std::path::{Path, PathBuf};

use mesh_agent_core::ml::pressure::{PressureReader, PressureSupport};
use tempfile::TempDir;

/// The kernel defines CPU `full` as zero, so a distinct value here catches a reader taking it.
const CPU_PRESSURE: &str = "some avg10=0.00 avg60=0.18 avg300=0.48 total=3735977\n\
                            full avg10=9.99 avg60=9.99 avg300=9.99 total=999\n";

const MEMORY_PRESSURE: &str = "some avg10=0.11 avg60=1.23 avg300=0.45 total=12345678\n\
                               full avg10=0.05 avg60=0.67 avg300=0.12 total=7654321\n";

const IO_PRESSURE: &str = "some avg10=0.46 avg60=5.31 avg300=2.48 total=10161629\n\
                           full avg10=0.46 avg60=5.29 avg300=2.45 total=9929348\n";

fn put(root: &Path, rel: &str, contents: &str) {
    let path = root.join(rel);
    fs::create_dir_all(path.parent().expect("a fixture file has a parent"))
        .expect("create the fixture directory");
    fs::write(&path, contents).expect("write the fixture file");
}

fn host_root() -> TempDir {
    let dir = tempfile::tempdir().expect("a temp fixture root");
    put(dir.path(), "proc/self/cgroup", "0::/\n");
    put(dir.path(), "proc/pressure/cpu", CPU_PRESSURE);
    put(dir.path(), "proc/pressure/memory", MEMORY_PRESSURE);
    put(dir.path(), "proc/pressure/io", IO_PRESSURE);
    dir
}

fn host_paths(root: &Path) -> [PathBuf; 3] {
    [
        root.join("proc/pressure/cpu"),
        root.join("proc/pressure/memory"),
        root.join("proc/pressure/io"),
    ]
}

#[test]
fn reads_avg60_for_every_stall_vital() {
    let root = host_root();

    let reader = PressureReader::for_root(root.path());
    let reading = reader.read();

    assert_eq!(reader.support(), PressureSupport::Supported);
    assert_eq!(reading.cpu_some, Some(0.18));
    assert_eq!(reading.mem_some, Some(1.23));
    assert_eq!(reading.mem_full, Some(0.67));
    assert_eq!(reading.io_some, Some(5.31));
    assert_eq!(reading.io_full, Some(5.29));
}

#[test]
fn the_cpu_full_line_is_never_read() {
    let root = host_root();

    let reading = PressureReader::for_root(root.path()).read();

    for value in [
        reading.cpu_some,
        reading.mem_some,
        reading.mem_full,
        reading.io_some,
        reading.io_full,
    ] {
        assert_ne!(value, Some(9.99), "no vital takes the CPU full line");
    }
}

#[test]
fn a_host_without_psi_is_unsupported_and_reports_nothing() {
    let root = tempfile::tempdir().expect("a temp fixture root");
    put(root.path(), "proc/self/cgroup", "0::/\n");

    let reader = PressureReader::for_root(root.path());
    let reading = reader.read();

    assert_eq!(reader.support(), PressureSupport::Unsupported);
    assert!(reader.paths().is_none(), "no source resolved");
    assert_eq!(reading.cpu_some, None);
    assert_eq!(reading.mem_some, None);
    assert_eq!(reading.mem_full, None);
    assert_eq!(reading.io_some, None);
    assert_eq!(reading.io_full, None);
}

#[test]
fn a_root_with_no_proc_at_all_is_unsupported() {
    let root = tempfile::tempdir().expect("a temp fixture root");

    let reader = PressureReader::for_root(root.path());

    assert_eq!(reader.support(), PressureSupport::Unsupported);
    assert_eq!(reader.read().cpu_some, None);
}

#[test]
fn malformed_pressure_costs_only_its_own_vital() {
    let cases = [
        ("truncated mid-field", "some avg10=0.00 avg60"),
        ("no avg60 field", "some avg10=0.00 avg300=0.00 total=0\n"),
        (
            "non-numeric avg60",
            "some avg10=0.00 avg60=abc avg300=0.00 total=0\n",
        ),
        ("not a number at all", "some avg60=nan\n"),
        ("an infinite average", "some avg60=inf\n"),
        ("an empty file", ""),
        (
            "no some line",
            "full avg10=0.00 avg60=1.00 avg300=0.00 total=0\n",
        ),
        ("a bare header", "cpu\n"),
        ("an empty value", "some avg60= avg300=0.00\n"),
    ];

    for (case, text) in cases {
        let root = host_root();
        put(root.path(), "proc/pressure/cpu", text);

        let reading = PressureReader::for_root(root.path()).read();

        assert_eq!(reading.cpu_some, None, "{case} yields no CPU reading");
        assert_eq!(
            reading.mem_some,
            Some(1.23),
            "{case} leaves the memory vitals untouched"
        );
        assert_eq!(
            reading.io_some,
            Some(5.31),
            "{case} leaves the I/O vitals untouched"
        );
    }
}

#[test]
fn a_value_outside_the_percentage_range_is_not_a_reading() {
    for text in [
        "some avg10=0.00 avg60=250.00 avg300=0.00 total=1\n",
        "some avg10=0.00 avg60=-5.00 avg300=0.00 total=1\n",
    ] {
        let root = host_root();
        put(root.path(), "proc/pressure/cpu", text);

        assert_eq!(PressureReader::for_root(root.path()).read().cpu_some, None);
    }
}

#[test]
fn the_ends_of_the_percentage_range_are_real_readings() {
    let root = host_root();
    put(
        root.path(),
        "proc/pressure/cpu",
        "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
    );
    put(
        root.path(),
        "proc/pressure/io",
        "some avg10=100.00 avg60=100.00 avg300=100.00 total=99\n\
         full avg10=100.00 avg60=100.00 avg300=100.00 total=99\n",
    );

    let reading = PressureReader::for_root(root.path()).read();

    assert_eq!(reading.cpu_some, Some(0.0));
    assert_eq!(reading.io_some, Some(100.0));
    assert_eq!(reading.io_full, Some(100.0));
}

#[test]
fn a_kernel_that_omits_the_full_line_still_reports_some() {
    let root = host_root();
    put(
        root.path(),
        "proc/pressure/memory",
        "some avg10=0.11 avg60=2.50 avg300=0.45 total=12345678\n",
    );

    let reading = PressureReader::for_root(root.path()).read();

    assert_eq!(reading.mem_some, Some(2.50));
    assert_eq!(reading.mem_full, None, "an absent line is an absent vital");
}

#[test]
fn a_missing_file_leaves_only_its_own_vitals_absent() {
    let root = host_root();
    fs::remove_file(root.path().join("proc/pressure/io")).expect("remove the io fixture");

    let reading = PressureReader::for_root(root.path()).read();

    assert_eq!(reading.cpu_some, Some(0.18));
    assert_eq!(reading.mem_some, Some(1.23));
    assert_eq!(reading.mem_full, Some(0.67));
    assert_eq!(reading.io_some, None);
    assert_eq!(reading.io_full, None);
}

#[test]
fn a_containerized_agent_reads_its_own_cgroup() {
    let root = host_root();
    let cgroup = "/system.slice/docker-9f3c.scope";
    put(root.path(), "proc/self/cgroup", &format!("0::{cgroup}\n"));
    let dir = format!("sys/fs/cgroup{cgroup}");
    put(
        root.path(),
        &format!("{dir}/cpu.pressure"),
        "some avg10=0.00 avg60=11.00 avg300=0.00 total=1\n",
    );
    put(
        root.path(),
        &format!("{dir}/memory.pressure"),
        "some avg10=0.00 avg60=22.00 avg300=0.00 total=1\n\
         full avg10=0.00 avg60=33.00 avg300=0.00 total=1\n",
    );
    put(
        root.path(),
        &format!("{dir}/io.pressure"),
        "some avg10=0.00 avg60=44.00 avg300=0.00 total=1\n\
         full avg10=0.00 avg60=55.00 avg300=0.00 total=1\n",
    );

    let reader = PressureReader::for_root(root.path());
    let paths = reader.paths().expect("the cgroup source resolved");
    let reading = reader.read();

    assert_eq!(paths.cpu, root.path().join(format!("{dir}/cpu.pressure")));
    assert_eq!(
        paths.memory,
        root.path().join(format!("{dir}/memory.pressure"))
    );
    assert_eq!(paths.io, root.path().join(format!("{dir}/io.pressure")));
    assert_eq!(reading.cpu_some, Some(11.0));
    assert_eq!(reading.mem_some, Some(22.0));
    assert_eq!(reading.mem_full, Some(33.0));
    assert_eq!(reading.io_some, Some(44.0));
    assert_eq!(reading.io_full, Some(55.0));
}

#[test]
fn an_agent_at_the_root_cgroup_reads_the_host() {
    let root = host_root();

    let reader = PressureReader::for_root(root.path());
    let paths = reader.paths().expect("the host source resolved");

    assert_eq!(
        [paths.cpu.clone(), paths.memory.clone(), paths.io.clone()],
        host_paths(root.path())
    );
    assert_eq!(reader.read().cpu_some, Some(0.18));
}

#[test]
fn a_container_without_cgroup_pressure_never_falls_back_to_the_host() {
    let root = host_root();
    put(
        root.path(),
        "proc/self/cgroup",
        "0::/system.slice/opengate\n",
    );

    let reader = PressureReader::for_root(root.path());
    let reading = reader.read();

    assert_eq!(reader.support(), PressureSupport::Unsupported);
    assert!(reader.paths().is_none(), "no source resolved");
    assert_eq!(
        reading.cpu_some, None,
        "the host's 0.18 is not this agent's"
    );
    assert_eq!(reading.io_some, None, "the host's 5.31 is not this agent's");
}

#[test]
fn a_cgroup_v1_hierarchy_reads_the_host() {
    let root = host_root();
    put(
        root.path(),
        "proc/self/cgroup",
        "12:pids:/user.slice\n11:memory:/user.slice\n10:cpu,cpuacct:/user.slice\n",
    );

    let reader = PressureReader::for_root(root.path());
    let paths = reader.paths().expect("the host source resolved");

    assert_eq!(paths.cpu, root.path().join("proc/pressure/cpu"));
    assert_eq!(reader.read().cpu_some, Some(0.18));
}

#[test]
fn a_host_without_a_cgroup_file_reads_the_host() {
    let root = host_root();
    fs::remove_file(root.path().join("proc/self/cgroup")).expect("remove the cgroup fixture");

    let reader = PressureReader::for_root(root.path());

    assert_eq!(reader.support(), PressureSupport::Supported);
    assert_eq!(reader.read().cpu_some, Some(0.18));
}
