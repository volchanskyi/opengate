//! Retro scans evaluate a pushed rule over the minute-resolution history in the local store.

use std::time::Duration;

use edge_tsdb::{Durability, LocalTsdb, Sample, SeriesId, TsdbConfig};
use mesh_agent_core::alerts::{
    AlertEvaluator, AlertOrigin, AlertSink, EdgeAlert, RetroBudget, RetroConditions, RetroCursor,
    RetroHold, RetroPlan, RetroScan, RetroStep, RetroUnsupported, DEVICE_HOURLY_CEILING,
};
use mesh_agent_core::ml::store_sink::{SERIES_DISK, SERIES_DISK_AWAIT_MS, SERIES_DISK_QUEUE_DEPTH};
use mesh_protocol::{
    AlertComparator, AlertEvidence, AlertSeverity, HistoryPoint, RuleCoverageState, RulePredicate,
    RuleTerm, ThresholdRule,
};

fn shipped_readings(alert: &EdgeAlert) -> Vec<HistoryPoint> {
    let evidence = AlertEvidence::decode(&alert.evidence, &alert.evidence_codec)
        .expect("a finding's evidence must read back");
    evidence
        .series
        .into_iter()
        .flat_map(|series| series.points)
        .collect()
}

/// Bucket-aligned, so each second's minute bucket lines up with its offset from here.
const START: i64 = 1_700_000_040;
const MICROS_PER_SEC: i64 = 1_000_000;
/// Scan time, years after the history, so a finding stamped with it is unmistakable.
const SCAN_NOW_MICROS: i64 = 1_900_000_000 * MICROS_PER_SEC;

fn disk_critical() -> ThresholdRule {
    ThresholdRule {
        id: "disk-critical".to_string(),
        version: 1,
        severity: AlertSeverity::Critical,
        metric: "disk.used_percent".to_string(),
        comparator: AlertComparator::Gte,
        threshold: 90.0,
        clear: 85.0,
        sustain_secs: 300,
        predicate: RulePredicate::Instant,
        window_secs: 0,
        all: Vec::new(),
    }
}

/// A `None` from `value_at` writes nothing for that second, leaving a hole in the history.
fn seed(
    dir: &std::path::Path,
    series: SeriesId,
    secs: i64,
    value_at: impl Fn(i64) -> Option<f64>,
) -> LocalTsdb {
    let mut db = LocalTsdb::open(dir, TsdbConfig::default()).unwrap();
    write_series(&mut db, series, secs, value_at);
    db
}

fn write_series(
    db: &mut LocalTsdb,
    series: SeriesId,
    secs: i64,
    value_at: impl Fn(i64) -> Option<f64>,
) {
    for i in 0..secs {
        if let Some(value) = value_at(i) {
            db.append(series, Sample::new(START + i, value), false)
                .unwrap();
        }
    }
    db.commit(Durability::Full).unwrap();
}

fn inside(i: i64, episodes: &[(i64, i64)]) -> bool {
    episodes
        .iter()
        .any(|&(from, len)| (from..from + len).contains(&i))
}

fn three_full_disk_episodes() -> impl Fn(i64) -> Option<f64> {
    move |i| {
        let episodes = [(600, 600), (3_600, 600), (7_200, 600)];
        Some(if inside(i, &episodes) { 96.0 } else { 50.0 })
    }
}

fn drain_scan(scan: &mut RetroScan, db: &LocalTsdb, sink: &AlertSink) -> RetroStep {
    let mut steps = 0;
    loop {
        let snapshot = db.snapshot().unwrap();
        let step = scan.run_chunk(&snapshot, sink, SCAN_NOW_MICROS).unwrap();
        steps += 1;
        assert!(steps < 10_000, "a scan that never finishes");
        match step {
            RetroStep::Yielded { .. } => continue,
            other => return other,
        }
    }
}

fn resume_scan_to_the_end(rule: &ThresholdRule, db: &LocalTsdb, sink: &AlertSink) {
    let budget = RetroBudget::new(30, 2.0);
    let mut cursor = RetroCursor::default();
    for step_count in 0..10_000 {
        let mut scan = RetroScan::resume(RetroPlan::for_rule(rule).unwrap(), budget, cursor);
        let snapshot = db.snapshot().unwrap();
        let step = scan.run_chunk(&snapshot, sink, SCAN_NOW_MICROS).unwrap();
        let next = scan.cursor();
        if !matches!(step, RetroStep::Yielded { .. }) {
            return;
        }
        assert_ne!(
            next, cursor,
            "the cursor stopped advancing after {step_count} chunks"
        );
        cursor = next;
    }
    panic!("a resumed scan that never finishes");
}

fn roomy() -> AlertSink {
    AlertSink::new(512, 512)
}

fn shipped_store() -> TsdbConfig {
    TsdbConfig {
        cap_bytes: 512 * 1024 * 1024,
        host_free_fraction: 0.05,
        default_scale: None,
    }
}

fn event_times(alerts: &[EdgeAlert]) -> Vec<i64> {
    alerts
        .iter()
        .map(|a| a.ts_micros / MICROS_PER_SEC)
        .collect()
}

#[test]
fn three_episodes_in_history_produce_three_backfilled_findings() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    let sink = roomy();
    let plan = RetroPlan::for_rule(&disk_critical()).unwrap();
    let mut scan = RetroScan::new(plan, RetroBudget::default());

    assert_eq!(drain_scan(&mut scan, &db, &sink), RetroStep::Complete);

    let alerts = sink.drain();
    assert_eq!(alerts.len(), 3, "one finding per episode");
    for alert in &alerts {
        assert_eq!(alert.origin, AlertOrigin::Backfilled);
        assert_eq!(alert.rule_id, "disk-critical");
        assert!(
            !shipped_readings(alert).is_empty(),
            "a finding carries the readings behind it"
        );
    }
    assert_eq!(scan.stats().findings, 3);
}

#[test]
fn a_finding_carries_the_time_it_happened_not_the_time_it_was_found() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );

    drain_scan(&mut scan, &db, &sink);

    assert_eq!(
        event_times(&sink.drain()),
        vec![START + 900, START + 3_900, START + 7_500]
    );
}

#[test]
fn every_finding_of_one_scan_shares_one_grouping_key() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );

    drain_scan(&mut scan, &db, &sink);

    let keys: std::collections::BTreeSet<(String, String)> = sink
        .drain()
        .into_iter()
        .map(|a| (a.rule_id, a.subject))
        .collect();
    assert_eq!(
        keys,
        [("disk-critical".to_string(), "disk.used_percent".to_string())]
            .into_iter()
            .collect(),
        "one (rule, scope) for the whole scan"
    );
}

#[test]
fn an_interrupted_scan_resumes_to_the_same_findings() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());

    let uninterrupted = roomy();
    let mut whole = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );
    drain_scan(&mut whole, &db, &uninterrupted);
    let expected = event_times(&uninterrupted.drain());

    let resumed = roomy();
    let budget = RetroBudget::new(30, 2.0);
    let mut cursor = RetroCursor::default();
    loop {
        let mut scan = RetroScan::resume(
            RetroPlan::for_rule(&disk_critical()).unwrap(),
            budget,
            cursor,
        );
        let snapshot = db.snapshot().unwrap();
        let step = scan
            .run_chunk(&snapshot, &resumed, SCAN_NOW_MICROS)
            .unwrap();
        cursor = scan.cursor();
        if !matches!(step, RetroStep::Yielded { .. }) {
            break;
        }
    }

    assert_eq!(event_times(&resumed.drain()), expected);
    assert!(!expected.is_empty(), "the fixture has findings to lose");
}

#[test]
fn a_rule_with_an_odd_sustain_resumes_onto_the_stored_minutes() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    // 90 s spans one and a half stored minutes.
    let odd = ThresholdRule {
        sustain_secs: 90,
        ..disk_critical()
    };

    let uninterrupted = roomy();
    let mut whole = RetroScan::new(RetroPlan::for_rule(&odd).unwrap(), RetroBudget::default());
    drain_scan(&mut whole, &db, &uninterrupted);
    let expected = event_times(&uninterrupted.drain());
    assert_eq!(
        expected,
        vec![START + 720, START + 3_720, START + 7_320],
        "it fires two minutes into each episode, the first whole minutes past 90 s"
    );

    let resumed = roomy();
    let mut cursor = RetroCursor::default();
    let mut evaluated = 0;
    loop {
        let mut scan = RetroScan::resume(
            RetroPlan::for_rule(&odd).unwrap(),
            RetroBudget::new(30, 2.0),
            cursor,
        );
        let snapshot = db.snapshot().unwrap();
        let step = scan
            .run_chunk(&snapshot, &resumed, SCAN_NOW_MICROS)
            .unwrap();
        cursor = scan.cursor();
        evaluated += scan.stats().buckets_evaluated;
        if !matches!(step, RetroStep::Yielded { .. }) {
            break;
        }
    }

    assert!(
        evaluated >= whole.stats().buckets_evaluated,
        "the resumed scan evaluated {evaluated} minutes, the uninterrupted one {}",
        whole.stats().buckets_evaluated
    );
    assert_eq!(event_times(&resumed.drain()), expected);
}

#[test]
fn a_scan_stops_when_its_rule_version_is_superseded() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    let sink = roomy();
    let mut scan = RetroScan::resume(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::new(30, 2.0),
        RetroCursor::default(),
    );

    let snapshot = db.snapshot().unwrap();
    scan.run_chunk(&snapshot, &sink, SCAN_NOW_MICROS).unwrap();

    assert!(
        !scan.superseded_by(&[disk_critical()]),
        "the same definition re-pushed is the same version"
    );
    assert!(
        scan.superseded_by(&[]),
        "a rule that is no longer installed at all"
    );
    let retuned = ThresholdRule {
        threshold: 95.0,
        ..disk_critical()
    };
    assert!(
        scan.superseded_by(&[retuned]),
        "a retuned threshold is a different version to scan for"
    );
    assert!(
        scan.cursor() != RetroCursor::default(),
        "the cursor holds where the stopped scan got to"
    );
}

#[test]
fn a_device_with_no_history_reports_an_empty_scope() {
    let dir = tempfile::tempdir().unwrap();
    let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );

    let snapshot = db.snapshot().unwrap();
    let step = scan.run_chunk(&snapshot, &sink, SCAN_NOW_MICROS).unwrap();

    assert_eq!(step, RetroStep::NoHistory);
    assert_ne!(step, RetroStep::Complete, "empty is not the same as done");
    assert!(sink.drain().is_empty());
    assert_eq!(scan.stats().buckets_evaluated, 0);
    assert_eq!(scan.scope(), None);
}

#[test]
fn a_chunk_is_bounded_and_every_chunk_stands_down() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    let sink = roomy();
    let budget = RetroBudget::new(20, 2.0);
    let mut scan = RetroScan::new(RetroPlan::for_rule(&disk_critical()).unwrap(), budget);

    let mut previous = scan.stats();
    let mut chunks = 0;
    loop {
        let snapshot = db.snapshot().unwrap();
        let step = scan.run_chunk(&snapshot, &sink, SCAN_NOW_MICROS).unwrap();
        let stats = scan.stats();
        assert!(
            stats.points_read - previous.points_read <= budget.chunk_points as u64,
            "a chunk read {} stored readings, budget is {}",
            stats.points_read - previous.points_read,
            budget.chunk_points
        );
        assert!(
            stats.busy_micros >= previous.busy_micros,
            "processor time is accounted for and never goes backwards"
        );
        previous = stats;
        chunks += 1;
        match step {
            RetroStep::Yielded { stand_down } => assert!(
                stand_down >= RetroBudget::MIN_STAND_DOWN,
                "a chunk that costs nothing still stands down, or the loop spins"
            ),
            _ => break,
        }
    }

    assert!(chunks > 5, "the fixture is big enough to need chunking");
    assert_eq!(scan.stats().chunks, chunks);
    assert_eq!(sink.drain().len(), 3, "chunking does not change the answer");
}

#[test]
fn the_stand_down_keeps_a_scan_inside_its_share_of_the_machine() {
    let budget = RetroBudget::new(4_096, 2.0);
    for busy_millis in [1u64, 7, 50, 250, 1_000, 10_000] {
        let busy = Duration::from_millis(busy_millis);
        let stand_down = budget.stand_down(busy);
        let share = busy.as_secs_f64() / (busy + stand_down).as_secs_f64();
        assert!(
            share <= budget.duty_percent / 100.0 + f64::EPSILON,
            "a {busy_millis} ms chunk took {:.3}% of the machine",
            share * 100.0
        );
    }
    assert!(
        RetroBudget::default().duty_percent < 5.0,
        "the scan's own budget stays well inside the agent's"
    );
}

#[test]
fn a_scan_stands_down_before_the_store_changes_what_it_keeps() {
    let store = shipped_store();
    let idle = |free: Option<u64>| RetroConditions {
        in_maintenance: false,
        cpu_percent: Some(3.0),
        host_free_bytes: free,
    };

    let engage = (store.cap_bytes as f64 / store.host_free_fraction) as u64;
    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&idle(Some(engage)), store),
        Some(RetroHold::DiskPressure),
        "the scan is already standing down where the store starts backing off"
    );

    let mut free = engage;
    while mesh_agent_core::alerts::retro_hold(&idle(Some(free)), store).is_some() {
        free = free.saturating_add(engage / 8);
        assert!(
            free < engage * 100,
            "the scan never resumes as space returns"
        );
    }
    assert_eq!(
        store.effective_cap(Some(free)),
        store.cap_bytes,
        "the scan resumed only where the store is at its full cap"
    );
    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&idle(None), store),
        None,
        "a host that has not reported its disk is not assumed to be full"
    );
}

#[test]
fn a_scan_waits_for_a_quiet_machine_and_never_runs_in_maintenance() {
    let store = shipped_store();
    let conditions = |in_maintenance: bool, cpu: Option<f32>| RetroConditions {
        in_maintenance,
        cpu_percent: cpu,
        host_free_bytes: Some(u64::MAX / 2),
    };

    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&conditions(false, Some(2.0)), store),
        None,
        "an idle machine scans"
    );
    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&conditions(false, Some(95.0)), store),
        Some(RetroHold::Busy)
    );
    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&conditions(true, Some(2.0)), store),
        Some(RetroHold::Maintenance),
        "maintenance stops it even on an idle machine"
    );
    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&conditions(false, None), store),
        Some(RetroHold::Busy),
        "a machine that has not reported its load is not assumed idle"
    );
}

#[test]
fn a_rule_finer_than_the_stored_minute_cannot_be_re_run() {
    assert!(RetroPlan::for_rule(&disk_critical()).is_ok());

    let brief = ThresholdRule {
        sustain_secs: 30,
        ..disk_critical()
    };
    assert_eq!(
        RetroPlan::for_rule(&brief).unwrap_err(),
        RetroUnsupported::FinerThanAMinute
    );

    let brief_window = ThresholdRule {
        predicate: RulePredicate::WindowMax,
        window_secs: 30,
        ..disk_critical()
    };
    assert_eq!(
        RetroPlan::for_rule(&brief_window).unwrap_err(),
        RetroUnsupported::FinerThanAMinute
    );

    let unknown_metric = ThresholdRule {
        metric: "nope.unknown".to_string(),
        ..disk_critical()
    };
    assert_eq!(
        RetroPlan::for_rule(&unknown_metric).unwrap_err(),
        RetroUnsupported::MetricNotStored
    );

    let unsustained = ThresholdRule {
        sustain_secs: 0,
        ..disk_critical()
    };
    assert!(
        RetroPlan::for_rule(&unsustained).is_ok(),
        "a rule with no sustain asks whether it ever crossed, which a minute can answer"
    );
}

#[test]
fn a_scan_cannot_blow_the_device_alert_ceiling() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 6_000, |i| {
        Some(if (i / 120) % 2 == 0 { 96.0 } else { 50.0 })
    });
    let sink = AlertSink::default();
    let brief = ThresholdRule {
        sustain_secs: 60,
        ..disk_critical()
    };
    let mut scan = RetroScan::new(RetroPlan::for_rule(&brief).unwrap(), RetroBudget::default());

    drain_scan(&mut scan, &db, &sink);

    let stats = sink.stats();
    assert!(
        stats.queued <= DEVICE_HOURLY_CEILING as usize,
        "{} findings admitted against a ceiling of {DEVICE_HOURLY_CEILING}",
        stats.queued
    );
    assert!(
        stats.suppressed_by_ceiling > 0,
        "and the excess is counted, not silently dropped"
    );
    assert!(
        scan.stats().findings > u64::from(DEVICE_HOURLY_CEILING),
        "the fixture really does exceed the ceiling"
    );
}

#[test]
fn a_gap_in_history_does_not_carry_a_breach_across_it() {
    let dir = tempfile::tempdir().unwrap();
    // Two minutes with no readings sit in the middle of a stretch over the line.
    let db = seed(dir.path(), SERIES_DISK, 900, |i| {
        (!(180..300).contains(&i)).then_some(96.0)
    });
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );

    drain_scan(&mut scan, &db, &sink);

    assert_eq!(event_times(&sink.drain()), vec![START + 600]);
}

#[test]
fn a_rule_with_two_sides_reads_both_at_the_same_minute() {
    let dir = tempfile::tempdir().unwrap();
    // Service time is bad throughout; the queue backs up only in the second half.
    let mut db = seed(dir.path(), SERIES_DISK_AWAIT_MS, 1_800, |_| Some(40.0));
    write_series(&mut db, SERIES_DISK_QUEUE_DEPTH, 1_800, |i| {
        Some(if i >= 900 { 28.0 } else { 1.0 })
    });

    let slow_and_backed_up = ThresholdRule {
        id: "disk-slow".to_string(),
        version: 1,
        severity: AlertSeverity::Warning,
        metric: "disk.await_ms".to_string(),
        comparator: AlertComparator::Gt,
        threshold: 20.0,
        clear: 20.0,
        sustain_secs: 300,
        predicate: RulePredicate::Instant,
        window_secs: 0,
        all: vec![RuleTerm {
            metric: "disk.queue_depth".to_string(),
            comparator: AlertComparator::Gt,
            threshold: 10.0,
            clear: 10.0,
            predicate: RulePredicate::Instant,
            window_secs: 0,
        }],
    };
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&slow_and_backed_up).unwrap(),
        RetroBudget::default(),
    );

    drain_scan(&mut scan, &db, &sink);

    assert_eq!(
        event_times(&sink.drain()),
        vec![START + 1_200],
        "it fires five minutes after the queue joined the slow service time"
    );
}

#[test]
fn a_peak_rule_and_an_average_rule_read_different_things_from_one_minute() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 3_600, |i| {
        Some(if i % 60 == 0 { 99.0 } else { 10.0 })
    });

    let peak = ThresholdRule {
        id: "disk-peak".to_string(),
        predicate: RulePredicate::WindowMax,
        window_secs: 300,
        sustain_secs: 0,
        comparator: AlertComparator::Gt,
        threshold: 90.0,
        clear: 90.0,
        ..disk_critical()
    };
    let average = ThresholdRule {
        id: "disk-average".to_string(),
        predicate: RulePredicate::WindowMean,
        ..peak.clone()
    };

    let peak_sink = roomy();
    let mut peak_scan = RetroScan::new(RetroPlan::for_rule(&peak).unwrap(), RetroBudget::default());
    drain_scan(&mut peak_scan, &db, &peak_sink);
    assert!(
        !peak_sink.drain().is_empty(),
        "the one-second spike is what a peak rule is for"
    );

    let average_sink = roomy();
    let mut average_scan = RetroScan::new(
        RetroPlan::for_rule(&average).unwrap(),
        RetroBudget::default(),
    );
    drain_scan(&mut average_scan, &db, &average_sink);
    assert!(
        average_sink.drain().is_empty(),
        "a minute averaging 11.5 has not crossed 90"
    );
}

#[test]
fn a_shape_the_live_evaluator_refuses_is_refused_over_history_too() {
    let ill_formed = [
        // An instant reading carrying a window it would silently ignore.
        ThresholdRule {
            window_secs: 300,
            ..disk_critical()
        },
        // A windowed predicate with no window to span.
        ThresholdRule {
            predicate: RulePredicate::WindowMax,
            window_secs: 0,
            ..disk_critical()
        },
        // A window past the bound the grammar states about itself.
        ThresholdRule {
            predicate: RulePredicate::WindowMean,
            window_secs: 100_000,
            ..disk_critical()
        },
        // A metric outside the vocabulary entirely.
        ThresholdRule {
            metric: "nope.unknown".to_string(),
            ..disk_critical()
        },
    ];

    for rule in ill_formed {
        let live = AlertEvaluator::new(vec![rule.clone()]);
        assert_eq!(
            live.coverage()[0].state,
            RuleCoverageState::Unsupported,
            "the live evaluator accepts {rule:?}"
        );
        assert!(
            RetroPlan::for_rule(&rule).is_err(),
            "history accepts a shape the live evaluator refuses: {rule:?}"
        );
    }
}

#[test]
fn a_finding_says_what_the_rule_means_and_shows_the_readings_behind_it() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );

    drain_scan(&mut scan, &db, &sink);
    let alerts = sink.drain();
    assert_eq!(alerts.len(), 3);

    for alert in &alerts {
        // The summary reads as the rule, in words: the metric, the relation its
        // comparator names, the line, and how long it had to hold.
        assert_eq!(
            alert.summary, "disk.used_percent at or above 90 for 5 min",
            "a finding's summary is the rule in the words the queue shows first"
        );
        assert_eq!(
            alert.subject, "disk.used_percent",
            "the subject names the metric the finding is about"
        );

        // The readings behind it are the run-up to the firing minute, oldest
        // first, under the name of the dimension the rule watched.
        let evidence = AlertEvidence::decode(&alert.evidence, &alert.evidence_codec)
            .expect("a finding's evidence must read back");
        assert_eq!(
            evidence.series.len(),
            1,
            "a scan over history looked at one dimension and says so"
        );
        assert_eq!(
            evidence.series[0].dim, "disk.used_percent",
            "the series names the dimension the rule watched"
        );
        assert!(
            evidence.ranked.is_empty(),
            "a scan computes no ranking, so it claims none"
        );

        let readings = shipped_readings(alert);
        assert!(
            !readings.is_empty(),
            "a finding carries the readings behind it"
        );
        assert!(
            readings.windows(2).all(|w| w[0].ts < w[1].ts),
            "readings run oldest first, got {readings:?}"
        );
        let fired_on = readings.last().unwrap();
        assert!(
            fired_on.ts <= alert.window_end_micros / MICROS_PER_SEC,
            "the last reading is no later than the minute the rule fired on"
        );
        assert!(
            readings.iter().any(|point| point.value >= 90.0),
            "the readings show the machine over its line, got {readings:?}"
        );
        assert_eq!(
            alert.value,
            Some(fired_on.value),
            "the alert names the reading that crossed the line"
        );
    }
}

#[test]
fn a_summary_names_the_relation_the_rule_actually_uses() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 3_600, |_| Some(95.0));
    let sink = roomy();

    let cases = [
        (AlertComparator::Gt, 0u32, "disk.used_percent above 90"),
        (
            AlertComparator::Gte,
            120,
            "disk.used_percent at or above 90 for 2 min",
        ),
    ];

    for (comparator, sustain_secs, want) in cases {
        let rule = ThresholdRule {
            comparator,
            sustain_secs,
            ..disk_critical()
        };
        let mut scan = RetroScan::new(RetroPlan::for_rule(&rule).unwrap(), RetroBudget::default());
        drain_scan(&mut scan, &db, &sink);
        let alerts = sink.drain();
        assert!(
            !alerts.is_empty(),
            "a machine held over the line must produce a finding for {comparator:?}"
        );
        assert_eq!(alerts[0].summary, want);
    }
}

#[test]
fn a_windowed_rule_resumes_across_its_window_as_well_as_its_sustain() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());

    // A five-minute average held for two minutes: the window is the wider term.
    let windowed = ThresholdRule {
        predicate: RulePredicate::WindowMean,
        window_secs: 300,
        sustain_secs: 120,
        ..disk_critical()
    };

    let uninterrupted = roomy();
    let mut whole = RetroScan::new(
        RetroPlan::for_rule(&windowed).unwrap(),
        RetroBudget::default(),
    );
    drain_scan(&mut whole, &db, &uninterrupted);
    let expected = event_times(&uninterrupted.drain());
    assert!(!expected.is_empty(), "the fixture has findings to lose");

    let resumed = roomy();
    resume_scan_to_the_end(&windowed, &db, &resumed);

    assert_eq!(event_times(&resumed.drain()), expected);
}
