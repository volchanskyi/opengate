//! WS-19 declarative threshold-alert evaluator tests.
//!
//! The evaluator watches sampler dimensions against tenant-pushed
//! [`ThresholdRule`]s and emits a breach signal per firing rule. A breach must
//! sustain continuously for `sustain_secs` before it fires (rising-edge flap
//! suppression), and hysteresis (`clear`) keeps it firing until the metric
//! recovers past the clear boundary (falling-edge flap suppression). Delivery is
//! investigation-aid only — these tests pin the pure decision logic.

use mesh_agent_core::alerts::AlertEvaluator;
use mesh_agent_core::ml::sampler::{MetricSample, ProcessSample};
use mesh_protocol::{AlertComparator, ThresholdRule};

/// Build a metric sample with the three gauge dimensions the evaluator watches.
fn sample(cpu: f32, mem: f32, disk: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: disk,
        network_rx_bytes: 0,
        network_tx_bytes: 0,
        processes: Vec::<ProcessSample>::new(),
    }
}

fn rule(
    id: &str,
    metric: &str,
    comparator: AlertComparator,
    threshold: f64,
    clear: f64,
    sustain_secs: u32,
) -> ThresholdRule {
    ThresholdRule {
        id: id.to_string(),
        metric: metric.to_string(),
        comparator,
        threshold,
        clear,
        sustain_secs,
    }
}

#[test]
fn empty_ruleset_never_breaches() {
    let mut eval = AlertEvaluator::new(vec![]);
    assert!(eval.evaluate(&sample(100.0, 100.0, 100.0), 0).is_empty());
}

#[test]
fn sustain_zero_fires_on_first_breaching_sample() {
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-high",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        0,
    )]);
    let breaches = eval.evaluate(&sample(95.0, 10.0, 10.0), 0);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].rule_id, "cpu-high");
    assert_eq!(breaches[0].metric, "cpu.total");
    assert_eq!(breaches[0].value, 95.0);
}

#[test]
fn sustained_breach_fires_only_after_n_seconds() {
    // cpu > 90 must hold continuously for 5 s before it fires.
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-sustained",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        5,
    )]);
    // t = 0..=4: breaching but the sustain window is not yet satisfied.
    for ts in 0..5 {
        assert!(
            eval.evaluate(&sample(95.0, 10.0, 10.0), ts).is_empty(),
            "must not fire before the sustain window elapses (ts={ts})"
        );
    }
    // t = 5: 5 s of continuous breach — fires now.
    let breaches = eval.evaluate(&sample(95.0, 10.0, 10.0), 5);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].rule_id, "cpu-sustained");
}

#[test]
fn brief_spike_below_sustain_never_fires() {
    // A 3 s spike under a 5 s sustain must be suppressed (rising-edge flapping).
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-sustained",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        5,
    )]);
    for ts in 0..3 {
        assert!(eval.evaluate(&sample(95.0, 10.0, 10.0), ts).is_empty());
    }
    // Recovers before the window elapses; the pending breach is discarded.
    assert!(eval.evaluate(&sample(10.0, 10.0, 10.0), 3).is_empty());
    // A later short spike must again restart the sustain window from scratch.
    for ts in 4..7 {
        assert!(eval.evaluate(&sample(95.0, 10.0, 10.0), ts).is_empty());
    }
}

#[test]
fn hysteresis_keeps_firing_until_clear_boundary() {
    // Fires above 90, only clears once it drops to/below 80. Between 80 and 90
    // it stays firing — a dip below the threshold is not a clear.
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-high",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        0,
    )]);
    assert_eq!(eval.evaluate(&sample(95.0, 0.0, 0.0), 0).len(), 1); // fires
    assert_eq!(
        eval.evaluate(&sample(85.0, 0.0, 0.0), 1).len(),
        1,
        "between clear and threshold stays firing"
    );
    assert_eq!(eval.evaluate(&sample(81.0, 0.0, 0.0), 2).len(), 1);
    assert!(
        eval.evaluate(&sample(80.0, 0.0, 0.0), 3).is_empty(),
        "reaching the clear boundary clears the breach"
    );
    assert!(eval.evaluate(&sample(85.0, 0.0, 0.0), 4).is_empty());
}

#[test]
fn dithering_around_threshold_does_not_flap() {
    // A value oscillating just above/below the threshold but always above the
    // clear boundary stays continuously firing — never a clear-then-refire cycle.
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-high",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        0,
    )]);
    let dither = [95.0f32, 89.0, 92.0, 88.0, 91.0, 87.0];
    for (ts, cpu) in dither.into_iter().enumerate() {
        let breaches = eval.evaluate(&sample(cpu, 0.0, 0.0), ts as i64);
        assert_eq!(
            breaches.len(),
            1,
            "must remain firing without flapping (ts={ts}, cpu={cpu})"
        );
    }
}

#[test]
fn lt_comparator_fires_low_and_clears_high_with_hysteresis() {
    // A "resource too low" rule: fires below 10, clears only above 20.
    let mut eval = AlertEvaluator::new(vec![rule(
        "mem-low",
        "mem.used",
        AlertComparator::Lt,
        10.0,
        20.0,
        0,
    )]);
    assert_eq!(eval.evaluate(&sample(0.0, 5.0, 0.0), 0).len(), 1); // below 10 → fires
    assert_eq!(
        eval.evaluate(&sample(0.0, 15.0, 0.0), 1).len(),
        1,
        "between threshold and clear stays firing"
    );
    assert!(
        eval.evaluate(&sample(0.0, 20.0, 0.0), 2).is_empty(),
        "reaching the clear boundary clears"
    );
}

#[test]
fn gte_and_lte_boundaries_are_inclusive() {
    let mut gte = AlertEvaluator::new(vec![rule(
        "cpu-gte",
        "cpu.total",
        AlertComparator::Gte,
        90.0,
        90.0,
        0,
    )]);
    assert_eq!(
        gte.evaluate(&sample(90.0, 0.0, 0.0), 0).len(),
        1,
        "gte fires at exactly the threshold"
    );

    let mut lte = AlertEvaluator::new(vec![rule(
        "disk-lte",
        "disk.used",
        AlertComparator::Lte,
        5.0,
        5.0,
        0,
    )]);
    assert_eq!(
        lte.evaluate(&sample(0.0, 0.0, 5.0), 0).len(),
        1,
        "lte fires at exactly the threshold"
    );
}

#[test]
fn multiple_rules_fire_independently() {
    let mut eval = AlertEvaluator::new(vec![
        rule("cpu-high", "cpu.total", AlertComparator::Gt, 90.0, 80.0, 0),
        rule("disk-full", "disk.used", AlertComparator::Gt, 95.0, 90.0, 0),
    ]);
    // Only CPU breaches.
    let breaches = eval.evaluate(&sample(99.0, 0.0, 50.0), 0);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].rule_id, "cpu-high");
    // Now both breach.
    let breaches = eval.evaluate(&sample(99.0, 0.0, 99.0), 1);
    assert_eq!(breaches.len(), 2);
    let ids: Vec<_> = breaches.iter().map(|b| b.rule_id.as_str()).collect();
    assert!(ids.contains(&"cpu-high"));
    assert!(ids.contains(&"disk-full"));
}

#[test]
fn unknown_metric_never_fires() {
    let mut eval = AlertEvaluator::new(vec![rule(
        "bogus",
        "not.a.metric",
        AlertComparator::Gt,
        0.0,
        0.0,
        0,
    )]);
    assert!(eval.evaluate(&sample(100.0, 100.0, 100.0), 0).is_empty());
}

#[test]
fn set_rules_preserves_firing_state_for_unchanged_rule() {
    let r = rule("cpu-high", "cpu.total", AlertComparator::Gt, 90.0, 80.0, 0);
    let mut eval = AlertEvaluator::new(vec![r.clone()]);
    assert_eq!(eval.evaluate(&sample(95.0, 0.0, 0.0), 0).len(), 1); // firing

    // Re-pushing an identical ruleset (server reconnect) must not reset the
    // firing state — the breach keeps firing under hysteresis at cpu=85.
    eval.set_rules(vec![r]);
    assert_eq!(
        eval.evaluate(&sample(85.0, 0.0, 0.0), 1).len(),
        1,
        "identical re-push keeps hysteresis state"
    );
}

#[test]
fn set_rules_resets_state_when_definition_changes() {
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-high",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        0,
    )]);
    assert_eq!(eval.evaluate(&sample(95.0, 0.0, 0.0), 0).len(), 1); // firing

    // A changed threshold is a new definition: state resets, so a value that
    // only satisfied the old hysteresis band no longer keeps it firing.
    eval.set_rules(vec![rule(
        "cpu-high",
        "cpu.total",
        AlertComparator::Gt,
        97.0,
        95.0,
        0,
    )]);
    assert!(
        eval.evaluate(&sample(85.0, 0.0, 0.0), 1).is_empty(),
        "changed rule definition resets to clear"
    );
}

#[test]
fn set_rules_removes_dropped_rules() {
    let mut eval = AlertEvaluator::new(vec![rule(
        "cpu-high",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        0,
    )]);
    assert_eq!(eval.evaluate(&sample(95.0, 0.0, 0.0), 0).len(), 1);
    // Empty push removes every rule — nothing can breach afterwards.
    eval.set_rules(vec![]);
    assert!(eval.evaluate(&sample(100.0, 100.0, 100.0), 1).is_empty());
}
