//! Kernel pressure-stall (PSI) reader — the five Linux-only stall vitals.
//!
//! Every test drives the reader against a fixture filesystem root it builds
//! itself. None of them reads the host's `/proc`: the reference host has PSI, so
//! a host-reading test would pass here and prove nothing about a host without
//! it — and the whole point of this reader is what it answers on a host that
//! cannot supply the reading.

use std::fs;
use std::path::{Path, PathBuf};

use mesh_agent_core::ml::pressure::{PressureReader, PressureSupport};
use tempfile::TempDir;

/// A `/proc/pressure/cpu` in the kernel's own text. The `full` line carries a
/// value no other line has, so a reader that mistakenly took it is caught by
/// value: the kernel defines CPU `full` as always zero and the contract omits
/// it.
const CPU_PRESSURE: &str = "some avg10=0.00 avg60=0.18 avg300=0.48 total=3735977\n\
                            full avg10=9.99 avg60=9.99 avg300=9.99 total=999\n";

/// A `/proc/pressure/memory` under real reclaim pressure.
const MEMORY_PRESSURE: &str = "some avg10=0.11 avg60=1.23 avg300=0.45 total=12345678\n\
                               full avg10=0.05 avg60=0.67 avg300=0.12 total=7654321\n";

/// A `/proc/pressure/io` from a device that is stalling readers.
const IO_PRESSURE: &str = "some avg10=0.46 avg60=5.31 avg300=2.48 total=10161629\n\
                           full avg10=0.46 avg60=5.29 avg300=2.45 total=9929348\n";

/// Write `contents` to `rel` under `root`, creating the intermediate directories.
fn put(root: &Path, rel: &str, contents: &str) {
    let path = root.join(rel);
    fs::create_dir_all(path.parent().expect("a fixture file has a parent"))
        .expect("create the fixture directory");
    fs::write(&path, contents).expect("write the fixture file");
}

/// A fixture root shaped like a PSI-capable host outside any container: a root
/// cgroup and the three `/proc/pressure` files.
fn host_root() -> TempDir {
    let dir = tempfile::tempdir().expect("a temp fixture root");
    put(dir.path(), "proc/self/cgroup", "0::/\n");
    put(dir.path(), "proc/pressure/cpu", CPU_PRESSURE);
    put(dir.path(), "proc/pressure/memory", MEMORY_PRESSURE);
    put(dir.path(), "proc/pressure/io", IO_PRESSURE);
    dir
}

/// The three host pressure paths under a fixture root.
fn host_paths(root: &Path) -> [PathBuf; 3] {
    [
        root.join("proc/pressure/cpu"),
        root.join("proc/pressure/memory"),
        root.join("proc/pressure/io"),
    ]
}

/// B5: each of the five vitals is the `avg60` field of its own line — the
/// kernel has already averaged over exactly the 60 s the vitals cadence
/// publishes, so the reading needs no further reduction.
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

/// The contract carries no `stall.cpu.full`: the kernel defines it as always
/// zero, so publishing it would spend a central series on a constant. The
/// fixture's CPU `full` line reads 9.99, a value no vital may take.
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

/// B11: a host whose kernel publishes no pressure information reports
/// `Unsupported` and no readings at all. A zero here would read as "never
/// stalled" — a claim about a measurement the host cannot make.
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

/// An empty root has neither the pressure files nor `/proc/self/cgroup`; the
/// missing cgroup file must not be mistaken for a container, and the absent
/// pressure files must not panic the reader.
#[test]
fn a_root_with_no_proc_at_all_is_unsupported() {
    let root = tempfile::tempdir().expect("a temp fixture root");

    let reader = PressureReader::for_root(root.path());

    assert_eq!(reader.support(), PressureSupport::Unsupported);
    assert_eq!(reader.read().cpu_some, None);
}

/// I5: every malformed shape a pressure file can take yields no reading rather
/// than a panic or a half-parsed number — and it costs only its own vital, so a
/// truncated CPU file never silences memory and I/O.
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

/// A stall vital is a percentage of time. A value outside `[0, 100]` is not one,
/// so it is no reading at all — clamping it would publish a number the kernel
/// never measured, and 0 in particular is the "never stalled" answer this
/// reader must never invent.
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

/// The ends of the range are real readings: a host that never stalled reads 0
/// and a host that stalled throughout reads 100.
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

/// Some kernels publish only the `some` line for a resource. That is a present
/// `some` vital and an absent `full` one, not a failed read of the file.
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

/// One unreadable file costs only the vitals it carries. A kernel exposing CPU
/// and memory pressure but not I/O still reports four of the five.
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

/// E26: an agent inside a container measures its **own** pressure. The chosen
/// paths are asserted, not just the values — a containerized agent reporting the
/// host's stall figures as its own is the silent-wrong-answer class this
/// program exists to remove, and identical values would hide it.
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

/// An agent at the root cgroup is not containerized: it reads the host's
/// pressure files, which are its own.
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

/// A container whose cgroup publishes no pressure files reports `Unsupported`.
/// Falling back to `/proc/pressure` would attribute the host's stalls — every
/// neighbouring container's included — to this agent.
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

/// A cgroup v1 hierarchy has no unified `0::` line and no per-cgroup pressure
/// files. Such a host is not a container for this purpose and reads the host's
/// pressure.
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

/// A kernel that publishes no `/proc/self/cgroup` at all is not a container
/// either — an unreadable cgroup file must not silence the host's vitals.
#[test]
fn a_host_without_a_cgroup_file_reads_the_host() {
    let root = host_root();
    fs::remove_file(root.path().join("proc/self/cgroup")).expect("remove the cgroup fixture");

    let reader = PressureReader::for_root(root.path());

    assert_eq!(reader.support(), PressureSupport::Supported);
    assert_eq!(reader.read().cpu_some, Some(0.18));
}
