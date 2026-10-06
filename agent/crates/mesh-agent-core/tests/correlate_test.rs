//! Edge correlation: ranking equivalence against the frozen reference, degenerate windows and
//! the bounds on a ranking run.

use std::path::PathBuf;
use std::time::Duration;

use edge_tsdb::{Durability, LocalTsdb, Sample, TsdbConfig};
use mesh_agent_core::correlate::{
    anomaly_rate, correlate_snapshot, ks_statistic, rank_dimensions, shift_magnitude,
    CorrelationLimits, CorrelationWindow, DimWindows,
};
use mesh_agent_core::ml::store_sink::{SERIES_CPU, SERIES_DISK_AWAIT_MS, SERIES_MEM};
use serde::Deserialize;

/// The ranking scores match the frozen reference within this absolute difference.
const TOLERANCE: f64 = 1e-12;

#[derive(Deserialize)]
struct Fixture {
    baseline_start: i64,
    baseline_end: i64,
    focus_start: i64,
    focus_end: i64,
    top_n: usize,
    dims: Vec<FixtureDim>,
    expected: Vec<FixtureRanked>,
}

#[derive(Deserialize)]
struct FixtureDim {
    dim: String,
    /// `[timestamp_secs, value]` pairs, ascending.
    points: Vec<[f64; 2]>,
}

#[derive(Deserialize)]
struct FixtureRanked {
    dim: String,
    score: f64,
    ks_statistic: f64,
    anomaly_rate: f64,
    shift_magnitude: f64,
    baseline_samples: usize,
    focus_samples: usize,
}

fn fixture() -> Fixture {
    let path: PathBuf = [
        env!("CARGO_MANIFEST_DIR"),
        "tests",
        "fixtures",
        "correlate_reference.json",
    ]
    .iter()
    .collect();
    let blob = std::fs::read_to_string(&path).expect("reference fixture is committed");
    serde_json::from_str(&blob).expect("reference fixture parses")
}

impl Fixture {
    fn window(&self) -> CorrelationWindow {
        CorrelationWindow::new(
            self.baseline_start,
            self.baseline_end,
            self.focus_start,
            self.focus_end,
        )
        .expect("the fixture window is well formed")
    }

    fn split(&self, limits: &CorrelationLimits) -> Vec<(String, Vec<f64>, Vec<f64>)> {
        let window = self.window();
        self.dims
            .iter()
            .map(|d| {
                let points: Vec<(i64, f64)> = d
                    .points
                    .iter()
                    .map(|p| (p[0] as i64, p[1]))
                    .collect::<Vec<_>>();
                let (baseline, focus) = window.split(&points, limits.max_points_per_window);
                (d.dim.clone(), baseline, focus)
            })
            .collect()
    }
}

fn dim_windows(split: &[(String, Vec<f64>, Vec<f64>)]) -> Vec<DimWindows<'_>> {
    split
        .iter()
        .map(|(dim, baseline, focus)| DimWindows {
            dim,
            baseline,
            focus,
        })
        .collect()
}

fn close(actual: f64, expected: f64, what: &str) {
    assert!(
        (actual - expected).abs() <= TOLERANCE,
        "{what}: {actual} is not within {TOLERANCE} of {expected}"
    );
}

#[test]
fn ranking_matches_the_frozen_reference_dimension_for_dimension() {
    let fixture = fixture();
    let limits = CorrelationLimits {
        top_n: fixture.top_n,
        ..CorrelationLimits::default()
    };
    let split = fixture.split(&limits);
    let ranking = rank_dimensions(&dim_windows(&split), &limits);

    let names: Vec<&str> = ranking.ranked.iter().map(|r| r.dim.as_str()).collect();
    let expected: Vec<&str> = fixture.expected.iter().map(|r| r.dim.as_str()).collect();
    assert_eq!(names, expected, "ranking order");

    for (got, want) in ranking.ranked.iter().zip(&fixture.expected) {
        close(got.score, want.score, &format!("{} score", want.dim));
        close(
            got.ks_statistic,
            want.ks_statistic,
            &format!("{} ks", want.dim),
        );
        close(
            got.anomaly_rate,
            want.anomaly_rate,
            &format!("{} anomaly rate", want.dim),
        );
        close(
            got.shift_magnitude,
            want.shift_magnitude,
            &format!("{} shift magnitude", want.dim),
        );
        assert_eq!(got.baseline_samples, want.baseline_samples);
        assert_eq!(got.focus_samples, want.focus_samples);
    }
}

#[test]
fn the_broken_pattern_dimension_ranks_first() {
    let fixture = fixture();
    let limits = CorrelationLimits::default();
    let split = fixture.split(&limits);
    let ranking = rank_dimensions(&dim_windows(&split), &limits);

    assert_eq!(ranking.ranked[0].dim, "disk.await_ms");
    assert!(
        ranking.ranked[0].score > ranking.ranked[1].score,
        "the broken dimension outranks the runner-up outright"
    );
}

#[test]
fn a_dimension_without_enough_readings_is_not_ranked() {
    let fixture = fixture();
    let limits = CorrelationLimits::default();
    let split = fixture.split(&limits);
    let ranking = rank_dimensions(&dim_windows(&split), &limits);

    assert!(fixture.dims.iter().any(|d| d.dim == "disk.mounts_critical"));
    assert!(
        !ranking
            .ranked
            .iter()
            .any(|r| r.dim == "disk.mounts_critical"),
        "a dimension with one focus reading is not ranked"
    );
    assert_eq!(ranking.dims_considered, fixture.dims.len());
}

#[test]
fn identical_samples_shift_by_nothing_and_disjoint_ones_shift_completely() {
    let same = [1.0, 2.0, 3.0, 4.0];
    assert_eq!(ks_statistic(&same, &same), 0.0);
    assert_eq!(ks_statistic(&[1.0, 2.0], &[9.0, 10.0]), 1.0);
    assert_eq!(
        ks_statistic(&[1.0, 2.0, 5.0], &[3.0, 4.0]),
        ks_statistic(&[3.0, 4.0], &[1.0, 2.0, 5.0])
    );
}

#[test]
fn degenerate_windows_never_produce_a_nan() {
    let empty: [f64; 0] = [];
    for (baseline, focus) in [
        (&empty[..], &empty[..]),
        (&[1.0][..], &empty[..]),
        (&empty[..], &[1.0][..]),
        (&[1.0][..], &[1.0][..]),
        (&[0.0, 0.0][..], &[0.0, 0.0][..]),
        (&[2.0, 2.0][..], &[7.0, 7.0][..]),
    ] {
        for value in [
            ks_statistic(baseline, focus),
            anomaly_rate(baseline, focus),
            shift_magnitude(baseline, focus),
        ] {
            assert!(value.is_finite(), "{baseline:?}/{focus:?} produced {value}");
            assert!((0.0..=1.0).contains(&value));
        }
    }

    assert_eq!(ks_statistic(&[5.0], &[5.0]), 0.0);
    assert_eq!(ks_statistic(&[5.0], &[6.0]), 1.0);
    assert_eq!(anomaly_rate(&[3.0, 3.0, 3.0], &[4.0, 4.0]), 1.0);
    assert_eq!(anomaly_rate(&[3.0, 3.0, 3.0], &[3.0, 3.0]), 0.0);
    assert_eq!(shift_magnitude(&[0.0, 0.0], &[0.0, 0.0]), 0.0);
    assert_eq!(shift_magnitude(&[0.0, 0.0], &[0.1, 0.1]), 1.0);
}

#[test]
fn a_non_finite_reading_is_dropped_rather_than_ranked() {
    let window = CorrelationWindow::new(0, 10, 10, 20).expect("window");
    let points = [
        (0, 1.0),
        (2, f64::NAN),
        (4, 1.0),
        (6, f64::INFINITY),
        (12, 9.0),
        (14, f64::NAN),
        (16, 9.0),
    ];
    let (baseline, focus) = window.split(&points, 100);
    assert_eq!(baseline, vec![1.0, 1.0]);
    assert_eq!(focus, vec![9.0, 9.0]);

    let dims = [DimWindows {
        dim: "cpu.total",
        baseline: &baseline,
        focus: &focus,
    }];
    let ranking = rank_dimensions(&dims, &CorrelationLimits::default());
    assert!(ranking.ranked[0].score.is_finite());
}

#[test]
fn the_windows_meet_without_overlapping() {
    let window = CorrelationWindow::new(100, 200, 200, 300).expect("window");
    let points = [
        (99, 1.0),
        (100, 2.0),
        (199, 3.0),
        (200, 4.0),
        (300, 5.0),
        (301, 6.0),
    ];
    let (baseline, focus) = window.split(&points, 100);
    assert_eq!(baseline, vec![2.0, 3.0]);
    assert_eq!(focus, vec![4.0, 5.0]);
}

#[test]
fn a_defaulted_baseline_is_the_window_before_the_focus() {
    let window = CorrelationWindow::preceding_baseline(1_000, 1_300).expect("window");
    assert_eq!(window.baseline_start(), 700);
    assert_eq!(window.baseline_end(), 1_000);
    assert_eq!(window.focus_start(), 1_000);
    assert_eq!(window.focus_end(), 1_300);
}

#[test]
fn a_window_that_runs_backwards_is_refused() {
    assert!(CorrelationWindow::new(0, 10, 20, 20).is_none());
    assert!(CorrelationWindow::new(0, 10, 30, 20).is_none());
    assert!(CorrelationWindow::new(10, 10, 20, 30).is_none());
    assert!(CorrelationWindow::preceding_baseline(100, 100).is_none());
}

#[test]
fn ranking_is_bounded_in_dimensions_points_and_time() {
    let fixture = fixture();

    let capped = CorrelationLimits {
        max_dims: 3,
        ..CorrelationLimits::default()
    };
    let split = fixture.split(&capped);
    let ranking = rank_dimensions(&dim_windows(&split), &capped);
    assert_eq!(ranking.dims_considered, 3);
    assert!(ranking.dims_truncated);
    assert!(ranking.ranked.len() <= 3);

    let few_points = CorrelationLimits {
        max_points_per_window: 5,
        ..CorrelationLimits::default()
    };
    let split = fixture.split(&few_points);
    let ranking = rank_dimensions(&dim_windows(&split), &few_points);
    for r in &ranking.ranked {
        assert!(r.baseline_samples <= 5 && r.focus_samples <= 5);
    }

    let topped = CorrelationLimits {
        top_n: 2,
        ..CorrelationLimits::default()
    };
    let split = fixture.split(&topped);
    let ranking = rank_dimensions(&dim_windows(&split), &topped);
    assert_eq!(ranking.ranked.len(), 2);
    assert_eq!(ranking.ranked[0].dim, "disk.await_ms");

    let no_time = CorrelationLimits {
        budget: Duration::ZERO,
        ..CorrelationLimits::default()
    };
    let split = fixture.split(&no_time);
    let ranking = rank_dimensions(&dim_windows(&split), &no_time);
    assert!(ranking.budget_exhausted);
    assert_eq!(ranking.dims_considered, 1);
    assert!(!ranking.ranked.is_empty());

    let ranking = rank_dimensions(&dim_windows(&split), &CorrelationLimits::default());
    assert!(!ranking.budget_exhausted);
}

#[test]
fn the_default_limits_leave_the_budget_unspent() {
    let limits = CorrelationLimits::default();
    assert!(limits.budget >= Duration::from_millis(50));
    assert!(limits.max_dims >= 13, "every host dimension fits");
    assert!(limits.max_points_per_window >= 900, "15 minutes at 1 Hz");
}

#[test]
fn correlating_the_local_store_ranks_the_dimension_that_broke() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).expect("open");
    for i in 0..600i64 {
        let ts = 1_700_000_000 + i;
        store
            .append(
                SERIES_CPU,
                Sample::new(ts, 20.0 + f64::from(i as i32 % 3)),
                false,
            )
            .expect("append cpu");
        store
            .append(SERIES_MEM, Sample::new(ts, 60.0), false)
            .expect("append mem");
        let await_ms = if i < 480 { 0.4 } else { 40.0 };
        store
            .append(SERIES_DISK_AWAIT_MS, Sample::new(ts, await_ms), false)
            .expect("append await");
    }
    store.commit(Durability::Full).expect("commit");

    let window =
        CorrelationWindow::preceding_baseline(1_700_000_480, 1_700_000_599).expect("window");
    let snapshot = store.snapshot().expect("snapshot");
    let ranking =
        correlate_snapshot(&snapshot, &window, &CorrelationLimits::default()).expect("correlate");

    assert_eq!(ranking.ranked[0].dim, "disk.await_ms");
    assert!(ranking.ranked[0].score > 0.9);
    let mem = ranking
        .ranked
        .iter()
        .find(|r| r.dim == "mem.used_percent")
        .expect("a flat dimension is still ranked");
    assert_eq!(mem.score, 0.0);
    assert_eq!(ranking.ranked.len(), 3);
}

#[test]
fn a_snapshot_read_is_unaffected_by_concurrent_sampling() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut store = LocalTsdb::open(dir.path(), TsdbConfig::default()).expect("open");
    for i in 0..120i64 {
        let value = if i < 60 { 1.0 } else { 50.0 };
        store
            .append(SERIES_CPU, Sample::new(1_700_000_000 + i, value), false)
            .expect("append");
    }
    store.commit(Durability::Full).expect("commit");

    let snapshot = store.snapshot().expect("snapshot");
    for i in 120..240i64 {
        store
            .append(SERIES_CPU, Sample::new(1_700_000_000 + i, 1.0), false)
            .expect("append");
    }
    store.commit(Durability::Full).expect("commit");

    let window = CorrelationWindow::new(1_700_000_000, 1_700_000_060, 1_700_000_060, 1_700_000_119)
        .expect("window");
    let ranking =
        correlate_snapshot(&snapshot, &window, &CorrelationLimits::default()).expect("correlate");
    assert_eq!(ranking.ranked.len(), 1);
    assert_eq!(ranking.ranked[0].dim, "cpu.total");
    assert_eq!(ranking.ranked[0].baseline_samples, 60);
    assert_eq!(ranking.ranked[0].focus_samples, 60);
    assert_eq!(ranking.ranked[0].ks_statistic, 1.0);
}

#[test]
fn an_empty_store_ranks_nothing() {
    let dir = tempfile::tempdir().expect("tempdir");
    let store = LocalTsdb::open(dir.path(), TsdbConfig::default()).expect("open");
    let window =
        CorrelationWindow::preceding_baseline(1_700_000_600, 1_700_000_900).expect("window");
    let snapshot = store.snapshot().expect("snapshot");
    let ranking =
        correlate_snapshot(&snapshot, &window, &CorrelationLimits::default()).expect("correlate");
    assert!(ranking.ranked.is_empty());
    assert!(!ranking.dims_truncated);
    assert!(!ranking.budget_exhausted);
}
