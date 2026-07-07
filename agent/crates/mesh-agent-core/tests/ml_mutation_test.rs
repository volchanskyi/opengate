//! Mutation-hardening tests for the Edge-Sentinel k-means primitives. These pin
//! the accessor return values and the training math with absolute assertions
//! (not model-vs-model comparisons, which a constant-return mutation satisfies).

use mesh_agent_core::ml::{
    ensemble::EdgeMlEnsemble,
    kmeans::KMeansModel,
    redact::{cmdline_hash, redact_cmdline},
    window::AnomalyRateWindow,
};

/// `centers()` and `threshold()` must return the trained values, not a constant.
/// Two clearly separated clusters put one center near 0 and the other near 10,
/// and leave a small positive within-cluster threshold.
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

/// A point sitting on a cluster centroid is normal; a point far from both is an
/// anomaly. This exercises `nearest_center`/`nearest_distance` at both centroids,
/// so a degenerate "always cluster N" classifier misplaces one of them.
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
    // Both cluster centroids are within their clusters → not anomalous.
    assert!(
        !model.is_anomaly(&[0.1, 0.1]),
        "low centroid must be normal"
    );
    assert!(
        !model.is_anomaly(&[20.1, 20.1]),
        "high centroid must be normal"
    );
    // A point equidistant-but-far from both clusters is anomalous.
    assert!(
        model.is_anomaly(&[10.0, 10.0]),
        "midpoint gap must be anomalous"
    );
    assert!(
        model.is_anomaly(&[50.0, 50.0]),
        "far point must be anomalous"
    );
}

/// `model_count()` must return the number of member models, not a constant.
#[test]
fn ensemble_model_count_matches_member_count() {
    let m1 = KMeansModel::<2>::train(&[[0.0, 0.0], [1.0, 1.0]], 5).unwrap();
    let m2 = KMeansModel::<2>::train(&[[0.0, 0.0], [2.0, 2.0]], 5).unwrap();
    let m3 = KMeansModel::<2>::train(&[[0.0, 0.0], [3.0, 3.0]], 5).unwrap();
    let ensemble = EdgeMlEnsemble::from_models(vec![m1, m2, m3]).unwrap();
    assert_eq!(ensemble.model_count(), 3);
}

/// `cmdline_hash` is a real SHA-256 hex digest, not a constant. The empty-input
/// digest is well known, and distinct inputs must hash differently.
#[test]
fn cmdline_hash_is_a_real_sha256() {
    assert_eq!(
        cmdline_hash(""),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    assert_ne!(cmdline_hash("a"), cmdline_hash("b"));
    assert_eq!(cmdline_hash("a").len(), 64);
}

/// `redact_cmdline` redacts assignments, AWS keys, and credential URLs while
/// leaving ordinary tokens untouched. The absolute equalities pin the branch
/// conditions (a widened AWS/URL match would redact a benign token; a narrowed
/// assignment redaction would leak the value).
#[test]
fn redact_cmdline_pins_branch_boundaries() {
    // Ordinary tokens pass through verbatim.
    assert_eq!(redact_cmdline("hello world"), "hello world");

    // Secret assignment → the whole token becomes [REDACTED], not empty.
    assert_eq!(redact_cmdline("x=y password=secret"), "x=y [REDACTED]");

    // A short AKIA-looking token (< 20 chars) is NOT an AWS key.
    assert_eq!(redact_cmdline("AKIA1234"), "AKIA1234");
    // A 20-char all-uppercase token that is not an AKIA/ASIA key is untouched.
    assert_eq!(
        redact_cmdline("ABCDEFGHIJ1234567890"),
        "ABCDEFGHIJ1234567890"
    );
    // A real 20-char AWS access key is redacted.
    assert_eq!(redact_cmdline("AKIAIOSFODNN7EXAMPLE"), "[REDACTED]");

    // A URL needs BOTH a scheme and credentials to be redacted.
    assert_eq!(
        redact_cmdline("http://example.com/x"),
        "http://example.com/x"
    );
    assert_eq!(redact_cmdline("postgres://u:p@db/app"), "[REDACTED_URL]");
}

/// `AnomalyRateWindow` reports emptiness and guards an out-of-range bit index
/// (a mutated guard would shift-overflow on `rate(64)`).
#[test]
fn anomaly_window_is_empty_and_guards_bit_index() {
    let mut window = AnomalyRateWindow::new(4).unwrap();
    assert!(window.is_empty(), "fresh window is empty");
    assert_eq!(window.len(), 0);

    window.push(1, 0b1);
    assert!(!window.is_empty(), "after push, not empty");
    assert_eq!(window.rate(0), 1.0);

    // Out-of-range bit indices return 0.0 without overflowing the shift.
    assert_eq!(window.rate(64), 0.0);
    assert_eq!(window.rate(200), 0.0);
}
