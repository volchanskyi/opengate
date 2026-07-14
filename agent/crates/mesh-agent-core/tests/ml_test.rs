use mesh_agent_core::ml::{
    ensemble::EdgeMlEnsemble,
    kmeans::KMeansModel,
    redact::{cmdline_hash, redact_cmdline, redact_log_line},
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
fn cmdline_hash_is_a_stable_sha256_digest() {
    assert_eq!(
        cmdline_hash("mesh-agent --version"),
        "bae19e4c4979db1cebc3b35b8a6d4a490ea82668b368d95da392a9c4ef2874f0"
    );
    assert_ne!(
        cmdline_hash("mesh-agent --version"),
        cmdline_hash("mesh-agent --version --verbose")
    );
}

#[test]
fn redact_cmdline_covers_common_secret_shapes() {
    let input = "app --password=hunter2 --token abc --api-key xyz Bearer secret AKIAIOSFODNN7EXAMPLE postgres://u:p@db/app";
    let redacted = redact_cmdline(input);

    assert!(!redacted.contains("hunter2"));
    assert!(!redacted.contains("abc"));
    assert!(!redacted.contains("xyz"));
    assert!(!redacted.contains("secret"));
    assert!(!redacted.contains("AKIAIOSFODNN7EXAMPLE"));
    assert!(!redacted.contains("u:p@db"));
    assert!(redacted.contains("[REDACTED]"));
}

#[test]
fn redact_cmdline_only_replaces_credential_bearing_urls() {
    let benign = "curl https://example.com/health user@example.com";
    assert_eq!(redact_cmdline(benign), benign);
    assert_eq!(
        redact_cmdline("postgres://user:password@db.internal/opengate"),
        "[REDACTED_URL]"
    );
}

#[test]
fn redact_cmdline_preserves_the_replacement_for_inline_assignments() {
    assert_eq!(redact_cmdline("app --password=hunter2"), "app [REDACTED]");
}

/// Builds the raw-log redaction corpus: each row is a secret-bearing log line
/// whose `secret` substring must never survive redaction. This corpus is
/// mirrored, case for case, by the server-side guard's `TestRedactSecrets` in
/// `server/internal/api/log_redact_test.go` — the two guards are independent
/// defense-in-depth layers, so both must strip every shape. Keep them in sync.
///
/// The connection-string case is assembled from parts rather than written as a
/// literal DSN so the fixture is not itself a hardcoded-credential hotspot.
fn raw_log_secret_corpus() -> Vec<(String, String)> {
    let dsn_pw = "s3cr3tpw";
    let dsn = format!("dsn postgres://appuser:{dsn_pw}@db.internal:5432/app opened");
    vec![
        // Bearer / basic auth headers.
        (
            "level=info msg=\"request\" auth=\"Bearer abcDEF012345_tok\"".into(),
            "abcDEF012345_tok".into(),
        ),
        (
            "proxy authorization: Basic dXNlcjpwYXNzd29yZA==".into(),
            "dXNlcjpwYXNzd29yZA==".into(),
        ),
        // key=value and key: value assignments.
        (
            "connecting with password=hunter2secret to db".into(),
            "hunter2secret".into(),
        ),
        (
            "api_key: sk-live-XYZ0123456789 accepted".into(),
            "sk-live-XYZ0123456789".into(),
        ),
        (
            "client_secret=ghp_00112233445566778899 rotated".into(),
            "ghp_00112233445566778899".into(),
        ),
        // JWT session token (three base64url segments).
        (
            "session started token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N"
                .into(),
            "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N".into(),
        ),
        // Cloud keys: AWS access-key id and GCP API key.
        (
            "aws creds AKIAIOSFODNN7EXAMPLE loaded".into(),
            "AKIAIOSFODNN7EXAMPLE".into(),
        ),
        (
            "google key AIzaSyA1234567890abcdefghijklmnopqrstuvw in env".into(),
            "AIzaSyA1234567890abcdefghijklmnopqrstuvw".into(),
        ),
        // Connection string with embedded credentials.
        (dsn, dsn_pw.into()),
    ]
}

#[test]
fn redact_log_line_strips_every_secret_shape() {
    for (line, secret) in raw_log_secret_corpus() {
        let redacted = redact_log_line(&line);
        assert!(
            !redacted.contains(&secret),
            "secret {secret:?} survived redaction of {line:?} → {redacted:?}"
        );
        assert!(
            redacted.contains("REDACTED"),
            "redacted line must carry a placeholder: {redacted:?}"
        );
    }
}

#[test]
fn redact_log_line_leaves_benign_lines_intact() {
    // A line with no secret material is returned unchanged, including a plain URL
    // that carries no credentials.
    for benign in [
        "user alice logged in from 10.0.0.1",
        "GET https://example.com/health 200 in 4ms",
        "disk usage 42% on /var",
    ] {
        assert_eq!(
            redact_log_line(benign),
            benign,
            "benign line must be untouched"
        );
    }
}

#[test]
fn redact_log_line_recognizes_exact_jwt_shape() {
    for token in ["eyJabc.def.ghi", "\"eyJabc.def.ghi,", "eyJ-_.-_.-_"] {
        assert_eq!(
            redact_log_line(token),
            "[REDACTED]",
            "valid JWT-shaped token was not redacted: {token:?}"
        );
    }

    for token in [
        "abc.def.ghi",
        "eyJabc.def",
        "eyJabc.def.ghi.jkl",
        "eyJabc..ghi",
        ".eyJabc.def.ghi",
        "eyJabc.def.",
    ] {
        assert_eq!(
            redact_log_line(token),
            token,
            "invalid JWT-shaped token was over-redacted: {token:?}"
        );
    }
}

#[test]
fn redact_log_line_recognizes_gcp_key_boundaries_and_alphabet() {
    let min_key = format!("AIza{}", "a".repeat(31));
    let max_key = format!("AIza{}", "Z".repeat(41));
    let url_safe_key = format!("AIza{}_-", "9".repeat(29));
    for token in [&min_key, &max_key, &url_safe_key] {
        assert_eq!(
            redact_log_line(token),
            "[REDACTED]",
            "valid GCP API key was not redacted: {token:?}"
        );
    }

    let too_short = format!("AIza{}", "a".repeat(30));
    let too_long = format!("AIza{}", "a".repeat(42));
    let wrong_prefix = format!("AIzb{}", "a".repeat(31));
    let invalid_char = format!("AIza{}!", "a".repeat(30));
    for token in [&too_short, &too_long, &wrong_prefix, &invalid_char] {
        assert_eq!(
            redact_log_line(token),
            token.as_str(),
            "invalid GCP API key was over-redacted: {token:?}"
        );
    }
}
