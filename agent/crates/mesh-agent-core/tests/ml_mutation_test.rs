//! Mutation-hardening tests for the k-means primitives, pinned with absolute assertions.

use mesh_agent_core::ml::{
    ensemble::EdgeMlEnsemble,
    kmeans::KMeansModel,
    redact::{cmdline_hash, redact_cmdline},
    window::AnomalyRateWindow,
};

#[test]
fn kmeans_centers_and_threshold_reflect_trained_clusters() {
    let samples = [[0.0, 0.0], [0.1, 0.1], [10.0, 10.0], [10.1, 10.1]];
    let model = KMeansModel::<2>::train(&samples, 25).unwrap();

    let centers = model.centers();
    let mut xs = [centers[0][0], centers[1][0]];
    xs.sort_by(f32::total_cmp);
    assert!(xs[0] < 1.0, "low cluster center x = {}", xs[0]);
    assert!(xs[1] > 9.0, "high cluster center x = {}", xs[1]);

    let threshold = model.threshold();
    assert!(
        threshold > 0.0 && threshold < 1.0,
        "within-cluster threshold = {threshold}"
    );
}

#[test]
fn kmeans_classifies_each_centroid_as_normal() {
    let samples = [
        [0.0, 0.0],
        [0.2, 0.1],
        [0.1, 0.2],
        [20.0, 20.0],
        [20.2, 20.1],
        [20.1, 20.2],
    ];
    let model = KMeansModel::<2>::train(&samples, 50).unwrap();
    assert!(
        !model.is_anomaly(&[0.1, 0.1]),
        "low centroid must be normal"
    );
    assert!(
        !model.is_anomaly(&[20.1, 20.1]),
        "high centroid must be normal"
    );
    assert!(
        model.is_anomaly(&[10.0, 10.0]),
        "midpoint gap must be anomalous"
    );
    assert!(
        model.is_anomaly(&[50.0, 50.0]),
        "far point must be anomalous"
    );
}

#[test]
fn ensemble_model_count_matches_member_count() {
    let m1 = KMeansModel::<2>::train(&[[0.0, 0.0], [1.0, 1.0]], 5).unwrap();
    let m2 = KMeansModel::<2>::train(&[[0.0, 0.0], [2.0, 2.0]], 5).unwrap();
    let m3 = KMeansModel::<2>::train(&[[0.0, 0.0], [3.0, 3.0]], 5).unwrap();
    let ensemble = EdgeMlEnsemble::from_models(vec![m1, m2, m3]).unwrap();
    assert_eq!(ensemble.model_count(), 3);
}

#[test]
fn cmdline_hash_is_a_real_sha256() {
    assert_eq!(
        cmdline_hash(""),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    assert_ne!(cmdline_hash("a"), cmdline_hash("b"));
    assert_eq!(cmdline_hash("a").len(), 64);
}

#[test]
fn redact_cmdline_pins_branch_boundaries() {
    assert_eq!(redact_cmdline("hello world"), "hello world");

    assert_eq!(redact_cmdline("x=y password=secret"), "x=y [REDACTED]");

    assert_eq!(redact_cmdline("AKIA1234"), "AKIA1234");
    assert_eq!(
        redact_cmdline("ABCDEFGHIJ1234567890"),
        "ABCDEFGHIJ1234567890"
    );
    assert_eq!(redact_cmdline("AKIAIOSFODNN7EXAMPLE"), "[REDACTED]");

    assert_eq!(
        redact_cmdline("http://example.com/x"),
        "http://example.com/x"
    );
    assert_eq!(redact_cmdline("postgres://u:p@db/app"), "[REDACTED_URL]");
}

#[test]
fn anomaly_window_is_empty_and_guards_bit_index() {
    let mut window = AnomalyRateWindow::new(4).unwrap();
    assert!(window.is_empty(), "fresh window is empty");
    assert_eq!(window.len(), 0);

    window.push(1, 0b1);
    assert!(!window.is_empty(), "after push, not empty");
    assert_eq!(window.rate(0), 1.0);

    assert_eq!(window.rate(64), 0.0);
    assert_eq!(window.rate(200), 0.0);
}
