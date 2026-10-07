use mesh_protocol::{
    canonical_rule_metric, AlertComparator, AlertSeverity, ControlMessage, Frame, RuleCoverage,
    RuleCoverageState, RulePredicate, RuleTerm, ThresholdRule, MAX_RULE_TERMS,
    MAX_RULE_WINDOW_SECS, RULE_METRICS, RULE_METRIC_ALIASES,
};

fn rule(metric: &str) -> ThresholdRule {
    ThresholdRule {
        id: "r".to_string(),
        version: 1,
        severity: AlertSeverity::Warning,
        metric: metric.to_string(),
        comparator: AlertComparator::Gt,
        threshold: 90.0,
        clear: 80.0,
        sustain_secs: 0,
        predicate: RulePredicate::Instant,
        window_secs: 0,
        all: Vec::new(),
    }
}

fn round_trip(msg: &ControlMessage) -> ControlMessage {
    let encoded = Frame::Control(msg.clone()).encode().expect("encode frame");
    let (frame, _) = Frame::decode(&encoded).expect("decode frame");
    match frame {
        Frame::Control(got) => got,
        other => panic!("expected a control frame, got {other:?}"),
    }
}

#[test]
fn every_canonical_metric_resolves_to_itself() {
    for name in RULE_METRICS {
        assert_eq!(
            canonical_rule_metric(name),
            Some(name),
            "{name} is the canonical name and must resolve to itself"
        );
    }
}

#[test]
fn legacy_names_resolve_to_their_canonical_metric() {
    assert_eq!(canonical_rule_metric("mem.used"), Some("mem.used_percent"));
    assert_eq!(
        canonical_rule_metric("disk.used"),
        Some("disk.used_percent")
    );
}

#[test]
fn every_alias_targets_a_canonical_metric_and_is_not_one_itself() {
    for (alias, canonical) in RULE_METRIC_ALIASES {
        assert!(
            RULE_METRICS.contains(&canonical),
            "alias {alias} points at {canonical}, which is not in the vocabulary"
        );
        assert!(
            !RULE_METRICS.contains(&alias),
            "{alias} is both an alias and a canonical name"
        );
    }
}

#[test]
fn vocabulary_has_no_duplicates() {
    let mut seen = RULE_METRICS.to_vec();
    seen.sort_unstable();
    let before = seen.len();
    seen.dedup();
    assert_eq!(before, seen.len(), "duplicate name in RULE_METRICS");
}

#[test]
fn unknown_metric_resolves_to_nothing() {
    assert_eq!(canonical_rule_metric("not.a.metric"), None);
    assert_eq!(canonical_rule_metric(""), None);
    // A per-window maximum is a reduction, not a reading the evaluator holds.
    assert_eq!(canonical_rule_metric("cpu.total.max"), None);
}

#[test]
fn a_rule_at_the_grammar_bounds_survives_the_wire() {
    let term = RuleTerm {
        metric: "cpu.total".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 1.0,
        clear: 0.0,
        predicate: RulePredicate::Rate,
        window_secs: MAX_RULE_WINDOW_SECS,
    };
    let mut r = rule("cpu.total");
    r.predicate = RulePredicate::WindowMean;
    r.window_secs = MAX_RULE_WINDOW_SECS;
    r.all = vec![term; MAX_RULE_TERMS];

    let msg = ControlMessage::PushAlertRules {
        rules: vec![r],
        device_hourly_ceiling: 0,
    };
    assert_eq!(round_trip(&msg), msg);
}

#[test]
fn rule_with_every_grammar_field_round_trips() {
    let msg = ControlMessage::PushAlertRules {
        rules: vec![ThresholdRule {
            id: "disk-wearing-out".to_string(),
            version: 4,
            severity: AlertSeverity::Critical,
            metric: "disk.await_ms".to_string(),
            comparator: AlertComparator::Gte,
            threshold: 20.0,
            clear: 10.0,
            sustain_secs: 300,
            predicate: RulePredicate::WindowMean,
            window_secs: 600,
            all: vec![RuleTerm {
                metric: "disk.queue_depth".to_string(),
                comparator: AlertComparator::Lte,
                threshold: 4.0,
                clear: 8.0,
                predicate: RulePredicate::WindowMax,
                window_secs: 600,
            }],
        }],
        device_hourly_ceiling: 25,
    };
    assert_eq!(round_trip(&msg), msg);
}

#[test]
fn every_predicate_kind_round_trips() {
    for predicate in [
        RulePredicate::Instant,
        RulePredicate::Rate,
        RulePredicate::WindowMax,
        RulePredicate::WindowMean,
    ] {
        let mut r = rule("cpu.total");
        r.predicate = predicate;
        r.window_secs = u32::from(predicate != RulePredicate::Instant) * 60;
        let msg = ControlMessage::PushAlertRules {
            rules: vec![r],
            device_hourly_ceiling: 0,
        };
        assert_eq!(
            round_trip(&msg),
            msg,
            "{predicate:?} did not survive the wire"
        );
    }
}

#[test]
fn rule_written_before_the_grammar_extension_still_decodes() {
    // Only the six original fields; the rule decodes as instant with no window or extra terms.
    let legacy = serde_json::json!({
        "type": "PushAlertRules",
        "rules": [{
            "id": "disk-critical",
            "metric": "disk.used",
            "comparator": "Gte",
            "threshold": 90.0,
            "clear": 85.0,
            "sustain_secs": 300,
        }],
    });
    let buf = rmp_serde::to_vec_named(&legacy).expect("encode legacy rule");

    match rmp_serde::from_slice::<ControlMessage>(&buf).expect("decode legacy rule") {
        ControlMessage::PushAlertRules {
            rules,
            device_hourly_ceiling,
        } => {
            assert_eq!(rules.len(), 1);
            assert_eq!(
                device_hourly_ceiling, 0,
                "a push carrying no allowance leaves the machine on the one it has"
            );
            assert_eq!(rules[0].predicate, RulePredicate::Instant);
            assert_eq!(rules[0].window_secs, 0);
            assert!(rules[0].all.is_empty());
            assert_eq!(
                canonical_rule_metric(&rules[0].metric),
                Some("disk.used_percent"),
                "a rule already on the fleet must keep watching the same thing"
            );
        }
        other => panic!("expected PushAlertRules, got {other:?}"),
    }
}

#[test]
fn coverage_rides_a_health_summary_and_round_trips() {
    let msg = ControlMessage::AgentHealthSummary {
        ts: 1_700_000_100,
        tenant_id: "00000000-0000-0000-0000-000000000002".to_string(),
        node_anomaly_rate: 0.0,
        per_family_rates: Vec::new(),
        recent_bitmask: Vec::new(),
        sampler_ver: String::new(),
        model_ver: String::new(),
        breaches: Vec::new(),
        rule_coverage: vec![
            RuleCoverage {
                rule_id: "disk-critical".to_string(),
                state: RuleCoverageState::Active,
            },
            RuleCoverage {
                rule_id: "io-stalled".to_string(),
                state: RuleCoverageState::Unsupported,
            },
        ],
    };
    assert_eq!(round_trip(&msg), msg);
}

#[test]
fn summary_without_coverage_decodes_as_no_coverage_reported() {
    // A summary without the coverage key decodes as the device reporting nothing.
    let legacy = serde_json::json!({
        "type": "AgentHealthSummary",
        "ts": 1_700_000_100_i64,
        "tenant_id": "t",
        "node_anomaly_rate": 0.25,
        "sampler_ver": "sysinfo-k2",
        "model_ver": "k2",
    });
    let buf = rmp_serde::to_vec_named(&legacy).expect("encode legacy summary");

    match rmp_serde::from_slice::<ControlMessage>(&buf).expect("decode legacy summary") {
        ControlMessage::AgentHealthSummary { rule_coverage, .. } => {
            assert!(rule_coverage.is_empty());
        }
        other => panic!("expected AgentHealthSummary, got {other:?}"),
    }
}
