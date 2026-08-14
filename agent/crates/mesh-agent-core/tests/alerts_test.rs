//! Declarative threshold-alert evaluation.
//!
//! The evaluator watches sampler dimensions against tenant-pushed
//! [`ThresholdRule`]s and emits a breach signal per firing rule. A breach must
//! sustain continuously for `sustain_secs` before it fires (rising-edge flap
//! suppression), and hysteresis (`clear`) keeps it firing until the metric
//! recovers past the clear boundary (falling-edge flap suppression). Delivery is
//! investigation-aid only — these tests pin the pure decision logic.
//!
//! Beside firing, every rule reports what it is *doing* on this device: a rule
//! naming a metric this host cannot read is `unsupported` and counted, never
//! quietly skipped, because silent partial coverage is the failure class the
//! whole telemetry program exists to eliminate.

use mesh_agent_core::alerts::{rule_cost, AlertEvaluator, RULE_BUDGET_READINGS_PER_SEC};
use mesh_agent_core::ml::sampler::{MetricSample, ProcessSample};
use mesh_protocol::{
    AlertComparator, RuleCoverageState, RulePredicate, RuleTerm, ThresholdRule, MAX_RULE_TERMS,
    MAX_RULE_WINDOW_SECS,
};

/// Build a metric sample with every reading present. `disk` is the fullest
/// mount's used percentage.
fn sample(cpu: f32, mem: f32, disk: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: Some(disk),
        disk_mounts_critical: Some(0),
        network_rx_bps: Some(0.0),
        network_tx_bps: Some(0.0),
        stall_cpu_some: Some(0.0),
        stall_mem_some: Some(0.0),
        stall_mem_full: Some(0.0),
        stall_io_some: Some(0.0),
        stall_io_full: Some(0.0),
        disk_await_ms: Some(0.0),
        disk_queue_depth: Some(0.0),
        processes: Vec::<ProcessSample>::new(),
    }
}

/// A sample from a host that publishes no kernel pressure information and whose
/// disk counters it cannot read — every optional reading absent. CPU and memory
/// are always measurable, so they stay present.
fn host_without_optional_readings(cpu: f32, mem: f32) -> MetricSample {
    MetricSample {
        cpu_total_percent: cpu,
        memory_used_percent: mem,
        disk_used_percent: None,
        disk_mounts_critical: None,
        network_rx_bps: None,
        network_tx_bps: None,
        stall_cpu_some: None,
        stall_mem_some: None,
        stall_mem_full: None,
        stall_io_some: None,
        stall_io_full: None,
        disk_await_ms: None,
        disk_queue_depth: None,
        processes: Vec::<ProcessSample>::new(),
    }
}

/// A sample carrying one disk-performance reading, everything else at rest.
fn disk_sample(await_ms: f32, queue_depth: f32) -> MetricSample {
    let mut s = sample(0.0, 0.0, 0.0);
    s.disk_await_ms = Some(await_ms);
    s.disk_queue_depth = Some(queue_depth);
    s
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
        predicate: RulePredicate::Instant,
        window_secs: 0,
        all: Vec::new(),
    }
}

/// An instant rule re-shaped to derive its compared number over a window.
fn windowed(mut r: ThresholdRule, predicate: RulePredicate, window_secs: u32) -> ThresholdRule {
    r.predicate = predicate;
    r.window_secs = window_secs;
    r
}

/// The coverage state the evaluator reports for `rule_id`.
fn coverage_of(eval: &AlertEvaluator, rule_id: &str) -> RuleCoverageState {
    eval.coverage()
        .into_iter()
        .find(|entry| entry.rule_id == rule_id)
        .unwrap_or_else(|| panic!("{rule_id} is missing from coverage entirely"))
        .state
}

#[test]
fn empty_ruleset_never_breaches() {
    let mut eval = AlertEvaluator::new(vec![]);
    assert!(eval.evaluate(&sample(100.0, 100.0, 100.0), 0).is_empty());
    assert!(eval.coverage().is_empty());
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
        "mem.used_percent",
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
        "disk.used_percent",
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
        rule(
            "disk-full",
            "disk.used_percent",
            AlertComparator::Gt,
            95.0,
            90.0,
            0,
        ),
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
    assert!(eval.coverage().is_empty());
}

// ---------------------------------------------------------------------------
// Metric-name aliasing
// ---------------------------------------------------------------------------

#[test]
fn legacy_memory_name_fires_against_the_canonical_reading() {
    // A rule pushed to the fleet before the rename says `mem.used`; the reading
    // it must watch is `mem.used_percent`. It fires, and the breach it emits
    // carries the canonical name so nothing downstream sees two names for one
    // thing.
    let mut eval = AlertEvaluator::new(vec![rule(
        "memory-pressure",
        "mem.used",
        AlertComparator::Gte,
        95.0,
        85.0,
        0,
    )]);
    let breaches = eval.evaluate(&sample(0.0, 97.0, 0.0), 0);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].metric, "mem.used_percent");
    assert_eq!(breaches[0].value, 97.0);
    assert_eq!(
        coverage_of(&eval, "memory-pressure"),
        RuleCoverageState::Active
    );
}

#[test]
fn legacy_disk_name_fires_against_the_canonical_reading() {
    let mut eval = AlertEvaluator::new(vec![rule(
        "disk-critical",
        "disk.used",
        AlertComparator::Gte,
        90.0,
        85.0,
        0,
    )]);
    let breaches = eval.evaluate(&sample(0.0, 0.0, 93.0), 0);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].metric, "disk.used_percent");
}

#[test]
fn canonical_and_legacy_names_watch_the_same_reading() {
    let mut eval = AlertEvaluator::new(vec![
        rule("legacy", "mem.used", AlertComparator::Gt, 90.0, 90.0, 0),
        rule(
            "canonical",
            "mem.used_percent",
            AlertComparator::Gt,
            90.0,
            90.0,
            0,
        ),
    ]);
    let breaches = eval.evaluate(&sample(0.0, 95.0, 0.0), 0);
    assert_eq!(breaches.len(), 2, "both spellings must fire on one reading");
    for breach in &breaches {
        assert_eq!(breach.metric, "mem.used_percent");
        assert_eq!(breach.value, 95.0);
    }
}

// ---------------------------------------------------------------------------
// Coverage: active / unsupported, never silently skipped
// ---------------------------------------------------------------------------

#[test]
fn unknown_metric_never_fires_and_is_counted_unsupported() {
    let eval = AlertEvaluator::new(vec![rule(
        "bogus",
        "not.a.metric",
        AlertComparator::Gt,
        0.0,
        0.0,
        0,
    )]);
    // The classification is static: an unknown name is unsupported before a
    // single sample has been taken.
    assert_eq!(coverage_of(&eval, "bogus"), RuleCoverageState::Unsupported);

    let mut eval = eval;
    assert!(eval.evaluate(&sample(100.0, 100.0, 100.0), 0).is_empty());
    assert_eq!(coverage_of(&eval, "bogus"), RuleCoverageState::Unsupported);
}

#[test]
fn every_rule_appears_exactly_once_in_coverage() {
    let mut eval = AlertEvaluator::new(vec![
        rule("cpu-high", "cpu.total", AlertComparator::Gt, 90.0, 80.0, 0),
        rule(
            "io-stalled",
            "stall.io.some",
            AlertComparator::Gt,
            10.0,
            5.0,
            0,
        ),
        rule("bogus", "not.a.metric", AlertComparator::Gt, 0.0, 0.0, 0),
    ]);
    eval.evaluate(&sample(50.0, 50.0, 50.0), 0);

    let coverage = eval.coverage();
    assert_eq!(coverage.len(), 3);
    let mut ids: Vec<_> = coverage.iter().map(|c| c.rule_id.clone()).collect();
    ids.sort();
    assert_eq!(ids, vec!["bogus", "cpu-high", "io-stalled"]);
}

#[test]
fn a_reading_this_host_cannot_take_is_unsupported_not_a_missed_evaluation() {
    // No kernel pressure information here. The rule is well-formed and the metric
    // is in the vocabulary — the host simply cannot answer it, which is a
    // permanent platform gap and must read as such rather than as a rule that
    // happened not to breach.
    let mut eval = AlertEvaluator::new(vec![rule(
        "io-stalled",
        "stall.io.some",
        AlertComparator::Gt,
        10.0,
        5.0,
        0,
    )]);
    assert!(eval
        .evaluate(&host_without_optional_readings(99.0, 99.0), 0)
        .is_empty());
    assert_eq!(
        coverage_of(&eval, "io-stalled"),
        RuleCoverageState::Unsupported
    );
}

#[test]
fn an_unsupported_rule_never_latches_a_breach() {
    // The reading disappears mid-flight (a disk that stopped answering its bus).
    // A rule already firing must not stay latched on a value nobody can read.
    let mut eval = AlertEvaluator::new(vec![rule(
        "disk-slow",
        "disk.await_ms",
        AlertComparator::Gt,
        20.0,
        10.0,
        0,
    )]);
    assert_eq!(eval.evaluate(&disk_sample(40.0, 1.0), 0).len(), 1);
    assert!(eval
        .evaluate(&host_without_optional_readings(0.0, 0.0), 1)
        .is_empty());
    assert_eq!(
        coverage_of(&eval, "disk-slow"),
        RuleCoverageState::Unsupported
    );
    // And it starts from Clear when the reading returns: a value inside the old
    // hysteresis band is not a continuation of a breach nobody was watching.
    assert!(eval.evaluate(&disk_sample(15.0, 1.0), 2).is_empty());
}

// ---------------------------------------------------------------------------
// Rate of change
// ---------------------------------------------------------------------------

/// Feed `window + 1` seconds of a linear series at `slope` units/second.
fn feed_linear(eval: &mut AlertEvaluator, window: u32, start: f32, slope: f32) -> usize {
    let mut fired = 0;
    for ts in 0..=i64::from(window) {
        let value = start + slope * ts as f32;
        fired = eval.evaluate(&disk_sample(value, 0.0), ts).len();
    }
    fired
}

#[test]
fn rate_fires_on_a_rising_series() {
    // Service time climbing 2 ms every second: the wear-out shape, which no
    // threshold on the instantaneous value catches until it is already bad.
    let mut eval = AlertEvaluator::new(vec![windowed(
        rule(
            "disk-degrading",
            "disk.await_ms",
            AlertComparator::Gt,
            1.0,
            0.5,
            0,
        ),
        RulePredicate::Rate,
        10,
    )]);
    assert_eq!(feed_linear(&mut eval, 10, 0.0, 2.0), 1);
    assert_eq!(
        coverage_of(&eval, "disk-degrading"),
        RuleCoverageState::Active
    );
}

#[test]
fn rate_never_fires_on_a_flat_series() {
    // A high but perfectly steady reading has no rate. A rate rule that fired
    // here would fire on every busy-but-healthy machine in the fleet.
    let mut eval = AlertEvaluator::new(vec![windowed(
        rule(
            "disk-degrading",
            "disk.await_ms",
            AlertComparator::Gt,
            1.0,
            0.5,
            0,
        ),
        RulePredicate::Rate,
        10,
    )]);
    assert_eq!(feed_linear(&mut eval, 10, 500.0, 0.0), 0);
}

#[test]
fn rate_direction_is_signed() {
    // A falling series must not satisfy a "rising faster than" rule, and must
    // satisfy a "falling faster than" one.
    let mut rising = AlertEvaluator::new(vec![windowed(
        rule("rising", "disk.await_ms", AlertComparator::Gt, 1.0, 0.5, 0),
        RulePredicate::Rate,
        10,
    )]);
    assert_eq!(feed_linear(&mut rising, 10, 100.0, -2.0), 0);

    let mut falling = AlertEvaluator::new(vec![windowed(
        rule(
            "falling",
            "disk.await_ms",
            AlertComparator::Lt,
            -1.0,
            -0.5,
            0,
        ),
        RulePredicate::Rate,
        10,
    )]);
    assert_eq!(feed_linear(&mut falling, 10, 100.0, -2.0), 1);
}

#[test]
fn rate_holds_its_sustain_window() {
    // A rate rule flaps as easily as a threshold rule, so sustain applies to it
    // unchanged: the rate must hold for the full window before anything fires.
    let mut eval = AlertEvaluator::new(vec![windowed(
        rule("rising", "disk.await_ms", AlertComparator::Gt, 1.0, 0.5, 3),
        RulePredicate::Rate,
        5,
    )]);
    // ts 0..=4 is the warm-up: the window is not spanned, so nothing decides.
    for ts in 0..5 {
        assert!(eval
            .evaluate(&disk_sample(2.0 * ts as f32, 0.0), ts)
            .is_empty());
    }
    // ts 5..=7: the rate is breaching but the sustain window has not elapsed.
    for ts in 5..8 {
        assert!(
            eval.evaluate(&disk_sample(2.0 * ts as f32, 0.0), ts)
                .is_empty(),
            "rate must not fire before its sustain elapses (ts={ts})"
        );
    }
    assert_eq!(eval.evaluate(&disk_sample(16.0, 0.0), 8).len(), 1);
}

// ---------------------------------------------------------------------------
// Window aggregates
// ---------------------------------------------------------------------------

/// Feed a series of readings, one per second from ts 0, returning what fired on
/// the last one.
fn feed(eval: &mut AlertEvaluator, values: &[f32]) -> usize {
    let mut fired = 0;
    for (ts, value) in values.iter().enumerate() {
        fired = eval.evaluate(&disk_sample(*value, 0.0), ts as i64).len();
    }
    fired
}

#[test]
fn window_max_fires_on_a_peak_the_mean_hides() {
    // One second pinned at 95 ms inside a ten-second window: the maximum sees the
    // freeze, the mean reads it as noise. Both rules are declared here, so the
    // difference is the predicate and nothing else.
    let readings = [5.0f32, 5.0, 5.0, 5.0, 95.0, 5.0, 5.0, 5.0, 5.0, 5.0, 5.0];

    let mut peak = AlertEvaluator::new(vec![windowed(
        rule("froze", "disk.await_ms", AlertComparator::Gt, 50.0, 40.0, 0),
        RulePredicate::WindowMax,
        10,
    )]);
    assert_eq!(feed(&mut peak, &readings), 1);

    let mut mean = AlertEvaluator::new(vec![windowed(
        rule("froze", "disk.await_ms", AlertComparator::Gt, 50.0, 40.0, 0),
        RulePredicate::WindowMean,
        10,
    )]);
    assert_eq!(
        feed(&mut mean, &readings),
        0,
        "a single spike must not move a ten-second mean past the line"
    );
}

#[test]
fn window_mean_fires_on_a_sustained_shift_the_peak_alone_would_not_prove() {
    // Every reading is under the line; their mean is not. This is the shape that
    // says a disk is generally slow rather than momentarily busy.
    let readings = [60.0f32; 11];
    let mut mean = AlertEvaluator::new(vec![windowed(
        rule("slow", "disk.await_ms", AlertComparator::Gt, 50.0, 40.0, 0),
        RulePredicate::WindowMean,
        10,
    )]);
    assert_eq!(feed(&mut mean, &readings), 1);
}

#[test]
fn a_partial_window_is_not_enough_data_and_never_fires() {
    // Fewer seconds than the window asks for is not a small window — it is no
    // answer. Firing here would make every agent restart page someone.
    let mut eval = AlertEvaluator::new(vec![windowed(
        rule("froze", "disk.await_ms", AlertComparator::Gt, 50.0, 40.0, 0),
        RulePredicate::WindowMax,
        10,
    )]);
    // Nine readings, every one of them far past the threshold.
    assert_eq!(feed(&mut eval, &[95.0f32; 9]), 0);
    // The rule is still evaluating; it is warming up, not unsupported.
    assert_eq!(coverage_of(&eval, "froze"), RuleCoverageState::Active);
    // The tenth second spans the window and the answer arrives.
    assert_eq!(eval.evaluate(&disk_sample(95.0, 0.0), 9).len(), 0);
    assert_eq!(eval.evaluate(&disk_sample(95.0, 0.0), 10).len(), 1);
}

#[test]
fn a_windowed_rule_holds_its_hysteresis() {
    // Once a windowed rule fires it stays firing until the derived number
    // recovers past the clear boundary, exactly as an instant rule does.
    let mut eval = AlertEvaluator::new(vec![windowed(
        rule("slow", "disk.await_ms", AlertComparator::Gt, 50.0, 40.0, 0),
        RulePredicate::WindowMean,
        4,
    )]);
    assert_eq!(feed(&mut eval, &[60.0f32; 5]), 1);
    // The four-second mean falls to 48 — under the threshold but still inside the
    // hysteresis band, so the breach holds rather than flapping off.
    assert_eq!(eval.evaluate(&disk_sample(0.0, 0.0), 5).len(), 1);
    // A second quiet reading takes it to 36, past the clear boundary: cleared.
    assert!(eval.evaluate(&disk_sample(0.0, 0.0), 6).is_empty());
}

// ---------------------------------------------------------------------------
// Cross-dimension conjunction
// ---------------------------------------------------------------------------

/// A rule that fires only when the disk is both slow and deeply queued.
fn slow_and_queued() -> ThresholdRule {
    let mut r = rule(
        "disk-in-trouble",
        "disk.await_ms",
        AlertComparator::Gt,
        20.0,
        10.0,
        0,
    );
    r.all = vec![RuleTerm {
        metric: "disk.queue_depth".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 8.0,
        clear: 4.0,
        predicate: RulePredicate::Instant,
        window_secs: 0,
    }];
    r
}

#[test]
fn conjunction_fires_only_when_every_side_holds() {
    let mut eval = AlertEvaluator::new(vec![slow_and_queued()]);
    // A slow disk that is not backed up: a single I/O taking its time.
    assert!(eval.evaluate(&disk_sample(40.0, 2.0), 0).is_empty());
    // A deep queue that is being served quickly: the 02:00 backup, and healthy.
    assert!(eval.evaluate(&disk_sample(3.0, 28.0), 1).is_empty());
    // Both at once: the device is in trouble.
    let breaches = eval.evaluate(&disk_sample(40.0, 28.0), 2);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].rule_id, "disk-in-trouble");
    assert_eq!(
        breaches[0].metric, "disk.await_ms",
        "the breach names the rule's own metric"
    );
    assert_eq!(breaches[0].value, 40.0);
}

#[test]
fn conjunction_clears_when_any_side_recovers_past_its_own_boundary() {
    let mut eval = AlertEvaluator::new(vec![slow_and_queued()]);
    assert_eq!(eval.evaluate(&disk_sample(40.0, 28.0), 0).len(), 1);
    // The queue drains between its clear boundary and its threshold — the
    // situation has not recovered, so the rule stays firing.
    assert_eq!(eval.evaluate(&disk_sample(40.0, 6.0), 1).len(), 1);
    // Now it drains past the boundary: half the conjunction is genuinely gone.
    assert!(eval.evaluate(&disk_sample(40.0, 3.0), 2).is_empty());
}

#[test]
fn conjunction_with_an_unsupported_side_is_unsupported_not_false() {
    // The temptation is to read "cannot evaluate" as "did not breach". That
    // reports a rule as watching a machine it is not watching.
    let mut r = rule(
        "cpu-and-stall",
        "cpu.total",
        AlertComparator::Gt,
        50.0,
        40.0,
        0,
    );
    r.all = vec![RuleTerm {
        metric: "stall.io.some".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 10.0,
        clear: 5.0,
        predicate: RulePredicate::Instant,
        window_secs: 0,
    }];
    let mut eval = AlertEvaluator::new(vec![r]);

    assert!(eval
        .evaluate(&host_without_optional_readings(99.0, 0.0), 0)
        .is_empty());
    assert_eq!(
        coverage_of(&eval, "cpu-and-stall"),
        RuleCoverageState::Unsupported,
        "one unreadable side makes the whole rule unsupported"
    );
}

#[test]
fn conjunction_terms_carry_their_own_predicates() {
    // The primary side is a rate and the extra side a window maximum: the two
    // halves of "the disk is getting slower and it is also freezing".
    let mut r = windowed(
        rule(
            "degrading",
            "disk.await_ms",
            AlertComparator::Gt,
            1.0,
            0.5,
            0,
        ),
        RulePredicate::Rate,
        5,
    );
    r.all = vec![RuleTerm {
        metric: "disk.queue_depth".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 10.0,
        clear: 5.0,
        predicate: RulePredicate::WindowMax,
        window_secs: 5,
    }];
    let mut eval = AlertEvaluator::new(vec![r]);

    // Rising service time, but the queue never got deep: no fire.
    for ts in 0..=5 {
        assert!(eval
            .evaluate(&disk_sample(2.0 * ts as f32, 1.0), ts)
            .is_empty());
    }
    // A deep queue enters the window while the rise continues.
    let mut fired = 0;
    for ts in 6..=11 {
        fired = eval.evaluate(&disk_sample(2.0 * ts as f32, 20.0), ts).len();
    }
    assert_eq!(fired, 1);
}

// ---------------------------------------------------------------------------
// The grammar's own bounds
// ---------------------------------------------------------------------------

#[test]
fn a_window_past_the_grammar_bound_is_unsupported() {
    // The cost of every expressible rule must be bounded, so a window nobody
    // could afford to retain is refused by name rather than attempted.
    let eval = AlertEvaluator::new(vec![windowed(
        rule("greedy", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
        RulePredicate::WindowMean,
        MAX_RULE_WINDOW_SECS + 1,
    )]);
    assert_eq!(coverage_of(&eval, "greedy"), RuleCoverageState::Unsupported);
}

#[test]
fn the_largest_allowed_window_is_expressible() {
    let eval = AlertEvaluator::new(vec![windowed(
        rule("patient", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
        RulePredicate::WindowMean,
        MAX_RULE_WINDOW_SECS,
    )]);
    assert_eq!(coverage_of(&eval, "patient"), RuleCoverageState::Active);
}

#[test]
fn a_windowed_predicate_with_no_window_is_unsupported() {
    let eval = AlertEvaluator::new(vec![windowed(
        rule("empty", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
        RulePredicate::WindowMax,
        0,
    )]);
    assert_eq!(coverage_of(&eval, "empty"), RuleCoverageState::Unsupported);
}

#[test]
fn an_instant_predicate_carrying_a_window_is_unsupported() {
    // One shape, one meaning: a window on an instant reading would be a field
    // the evaluator silently ignores, and a rule nobody can predict from its own
    // text.
    let eval = AlertEvaluator::new(vec![windowed(
        rule("confused", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
        RulePredicate::Instant,
        60,
    )]);
    assert_eq!(
        coverage_of(&eval, "confused"),
        RuleCoverageState::Unsupported
    );
}

#[test]
fn more_terms_than_the_grammar_allows_is_unsupported() {
    let term = RuleTerm {
        metric: "cpu.total".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 1.0,
        clear: 0.0,
        predicate: RulePredicate::Instant,
        window_secs: 0,
    };
    let mut r = rule("sprawling", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0);
    r.all = vec![term; MAX_RULE_TERMS + 1];
    let eval = AlertEvaluator::new(vec![r]);
    assert_eq!(
        coverage_of(&eval, "sprawling"),
        RuleCoverageState::Unsupported
    );
}

// ---------------------------------------------------------------------------
// Cost: statically computable for everything the grammar can express
// ---------------------------------------------------------------------------

#[test]
fn an_instant_rule_costs_one_reading() {
    assert_eq!(
        rule_cost(&rule("r", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0)),
        1
    );
}

#[test]
fn every_windowed_predicate_costs_its_window() {
    for predicate in [
        RulePredicate::Rate,
        RulePredicate::WindowMax,
        RulePredicate::WindowMean,
    ] {
        let r = windowed(
            rule("r", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
            predicate,
            60,
        );
        assert_eq!(rule_cost(&r), 61, "{predicate:?} must retain its window");
    }
}

#[test]
fn cost_is_monotone_in_window_size() {
    let mut previous = 0;
    for window in [1u32, 10, 100, MAX_RULE_WINDOW_SECS] {
        let cost = rule_cost(&windowed(
            rule("r", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
            RulePredicate::WindowMean,
            window,
        ));
        assert!(
            cost > previous,
            "a larger window must cost more (window={window})"
        );
        previous = cost;
    }
}

#[test]
fn conjunction_costs_the_sum_of_its_sides() {
    let mut r = windowed(
        rule("r", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
        RulePredicate::WindowMean,
        30,
    );
    r.all = vec![
        RuleTerm {
            metric: "mem.used_percent".to_string(),
            comparator: AlertComparator::Gt,
            threshold: 1.0,
            clear: 0.0,
            predicate: RulePredicate::Instant,
            window_secs: 0,
        },
        RuleTerm {
            metric: "disk.await_ms".to_string(),
            comparator: AlertComparator::Gt,
            threshold: 1.0,
            clear: 0.0,
            predicate: RulePredicate::Rate,
            window_secs: 10,
        },
    ];
    assert_eq!(rule_cost(&r), 31 + 1 + 11);
}

#[test]
fn every_expressible_rule_has_a_bounded_cost() {
    // The whole point of a closed grammar: there is no rule an operator can write
    // whose cost the build cannot compute before it reaches an endpoint.
    let term = RuleTerm {
        metric: "cpu.total".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 1.0,
        clear: 0.0,
        predicate: RulePredicate::Rate,
        window_secs: MAX_RULE_WINDOW_SECS,
    };
    let mut worst = windowed(
        rule("worst", "cpu.total", AlertComparator::Gt, 1.0, 0.0, 0),
        RulePredicate::WindowMean,
        MAX_RULE_WINDOW_SECS,
    );
    worst.all = vec![term; MAX_RULE_TERMS];

    let ceiling = u64::from(MAX_RULE_WINDOW_SECS + 1) * (MAX_RULE_TERMS as u64 + 1);
    assert_eq!(rule_cost(&worst), ceiling);
}

// What a rule costs the machine it is running on.
//
// The pack is cost-bounded before it ships, but the endpoint is what pays, and
// a rule can reach one without having come through that gate. So the machine
// enforces its own ceiling: a rule that costs more than its allowance stops,
// and only that rule stops.

/// A rule the pack's own cost gate would refuse: every condition the grammar
/// allows, each over the longest window it allows. Nothing shippable looks like
/// this — which is the point of having the endpoint check as well.
fn past_the_allowance(id: &str) -> ThresholdRule {
    let mut r = windowed(
        rule(id, "cpu.total", AlertComparator::Gte, 0.0, -1.0, 0),
        RulePredicate::WindowMean,
        MAX_RULE_WINDOW_SECS,
    );
    r.all = vec![
        RuleTerm {
            metric: "mem.used_percent".to_string(),
            comparator: AlertComparator::Gte,
            threshold: 0.0,
            clear: -1.0,
            predicate: RulePredicate::WindowMean,
            window_secs: MAX_RULE_WINDOW_SECS,
        };
        MAX_RULE_TERMS
    ];
    r
}

/// Run an evaluator over `seconds` of steady readings, one per second.
fn run_for(eval: &mut AlertEvaluator, seconds: i64) {
    for ts in 0..seconds {
        eval.evaluate(&sample(50.0, 50.0, 50.0), ts);
    }
}

#[test]
fn a_rule_past_its_allowance_is_throttled_hard() {
    let mut eval = AlertEvaluator::new(vec![past_the_allowance("greedy")]);
    run_for(&mut eval, 1_200);

    assert_eq!(
        coverage_of(&eval, "greedy"),
        RuleCoverageState::Throttled,
        "a rule costing more than its allowance stops running here"
    );
    assert!(
        eval.evaluate(&sample(50.0, 50.0, 50.0), 1_200).is_empty(),
        "a throttled rule evaluates nothing, so it cannot fire either"
    );
}

#[test]
fn a_throttle_stops_one_rule_and_not_the_evaluator() {
    // The expensive rule and a cheap one that is firing, together. One rule
    // silencing the others would turn a bad rollout into blanket blindness while
    // still looking contained.
    let cheap = rule("cpu-high", "cpu.total", AlertComparator::Gt, 40.0, 30.0, 0);
    let mut eval = AlertEvaluator::new(vec![past_the_allowance("greedy"), cheap]);
    run_for(&mut eval, 1_200);

    assert_eq!(coverage_of(&eval, "greedy"), RuleCoverageState::Throttled);
    assert_eq!(
        coverage_of(&eval, "cpu-high"),
        RuleCoverageState::Active,
        "the cheap rule is still watching this machine"
    );

    let breaches = eval.evaluate(&sample(50.0, 50.0, 50.0), 1_200);
    assert_eq!(breaches.len(), 1);
    assert_eq!(breaches[0].rule_id, "cpu-high");
}

#[test]
fn the_most_expensive_shippable_rule_is_never_throttled() {
    // Three conditions at the longest window the grammar allows is the most a
    // rule can cost and still pass the pack's cost gate. If that tripped the
    // endpoint's ceiling, the two gates would disagree and a legitimate rule
    // would stop working on the fleet.
    let mut r = windowed(
        rule("patient", "cpu.total", AlertComparator::Gte, 0.0, -1.0, 0),
        RulePredicate::WindowMean,
        MAX_RULE_WINDOW_SECS,
    );
    r.all = vec![
        RuleTerm {
            metric: "mem.used_percent".to_string(),
            comparator: AlertComparator::Gte,
            threshold: 0.0,
            clear: -1.0,
            predicate: RulePredicate::WindowMean,
            window_secs: MAX_RULE_WINDOW_SECS,
        };
        2
    ];
    assert!(
        rule_cost(&r) <= RULE_BUDGET_READINGS_PER_SEC,
        "this must be a rule the pack allows"
    );

    let mut eval = AlertEvaluator::new(vec![r]);
    run_for(&mut eval, 2_000);
    assert_eq!(coverage_of(&eval, "patient"), RuleCoverageState::Active);
}

#[test]
fn an_ordinary_rule_is_never_throttled() {
    let mut eval = AlertEvaluator::new(vec![rule(
        "disk-critical",
        "disk.used_percent",
        AlertComparator::Gte,
        90.0,
        85.0,
        300,
    )]);
    run_for(&mut eval, 5_000);
    assert_eq!(
        coverage_of(&eval, "disk-critical"),
        RuleCoverageState::Active
    );
}

#[test]
fn a_throttle_survives_the_same_ruleset_arriving_again() {
    // Reconnecting re-pushes the whole ruleset. A rule that was stopped for
    // costing too much must not start again every time a flaky link comes back —
    // it would spend an allowance an hour, forever, on the same machine.
    let mut eval = AlertEvaluator::new(vec![past_the_allowance("greedy")]);
    run_for(&mut eval, 1_200);
    assert_eq!(coverage_of(&eval, "greedy"), RuleCoverageState::Throttled);

    eval.set_rules(vec![past_the_allowance("greedy")]);
    assert_eq!(
        coverage_of(&eval, "greedy"),
        RuleCoverageState::Throttled,
        "an identical re-push does not undo a throttle"
    );
}

#[test]
fn a_changed_rule_gets_its_allowance_back() {
    // Retuning the rule is what un-stops it: a different rule is a different
    // question, and it is owed the chance to answer it.
    let mut eval = AlertEvaluator::new(vec![past_the_allowance("greedy")]);
    run_for(&mut eval, 1_200);
    assert_eq!(coverage_of(&eval, "greedy"), RuleCoverageState::Throttled);

    eval.set_rules(vec![rule(
        "greedy",
        "cpu.total",
        AlertComparator::Gt,
        90.0,
        80.0,
        0,
    )]);
    eval.evaluate(&sample(50.0, 50.0, 50.0), 2_000);
    assert_eq!(
        coverage_of(&eval, "greedy"),
        RuleCoverageState::Active,
        "the retuned rule runs"
    );
}
