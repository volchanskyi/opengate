//! Re-running a rule over the history a device already holds.
//!
//! "Has this happened before?" is answered on the machine, by evaluating a newly
//! pushed rule against the minute-by-minute history in the local store — not by
//! shipping every device's seconds to a central recorder. The tests here pin the
//! three things that makes that safe to do: a finding is stamped with the time it
//! *happened*, a scan can be stopped and resumed without changing its answer, and
//! a scan can never take more of the machine, or more of the device's alert
//! allowance, than a live rule would.

use std::time::Duration;

use edge_tsdb::{Durability, LocalTsdb, Sample, SeriesId, TsdbConfig};
use mesh_agent_core::alerts::{
    AlertOrigin, AlertSink, EdgeAlert, RetroBudget, RetroConditions, RetroCursor, RetroHold,
    RetroPlan, RetroScan, RetroStep, RetroUnsupported, DEVICE_HOURLY_CEILING,
};
use mesh_agent_core::ml::store_sink::{SERIES_DISK, SERIES_DISK_AWAIT_MS, SERIES_DISK_QUEUE_DEPTH};
use mesh_protocol::{AlertComparator, RulePredicate, RuleTerm, ThresholdRule};

/// Bucket-aligned, so a second's timestamp and its minute bucket line up and
/// every expectation can be read off the offset from here.
const START: i64 = 1_700_000_040;
const MICROS_PER_SEC: i64 = 1_000_000;
/// Wall-clock instant the scans below run at — years after the history they read,
/// so an alert stamped with scan time instead of event time is unmistakable.
const SCAN_NOW_MICROS: i64 = 1_900_000_000 * MICROS_PER_SEC;

/// The shipped shape of `disk-critical`: a percentage that has to stay over the
/// line for five minutes, with a hysteresis band under it.
fn disk_critical() -> ThresholdRule {
    ThresholdRule {
        id: "disk-critical".to_string(),
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

/// A store holding one second-by-second series, valued by `value_at(second)`.
/// `None` writes nothing for that second, which is the hole a device that was
/// switched off leaves behind.
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

/// Append one more second-by-second series into an open store.
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

/// Whether second `i` falls inside one of `episodes`, each `(start, length)` in
/// seconds from [`START`].
fn inside(i: i64, episodes: &[(i64, i64)]) -> bool {
    episodes
        .iter()
        .any(|&(from, len)| (from..from + len).contains(&i))
}

/// Three separated stretches of a nearly-full disk in three hours of history.
fn three_full_disk_episodes() -> impl Fn(i64) -> Option<f64> {
    move |i| {
        let episodes = [(600, 600), (3_600, 600), (7_200, 600)];
        Some(if inside(i, &episodes) { 96.0 } else { 50.0 })
    }
}

/// Run a scan to its end, returning everything it raised and the step it
/// finished on. A generous budget keeps chunking out of the way of tests that
/// are about findings rather than pacing.
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

/// A roomy sink, so the ceiling is only in play in the test that is about it.
fn roomy() -> AlertSink {
    AlertSink::new(512, 512)
}

/// The store as the fleet ships it: a hard cap, with a backoff against the
/// host's own free space.
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

/// Three stretches of a full disk in history produce exactly three findings —
/// one per episode, not one per breaching minute — and every one of them is
/// marked as having come from history rather than from now.
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
            !alert.evidence.is_empty(),
            "a finding carries the readings behind it"
        );
    }
    assert_eq!(scan.stats().findings, 3);
}

/// A finding is stamped with the minute it happened, not the minute it was
/// found. Scan time would fold a freeze from three weeks ago into today's
/// incident and make every historical finding look simultaneous.
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

    // Each episode fires once its five-minute sustain has elapsed inside it.
    assert_eq!(
        event_times(&sink.drain()),
        vec![START + 900, START + 3_900, START + 7_500]
    );
}

/// All three findings share one grouping key — the rule and what on this device
/// it is about — so the incident engine folds a retro scan into one incident
/// rather than one per episode.
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

/// A scan stopped part-way and resumed from its cursor answers exactly what an
/// uninterrupted scan answers: no finding delivered twice, none lost across the
/// seam. The seam is the interesting case — an episode straddling it must be
/// reported once.
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

    // Walk the same history in small chunks, throwing the scan away after each
    // one and rebuilding it from nothing but the cursor — an agent restart.
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

/// A rule whose sustain is not a whole number of minutes resumes onto the minute
/// grid, not off it. The window a resume re-reads is as long as the rule's own
/// memory, so an odd sustain lands the read between two stored minutes — and a
/// read that lines up with nothing finds nothing, silently, for the rest of the
/// scan.
#[test]
fn a_rule_with_an_odd_sustain_resumes_onto_the_stored_minutes() {
    let dir = tempfile::tempdir().unwrap();
    let db = seed(dir.path(), SERIES_DISK, 10_800, three_full_disk_episodes());
    // 90 s: over the stored minute, so history can answer it, but not a whole
    // number of them.
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

    // Every minute the uninterrupted run looked at was looked at here too — a
    // resumed chunk that lands between two stored minutes reads nothing and
    // reports nothing, which is the quietest way to lose half a scan.
    assert!(
        evaluated >= whole.stats().buckets_evaluated,
        "the resumed scan evaluated {evaluated} minutes, the uninterrupted one {}",
        whole.stats().buckets_evaluated
    );
    assert_eq!(event_times(&resumed.drain()), expected);
}

/// A scan whose rule version is replaced mid-run stops rather than finishing
/// against a definition nobody is using any more.
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

/// A device enrolled this morning has nothing to look back over, and the scan
/// says so. Reporting an empty scope as a completed scan would claim the machine
/// has been checked back through history it never had.
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

/// The throttle is in the shape of the work, not in how fast the machine
/// happens to be: a chunk reads at most the budgeted number of stored readings,
/// and every chunk hands back a stand-down the caller has to observe before the
/// next one.
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

/// Whatever a chunk costs, the stand-down after it keeps the scan's share of the
/// machine inside its budget. Asserted as arithmetic over the budget rather than
/// by timing a run, which no test can do deterministically.
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

/// The scan stands down while the host disk is filling, and it does so *before*
/// the store starts shrinking what it keeps — a scan competing for the last of a
/// host's disk with the eviction that is trying to free it helps nobody.
#[test]
fn a_scan_stands_down_before_the_store_changes_what_it_keeps() {
    let store = shipped_store();
    let idle = |free: Option<u64>| RetroConditions {
        in_maintenance: false,
        cpu_percent: Some(3.0),
        host_free_bytes: free,
    };

    // The point where the store's own backoff starts to bite.
    let engage = (store.cap_bytes as f64 / store.host_free_fraction) as u64;
    assert_eq!(
        mesh_agent_core::alerts::retro_hold(&idle(Some(engage)), store),
        Some(RetroHold::DiskPressure),
        "the scan is already standing down where the store starts backing off"
    );

    // And at the point the scan stands down, the store is still keeping
    // everything it was keeping before.
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

/// A scan waits for a quiet machine and never runs during maintenance: it is
/// answering a question about the past, which can always wait for the present to
/// calm down.
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

/// A rule asking about something finer than the stored minute is reported as one
/// history cannot answer, rather than answered wrongly at a coarser resolution.
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

/// A scan cannot spend a device's alert allowance just because what it found is
/// old. Twenty-five historical episodes are still twenty alerts an hour.
#[test]
fn a_scan_cannot_blow_the_device_alert_ceiling() {
    let dir = tempfile::tempdir().unwrap();
    // Twenty-five short episodes: two minutes over the line, two minutes under.
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

/// A hole in history breaks the run rather than being read through. The device
/// was switched off; nobody knows what the disk was doing, and a rule that needs
/// five continuous minutes has not seen them.
#[test]
fn a_gap_in_history_does_not_carry_a_breach_across_it() {
    let dir = tempfile::tempdir().unwrap();
    // Over the line the whole time, except for two minutes with no readings at
    // all in the middle of what would otherwise be a firing stretch.
    let db = seed(dir.path(), SERIES_DISK, 900, |i| {
        (!(180..300).contains(&i)).then_some(96.0)
    });
    let sink = roomy();
    let mut scan = RetroScan::new(
        RetroPlan::for_rule(&disk_critical()).unwrap(),
        RetroBudget::default(),
    );

    drain_scan(&mut scan, &db, &sink);

    // The first stretch is three minutes, the second ten: only the second one is
    // long enough, and it fires five minutes into itself rather than five
    // minutes into the pair.
    assert_eq!(event_times(&sink.drain()), vec![START + 600]);
}

/// A rule requiring two things at once reads both sides at the same minute. This
/// is the slow-disk-versus-busy-disk case: a queue 28 deep at a healthy 3 ms is
/// a nightly backup, and only the two readings together tell them apart.
#[test]
fn a_rule_with_two_sides_reads_both_at_the_same_minute() {
    let dir = tempfile::tempdir().unwrap();
    // Service time is bad throughout; the queue only backs up in the second half.
    let mut db = seed(dir.path(), SERIES_DISK_AWAIT_MS, 1_800, |_| Some(40.0));
    write_series(&mut db, SERIES_DISK_QUEUE_DEPTH, 1_800, |i| {
        Some(if i >= 900 { 28.0 } else { 1.0 })
    });

    let slow_and_backed_up = ThresholdRule {
        id: "disk-slow".to_string(),
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

/// The minute a rule reads is the minute its own question needs. A rule about
/// the peak reads the peak; a rule about the average reads the average — and a
/// machine that spikes for one second a minute is not a machine that is averaging
/// badly.
#[test]
fn a_peak_rule_and_an_average_rule_read_different_things_from_one_minute() {
    let dir = tempfile::tempdir().unwrap();
    // One second in every minute pinned at 99, the rest quiet at 10.
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
