use criterion::{criterion_group, criterion_main, Criterion};
use mesh_agent_core::alerts::{
    pack_evidence, pack_metric_evidence, DimSeries, EvidenceSource, LOG_SAMPLES, PROCESS_ROWS,
    RANKED_DIMS, SERIES_DIMS, SERIES_MAX_POINTS,
};
use mesh_agent_core::correlate::Ranked;
use mesh_agent_core::ml::{
    ensemble::EdgeMlEnsemble,
    redact::redact_log_line,
    sampler::{MetricSampler, SysinfoSampler},
    window::AnomalyRateWindow,
};
use mesh_protocol::{EvidenceProcess, HistoryPoint};
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

// The sampler waits between two CPU readings by design, so its cost is the CPU time it spends.
#[cfg(unix)]
fn bench_sysinfo_sampler_capture(c: &mut Criterion<cpu_time::ProcessCpuTime>) {
    let mut sampler = SysinfoSampler::new(10).unwrap();
    let mut group = c.benchmark_group("edge_sentinel_sampler");
    group
        .sampling_mode(criterion::SamplingMode::Flat)
        .warm_up_time(std::time::Duration::from_millis(100))
        .measurement_time(std::time::Duration::from_millis(500));
    group.bench_function("edge_sentinel_sysinfo_sample", |b| {
        b.iter(|| black_box(sampler.sample().unwrap()))
    });
    group.finish();
}

/// Criterion's measurement as the process's CPU time, in nanoseconds, across every thread.
#[cfg(unix)]
mod cpu_time {
    use criterion::measurement::{Measurement, ValueFormatter};
    use criterion::Throughput;

    /// Reads `CLOCK_PROCESS_CPUTIME_ID`, which a sleeping thread does not advance.
    pub struct ProcessCpuTime;

    fn now_ns() -> u64 {
        let mut ts = libc::timespec {
            tv_sec: 0,
            tv_nsec: 0,
        };
        // SAFETY: clock_gettime writes one timespec through a pointer to a live timespec.
        let rc = unsafe { libc::clock_gettime(libc::CLOCK_PROCESS_CPUTIME_ID, &mut ts) };
        assert_eq!(rc, 0, "the process CPU clock reads");
        let secs = u64::try_from(ts.tv_sec).unwrap_or(0);
        let nanos = u64::try_from(ts.tv_nsec).unwrap_or(0);
        secs * 1_000_000_000 + nanos
    }

    impl Measurement for ProcessCpuTime {
        type Intermediate = u64;
        type Value = u64;

        fn start(&self) -> u64 {
            now_ns()
        }

        fn end(&self, started: u64) -> u64 {
            now_ns().saturating_sub(started)
        }

        fn add(&self, a: &u64, b: &u64) -> u64 {
            a + b
        }

        fn zero(&self) -> u64 {
            0
        }

        fn to_f64(&self, value: &u64) -> f64 {
            *value as f64
        }

        fn formatter(&self) -> &dyn ValueFormatter {
            &CpuNanos
        }
    }

    /// Scales CPU nanoseconds to the unit a reader takes in at a glance.
    struct CpuNanos;

    impl ValueFormatter for CpuNanos {
        fn scale_values(&self, typical: f64, values: &mut [f64]) -> &'static str {
            let (factor, unit) = match typical {
                t if t >= 1e9 => (1e-9, "s CPU"),
                t if t >= 1e6 => (1e-6, "ms CPU"),
                t if t >= 1e3 => (1e-3, "µs CPU"),
                _ => (1.0, "ns CPU"),
            };
            values.iter_mut().for_each(|v| *v *= factor);
            unit
        }

        fn scale_throughputs(&self, _: f64, _: &Throughput, _: &mut [f64]) -> &'static str {
            "per CPU second"
        }

        fn scale_for_machines(&self, _: &mut [f64]) -> &'static str {
            "ns CPU"
        }
    }
}

/// Measures the per-line secret scrub over a secret-dense mix of log lines.
fn bench_log_line_redaction(c: &mut Criterion) {
    let lines = [
        "level=info msg=\"request\" auth=\"Bearer abcDEF012345_tok\"",
        "connecting with password=hunter2secret to db",
        "api_key: sk-live-XYZ0123456789 accepted",
        "session token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N",
        "aws creds AKIAIOSFODNN7EXAMPLE loaded ok in region us-east-1",
        "user alice logged in from 10.0.0.1 handled request in 4ms",
    ];

    c.bench_function("edge_sentinel_log_line_redaction", |b| {
        b.iter(|| {
            for line in black_box(&lines) {
                black_box(redact_log_line(line));
            }
        })
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

/// The second the benchmarked alert fires at.
const FIRE_TS: i64 = 1_763_000_000;

/// Measures assembling and packing a full alert: eight ranked dimensions (three with readings),
/// ten processes and twenty log lines.
fn bench_alert_evidence_at_fire_time(c: &mut Criterion) {
    let ranked: Vec<Ranked> = (0..RANKED_DIMS)
        .map(|i| Ranked {
            dim: format!("disk.await_ms.{i}"),
            score: 0.9 - (i as f64 / 100.0),
            ks_statistic: 0.8,
            anomaly_rate: 0.7,
            shift_magnitude: 3.2,
            baseline_samples: 600,
            focus_samples: 600,
        })
        .collect();

    // Each series carries its full allowance, one reading a second centred on the event.
    let readings: Vec<DimSeries> = ranked
        .iter()
        .take(SERIES_DIMS)
        .map(|r| DimSeries {
            dim: r.dim.clone(),
            points: (0..SERIES_MAX_POINTS)
                .map(|i| HistoryPoint {
                    ts: FIRE_TS - SERIES_MAX_POINTS as i64 / 2 + i as i64,
                    value: 40.0 + (i as f64 % 17.0),
                })
                .collect(),
        })
        .collect();

    let processes: Vec<EvidenceProcess> = (0..PROCESS_ROWS)
        .map(|i| EvidenceProcess {
            rank: i as u32 + 1,
            basename: "pg_dump".to_string(),
            cmdline_hash: None,
            pid: 4242 + i as u32,
            cpu_share: Some(37.5),
            mem: 2_147_483_648.0,
        })
        .collect();

    // Each line carries a bearer token, a URL credential and an AWS key.
    let log_lines: Vec<String> = (0..LOG_SAMPLES)
        .map(|i| {
            format!(
                "level=error msg=\"backup {i} failed\" auth=\"Bearer abcDEF012345_tok\" \
                 db=postgres://user:pw@host/db aws_key AKIAIOSFODNN7EXAMPLE"
            )
        })
        .collect();

    c.bench_function("alert_evidence_compose_and_pack", |b| {
        b.iter(|| {
            let packed = pack_evidence(black_box(&EvidenceSource {
                ranked: &ranked,
                readings: &readings,
                processes: &processes,
                log_lines: &log_lines,
                event_ts: FIRE_TS,
            }));
            black_box(packed.bytes.len())
        })
    });
}

/// Measures packing a finding raised over stored history, which carries one dimension's readings.
fn bench_finding_evidence_over_history(c: &mut Criterion) {
    let points: Vec<HistoryPoint> = (0..SERIES_MAX_POINTS)
        .map(|i| HistoryPoint {
            ts: i as i64,
            value: 90.0 + (i as f64 % 9.0),
        })
        .collect();

    c.bench_function("alert_finding_evidence_pack", |b| {
        b.iter(|| {
            let packed = pack_metric_evidence(
                black_box("disk.used_percent"),
                black_box(&points),
                SERIES_MAX_POINTS as i64 / 2,
            );
            black_box(packed.bytes.len())
        })
    });
}

#[cfg(target_os = "linux")]
criterion_group! {
    name = benches;
    config = Criterion::default().sample_size(10);
    targets = bench_detection_vote_and_window, bench_log_line_redaction,
        bench_alert_evidence_at_fire_time, bench_finding_evidence_over_history, bench_rss_probe
}

#[cfg(not(target_os = "linux"))]
criterion_group! {
    name = benches;
    config = Criterion::default().sample_size(10);
    targets = bench_detection_vote_and_window, bench_log_line_redaction,
        bench_alert_evidence_at_fire_time, bench_finding_evidence_over_history
}

#[cfg(unix)]
criterion_group! {
    name = sampler;
    config = Criterion::default().with_measurement(cpu_time::ProcessCpuTime).sample_size(10);
    targets = bench_sysinfo_sampler_capture
}

#[cfg(unix)]
criterion_main!(benches, sampler);

#[cfg(not(unix))]
criterion_main!(benches);
