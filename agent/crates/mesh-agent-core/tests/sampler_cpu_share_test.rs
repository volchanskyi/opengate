//! The sampler's processor share read off a live process, alone in its own test binary so no
//! other test spends processor time inside it.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use mesh_agent_core::ml::sampler::{MetricSampler, SysinfoSampler};
use sysinfo::{Pid, ProcessesToUpdate, System};

/// This process's accumulated processor time, in milliseconds.
fn own_cpu_ms(system: &mut System, pid: Pid) -> u64 {
    system.refresh_processes(ProcessesToUpdate::Some(&[pid]), false);
    system
        .process(pid)
        .expect("the test process is visible to itself")
        .accumulated_cpu_time()
}

#[test]
fn a_thread_spinning_one_core_reads_as_one_core_of_the_host() {
    let pid = Pid::from_u32(std::process::id());
    let mut own = System::new_all();
    let cores = own.cpus().len();
    assert!(cores > 0, "the host reports its processors");

    let stop = Arc::new(AtomicBool::new(false));
    let spinning = stop.clone();
    let spinner = std::thread::spawn(move || {
        while !spinning.load(Ordering::Relaxed) {
            std::hint::spin_loop();
        }
    });

    let mut sampler = SysinfoSampler::new(u8::MAX as usize).expect("a rank fits in a byte");
    let started = Instant::now();
    let cpu_before = own_cpu_ms(&mut own, pid);
    let first = sampler.sample().expect("first sample");
    // The agent sleeps a second between samples.
    std::thread::sleep(Duration::from_secs(1));
    let second = sampler.sample().expect("second sample");
    let cpu_after = own_cpu_ms(&mut own, pid);
    let wall = started.elapsed();
    stop.store(true, Ordering::Relaxed);
    spinner.join().expect("spinner stops");

    assert!(
        first.processes.iter().all(|p| p.cpu_share.is_none()),
        "a process seen once has no share"
    );
    for process in &second.processes {
        if let Some(share) = process.cpu_share {
            assert!(
                (0.0..=100.0).contains(&share),
                "{} reads {share}, outside the whole host",
                process.basename
            );
        }
    }

    let spent_ms = cpu_after - cpu_before;
    assert!(spent_ms >= 200, "the spinner ran: {spent_ms} ms");
    let measured = spent_ms as f64 / (wall.as_secs_f64() * 1_000.0 * cores as f64) * 100.0;
    let row = second
        .processes
        .iter()
        .find(|p| p.pid == pid.as_u32())
        .expect("a process spinning a core ranks among the busiest");
    let share = row.cpu_share.expect("a process seen twice has a share");
    assert!(
        (share - measured).abs() <= measured * 0.35,
        "the sampler reads {share:.2}, the process spent {measured:.2} ({cores} cores)"
    );
}
