use criterion::{criterion_group, criterion_main, Criterion};
use mesh_agent_core::ml::{
    ensemble::EdgeMlEnsemble,
    sampler::{MetricSampler, SysinfoSampler},
    window::AnomalyRateWindow,
};
use std::hint::black_box;

fn bench_detection_vote_and_window(c: &mut Criterion) {
    let samples = [
        [0.0, 0.0, 0.0],
        [0.1, 0.2, 0.1],
        [9.8, 10.0, 9.9],
        [10.1, 9.9, 10.2],
        [10.2, 10.1, 9.8],
    ];
    let ensemble = EdgeMlEnsemble::<3>::train_staggered(&samples, 6, 20).unwrap();
    let mut window = AnomalyRateWindow::new(120).unwrap();
    let probe = [50.0, 50.0, 50.0];
    let mut timestamp = 0i64;

    c.bench_function("edge_sentinel_detection_vote_window", |b| {
        b.iter(|| {
            timestamp += 1;
            let bits = u64::from(black_box(&ensemble).is_anomaly(black_box(&probe)));
            window.push(timestamp, bits);
            black_box(window.rate(0))
        })
    });
}

fn bench_sysinfo_sampler_capture(c: &mut Criterion) {
    let mut sampler = SysinfoSampler::new(10).unwrap();
    c.bench_function("edge_sentinel_sysinfo_sample", |b| {
        b.iter(|| black_box(sampler.sample().unwrap()))
    });
}

#[cfg(target_os = "linux")]
fn bench_rss_probe(c: &mut Criterion) {
    let before = current_rss_kib();
    let samples = [
        [0.0, 0.0, 0.0],
        [0.1, 0.2, 0.1],
        [9.8, 10.0, 9.9],
        [10.1, 9.9, 10.2],
        [10.2, 10.1, 9.8],
    ];
    let ensemble = EdgeMlEnsemble::<3>::train_staggered(&samples, 6, 20).unwrap();
    let window = AnomalyRateWindow::new(120).unwrap();
    let after = current_rss_kib();
    println!(
        "edge_sentinel_rss_delta_kib={}",
        after.saturating_sub(before)
    );
    black_box((&ensemble, &window));

    c.bench_function("edge_sentinel_rss_probe", |b| {
        b.iter(|| black_box(current_rss_kib()))
    });
}

#[cfg(target_os = "linux")]
fn current_rss_kib() -> usize {
    let statm = std::fs::read_to_string("/proc/self/statm").expect("read /proc/self/statm");
    let resident_pages = statm
        .split_whitespace()
        .nth(1)
        .expect("resident page count")
        .parse::<usize>()
        .expect("parse resident page count");
    resident_pages * 4
}

#[cfg(target_os = "linux")]
criterion_group! {
    name = benches;
    config = Criterion::default().sample_size(10);
    targets = bench_detection_vote_and_window, bench_sysinfo_sampler_capture, bench_rss_probe
}

#[cfg(not(target_os = "linux"))]
criterion_group! {
    name = benches;
    config = Criterion::default().sample_size(10);
    targets = bench_detection_vote_and_window, bench_sysinfo_sampler_capture
}

criterion_main!(benches);
