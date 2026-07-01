use mesh_agent_core::ml::{
    ensemble::EdgeMlEnsemble,
    kmeans::KMeansModel,
    redact::{cmdline_hash, redact_cmdline},
    sampler::{FakeSampler, MetricSample, MetricSampler, ProcessSample},
    window::AnomalyRateWindow,
};

#[test]
fn kmeans_converges_deterministically_and_rejects_non_finite_samples() {
    let samples = [
        [0.0, 0.1],
        [0.2, 0.0],
        [9.8, 10.0],
        [10.0, 9.9],
        [10.1, 10.2],
    ];

    let first = KMeansModel::<2>::train(&samples, 25).unwrap();
    let second = KMeansModel::<2>::train(&samples, 25).unwrap();

    assert_eq!(first.centers(), second.centers());
    assert!(!first.is_anomaly(&[0.1, 0.1]));
    assert!(!first.is_anomaly(&[10.0, 10.0]));
    assert!(first.is_anomaly(&[40.0, 40.0]));

    let err = KMeansModel::<2>::train(&[[0.0, f32::NAN], [1.0, 1.0]], 10).unwrap_err();
    assert!(err.to_string().contains("finite"));
}

#[test]
fn ensemble_requires_consensus_and_uses_robust_boundary() {
    let mut training = Vec::new();
    for i in 0..100 {
        let drift = (i as f32) / 1000.0;
        training.push([1.0 + drift, 1.0 - drift]);
    }
    training.push([50.0, 50.0]);

    let ensemble = EdgeMlEnsemble::<2>::train_staggered(&training, 6, 20).unwrap();
    assert!(!ensemble.is_anomaly(&[1.02, 0.98]));
    assert!(ensemble.is_anomaly(&[12.0, 12.0]));

    let lenient = KMeansModel::<2>::train(&[[0.0, 0.0], [2.0, 2.0], [100.0, 100.0]], 10).unwrap();
    let strict = KMeansModel::<2>::train(&[[0.0, 0.0], [1.0, 1.0], [1.1, 1.1]], 10).unwrap();
    let split = EdgeMlEnsemble::from_models(vec![lenient, strict]).unwrap();
    assert!(
        !split.is_anomaly(&[2.0, 2.0]),
        "one dissenting model must veto consensus"
    );
}

#[test]
fn anomaly_window_rolls_and_reports_bit_rates() {
    let mut window = AnomalyRateWindow::new(4).unwrap();
    window.push(100, 0b001);
    window.push(101, 0b101);
    window.push(102, 0b000);
    window.push(103, 0b100);

    assert_eq!(window.len(), 4);
    assert_eq!(window.rate(0), 0.5);
    assert_eq!(window.rate(2), 0.5);

    window.push(104, 0b100);
    assert_eq!(window.len(), 4);
    assert_eq!(window.rate(0), 0.25);
    assert_eq!(window.rate(2), 0.75);
}

#[test]
fn fake_sampler_returns_deterministic_process_ranks_without_full_cmdline() {
    let sample = MetricSample {
        cpu_total_percent: 42.0,
        memory_used_percent: 25.0,
        disk_used_percent: 70.0,
        network_rx_bytes: 1000,
        network_tx_bytes: 2000,
        processes: vec![
            ProcessSample {
                rank: 1,
                basename: "postgres".to_string(),
                cmdline_hash: Some(cmdline_hash("postgres --password=secret")),
            },
            ProcessSample {
                rank: 2,
                basename: "mesh-agent".to_string(),
                cmdline_hash: None,
            },
        ],
    };
    let mut sampler = FakeSampler::new(vec![sample.clone()]);

    assert_eq!(sampler.sample().unwrap(), sample);
    assert!(sampler.sample().is_err());
}

#[test]
fn redact_cmdline_covers_common_secret_shapes() {
    let input = "app --password=hunter2 token=abc api_key=xyz Bearer secret AKIAIOSFODNN7EXAMPLE postgres://u:p@db/app";
    let redacted = redact_cmdline(input);

    assert!(!redacted.contains("hunter2"));
    assert!(!redacted.contains("abc"));
    assert!(!redacted.contains("xyz"));
    assert!(!redacted.contains("secret"));
    assert!(!redacted.contains("AKIAIOSFODNN7EXAMPLE"));
    assert!(!redacted.contains("u:p@db"));
    assert!(redacted.contains("[REDACTED]"));
}
