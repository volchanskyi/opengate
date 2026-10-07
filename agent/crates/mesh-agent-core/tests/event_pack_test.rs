//! System-event rule pack tests: matching, near-miss refusal and dedup across overlapping polls.

use mesh_agent_core::alerts::{
    AlertSeverity, EdgeAlert, EventLevel, EventMatcher, EventPack, EventRule, HostEvent,
    ServiceErrorRule,
};
use mesh_protocol::{AlertEvidence, RuleCoverageState};

fn shipped_lines(alert: &EdgeAlert) -> Vec<String> {
    AlertEvidence::decode(&alert.evidence, &alert.evidence_codec)
        .expect("an alert's evidence must read back")
        .log_samples
}

const SECOND: i64 = 1_000_000;

const START: i64 = 1_000 * SECOND;

fn event<'a>(ts: i64, level: &'a str, unit: &'a str, message: &'a str) -> HostEvent<'a> {
    HostEvent {
        ts_micros: ts,
        level,
        unit,
        message,
    }
}

fn pack() -> EventPack {
    EventPack::new(
        EventRule::linux_pack(),
        ServiceErrorRule {
            threshold: 3,
            ..ServiceErrorRule::default()
        },
        START,
    )
}

fn rule_ids(alerts: &[mesh_agent_core::alerts::EdgeAlert]) -> Vec<String> {
    alerts.iter().map(|a| a.rule_id.clone()).collect()
}

type Record = (&'static str, &'static str);

type Case = (&'static str, Record, Record);

fn corpus() -> Vec<Case> {
    vec![
        (
            "linux-hung-task",
            (
                "ERROR",
                "INFO: task nfsd:1234 blocked for more than 120 seconds.",
            ),
            (
                "INFO",
                "INFO: task systemd:1 blocked for more than 120 seconds.",
            ),
        ),
        (
            "linux-oom-kill",
            (
                "ERROR",
                "Out of memory: Killed process 4242 (mysqld) total-vm:8192kB",
            ),
            (
                "WARN",
                "cache is out of memory budget, evicting cold entries",
            ),
        ),
        (
            "linux-ata-reset",
            (
                "ERROR",
                "ata3.00: exception Emask 0x0 SAct 0x0 SErr 0x0 action 0x6 frozen",
            ),
            (
                "INFO",
                "ata3: SATA link up 6.0 Gbps (SStatus 133 SControl 300)",
            ),
        ),
        (
            "linux-thermal-throttle",
            (
                "ERROR",
                "CPU2: Core temperature above threshold, cpu clock throttled (total events = 12)",
            ),
            ("INFO", "CPU2: Core temperature/speed normal"),
        ),
    ]
}

#[test]
fn each_rule_fires_once_for_its_own_record() {
    for (rule_id, (level, message), _) in corpus() {
        let mut pack = pack();
        let alerts = pack.poll(&[event(START + SECOND, level, "kernel", message)], false);
        assert_eq!(
            rule_ids(&alerts),
            vec![rule_id.to_string()],
            "{rule_id} must fire exactly once for its own record"
        );
        assert_eq!(
            shipped_lines(&alerts[0]).len(),
            1,
            "{rule_id} carries the record that fired it"
        );
        assert!(
            !alerts[0].summary.is_empty(),
            "{rule_id} says what it means in words"
        );
    }
}

#[test]
fn near_misses_fire_nothing() {
    for (rule_id, _, (level, message)) in corpus() {
        let mut pack = pack();
        let alerts = pack.poll(&[event(START + SECOND, level, "kernel", message)], false);
        assert!(
            alerts.is_empty(),
            "{rule_id}'s near-miss must fire nothing, fired {:?}",
            rule_ids(&alerts)
        );
    }
}

#[test]
fn the_whole_corpus_fires_each_rule_once_and_nothing_more() {
    let mut pack = pack();
    let mut records = Vec::new();
    let mut ts = START;
    let corpus = corpus();
    for (_, (level, message), (near_level, near_message)) in &corpus {
        ts += SECOND;
        records.push(event(ts, level, "kernel", message));
        ts += SECOND;
        records.push(event(ts, near_level, "kernel", near_message));
    }

    let mut fired = rule_ids(&pack.poll(&records, false));
    fired.sort();
    let mut expected: Vec<String> = corpus.iter().map(|(id, _, _)| (*id).to_string()).collect();
    expected.sort();
    assert_eq!(
        fired, expected,
        "each rule fires once, and only its own record"
    );
}

#[test]
fn a_record_re_presented_by_an_overlapping_poll_fires_once() {
    let mut pack = pack();
    let record = event(
        START + SECOND,
        "ERROR",
        "kernel",
        "Out of memory: Killed process 4242 (mysqld) total-vm:8192kB",
    );

    assert_eq!(
        rule_ids(&pack.poll(std::slice::from_ref(&record), false)),
        vec!["linux-oom-kill".to_string()],
        "the first sight of the record fires"
    );
    assert!(
        pack.poll(std::slice::from_ref(&record), false).is_empty(),
        "the same record on an overlapping poll fires nothing"
    );
    assert!(
        pack.poll(std::slice::from_ref(&record), false).is_empty(),
        "and keeps firing nothing however often it is re-presented"
    );
}

#[test]
fn records_sharing_the_cursor_instant_each_fire_once() {
    let mut pack = pack();
    let at = START + SECOND;
    let records = vec![
        event(
            at,
            "ERROR",
            "kernel",
            "Out of memory: Killed process 1 (a) total-vm:1kB",
        ),
        event(
            at,
            "ERROR",
            "kernel",
            "Out of memory: Killed process 2 (b) total-vm:1kB",
        ),
    ];

    assert_eq!(
        pack.poll(&records, false).len(),
        2,
        "two distinct records at the same instant both fire"
    );
    assert!(
        pack.poll(&records, false).is_empty(),
        "and neither fires again on the overlapping poll"
    );
}

#[test]
fn a_record_behind_the_cursor_never_fires() {
    let mut pack = pack();
    let late = event(
        START + SECOND,
        "ERROR",
        "kernel",
        "ata3.00: exception Emask 0x0 SAct 0x0 SErr 0x0 action 0x6 frozen",
    );
    let newer = event(
        START + 10 * SECOND,
        "ERROR",
        "kernel",
        "Out of memory: Killed process 9 (z) total-vm:1kB",
    );

    assert_eq!(
        pack.poll(&[newer], false).len(),
        1,
        "the newer record fires"
    );
    assert!(
        pack.poll(&[late], false).is_empty(),
        "a record older than the cursor fires not at all, even unseen"
    );
}

#[test]
fn records_older_than_the_start_of_the_watch_never_fire() {
    let mut pack = pack();
    let alerts = pack.poll(
        &[event(
            START - SECOND,
            "ERROR",
            "kernel",
            "Out of memory: Killed process 1 (old) total-vm:1kB",
        )],
        false,
    );
    assert!(
        alerts.is_empty(),
        "a record from before the watch began is history, not news"
    );
}

#[test]
fn a_saturated_poll_is_counted_and_still_fires_what_it_saw() {
    let mut pack = pack();
    assert_eq!(pack.saturated_polls(), 0);

    let alerts = pack.poll(
        &[event(
            START + SECOND,
            "ERROR",
            "kernel",
            "Out of memory: Killed process 4242 (mysqld) total-vm:8192kB",
        )],
        true,
    );
    assert_eq!(alerts.len(), 1, "a saturated poll still fires what it saw");
    assert_eq!(
        pack.saturated_polls(),
        1,
        "the poll that may have lost records is counted"
    );

    pack.poll(&[], false);
    assert_eq!(
        pack.saturated_polls(),
        1,
        "an unsaturated poll adds nothing to the count"
    );
}

#[test]
fn repeated_service_errors_fire_once_on_crossing() {
    let mut pack = pack();
    let mut alerts = Vec::new();
    for i in 1..=3 {
        alerts.extend(pack.poll(
            &[event(
                START + i * SECOND,
                "ERROR",
                "nginx.service",
                "upstream timed out",
            )],
            false,
        ));
    }
    assert_eq!(
        rule_ids(&alerts),
        vec!["linux-service-errors".to_string()],
        "the third error inside the window fires once"
    );
    assert_eq!(
        alerts[0].subject, "nginx.service",
        "the alert names the service"
    );

    let more = pack.poll(
        &[event(
            START + 4 * SECOND,
            "ERROR",
            "nginx.service",
            "upstream timed out",
        )],
        false,
    );
    assert!(
        more.is_empty(),
        "a fourth error does not fire a second alert while the service is already over"
    );
}

#[test]
fn errors_ageing_out_of_the_window_lower_the_count() {
    let day = 24 * 60 * 60 * SECOND;
    let mut pack = pack();

    for i in 0..6 {
        // A spacing just over twelve hours puts any three errors wider than the 24 h window.
        let alerts = pack.poll(
            &[event(
                START + SECOND + i * (day / 2 + SECOND),
                "ERROR",
                "nginx.service",
                "upstream timed out",
            )],
            false,
        );
        assert!(
            alerts.is_empty(),
            "a trickle slower than the window must never fire (error {i})"
        );
    }
}

#[test]
fn services_are_counted_separately() {
    let mut pack = pack();
    let mut alerts = Vec::new();
    for i in 1..=2 {
        for unit in ["nginx.service", "postgresql.service"] {
            alerts.extend(pack.poll(&[event(START + i * SECOND, "ERROR", unit, "failed")], false));
        }
    }
    assert!(
        alerts.is_empty(),
        "two errors each from two services is below the threshold for both"
    );

    let third = pack.poll(
        &[event(
            START + 3 * SECOND,
            "ERROR",
            "nginx.service",
            "failed",
        )],
        false,
    );
    assert_eq!(third.len(), 1, "only the service that crossed fires");
    assert_eq!(third[0].subject, "nginx.service");
}

#[test]
fn the_tracked_service_set_is_capped_and_the_overflow_counted() {
    let mut pack = EventPack::new(
        EventRule::linux_pack(),
        ServiceErrorRule {
            threshold: 3,
            max_services: 2,
            ..ServiceErrorRule::default()
        },
        START,
    );

    for (i, unit) in ["a.service", "b.service", "c.service"].iter().enumerate() {
        let ts = START + (i as i64 + 1) * SECOND;
        pack.poll(&[event(ts, "ERROR", unit, "failed")], false);
    }
    assert_eq!(
        pack.untracked_services(),
        1,
        "the service the cap turned away is counted, never silently dropped"
    );
}

#[test]
fn warnings_do_not_feed_the_error_counter() {
    let mut pack = pack();
    let mut alerts = Vec::new();
    for i in 1..=5 {
        alerts.extend(pack.poll(
            &[event(
                START + i * SECOND,
                "WARN",
                "nginx.service",
                "slow upstream",
            )],
            false,
        ));
    }
    assert!(alerts.is_empty(), "warnings are not errors");
}

#[test]
fn records_without_a_service_do_not_feed_the_counter() {
    let mut pack = pack();
    let mut alerts = Vec::new();
    for i in 1..=5 {
        alerts.extend(pack.poll(
            &[event(START + i * SECOND, "ERROR", "", "some failure")],
            false,
        ));
    }
    assert!(
        alerts.is_empty(),
        "records with no service attributed to them feed no service's count"
    );
}

#[test]
fn skipping_a_maintenance_window_fires_nothing_from_it() {
    let mut pack = pack();
    let during = START + 5 * SECOND;

    pack.skip_to(START + 10 * SECOND);

    let alerts = pack.poll(
        &[event(
            during,
            "ERROR",
            "kernel",
            "Out of memory: Killed process 4242 (mysqld) total-vm:8192kB",
        )],
        false,
    );
    assert!(
        alerts.is_empty(),
        "records from inside a skipped window never fire, before or after it ends"
    );

    let after = pack.poll(
        &[event(
            START + 11 * SECOND,
            "ERROR",
            "kernel",
            "Out of memory: Killed process 7 (later) total-vm:1kB",
        )],
        false,
    );
    assert_eq!(after.len(), 1, "the watch resumes after the skipped window");
}

#[test]
fn alert_evidence_is_redacted() {
    let mut pack = pack();
    let secrets = [
        "AKIAIOSFODNN7EXAMPLE",
        "hunter2secret",
        "eyJhbGciOiJIUzI1NiJ9.payload.sig",
    ];
    let message = "Out of memory: Killed process 4242 (mysqld) password=hunter2secret \
         aws_key AKIAIOSFODNN7EXAMPLE Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig \
         db=postgres://user:pw@host/db";

    let alerts = pack.poll(&[event(START + SECOND, "ERROR", "kernel", message)], false);
    assert_eq!(alerts.len(), 1);
    let evidence = shipped_lines(&alerts[0]).join(" ");
    for secret in secrets {
        assert!(
            !evidence.contains(secret),
            "{secret} must not survive into an alert: {evidence}"
        );
    }
    assert!(
        !evidence.contains("user:pw@host"),
        "credentials inside a connection string must not survive: {evidence}"
    );
    assert!(
        evidence.contains("Killed process"),
        "redaction must leave the record legible: {evidence}"
    );
}

#[test]
fn matcher_honours_level_floor_alternatives_and_exclusions() {
    let matcher = EventMatcher {
        any_of: vec!["hard resetting link".into(), "exception emask".into()],
        none_of: vec!["link up".into()],
        min_level: EventLevel::Warn,
    };

    assert!(matcher.matches("WARN", "ata1: hard resetting link"));
    assert!(
        matcher.matches("ERROR", "ata1.00: exception Emask 0x0"),
        "any one alternative is enough, and matching ignores case"
    );
    assert!(
        !matcher.matches("INFO", "ata1: hard resetting link"),
        "a record below the level floor does not match"
    );
    assert!(
        !matcher.matches("ERROR", "ata1: hard resetting link after link up"),
        "an exclusion vetoes an otherwise matching record"
    );
    assert!(!matcher.matches("ERROR", "ata1: nothing to see"));
}

#[test]
fn the_pack_states_the_lowest_level_any_rule_can_act_on() {
    let pack = EventPack::new(EventRule::linux_pack(), ServiceErrorRule::default(), START);
    assert_eq!(
        pack.min_level(),
        EventLevel::Error,
        "every shipped rule acts on errors alone"
    );

    let lenient = EventPack::new(
        vec![EventRule {
            rule_id: "test-warn".into(),
            version: 1,
            severity: AlertSeverity::Info,
            summary: "watches warnings".into(),
            matcher: EventMatcher {
                any_of: vec!["anything".into()],
                none_of: Vec::new(),
                min_level: EventLevel::Warn,
            },
        }],
        ServiceErrorRule::default(),
        START,
    );
    assert_eq!(
        lenient.min_level(),
        EventLevel::Warn,
        "a rule that watches warnings lowers what the reader must fetch"
    );

    let empty = EventPack::new(Vec::new(), ServiceErrorRule::default(), START);
    assert_eq!(
        empty.min_level(),
        EventLevel::Error,
        "with no per-record rules at all, the service counter still needs errors"
    );
}

#[test]
fn the_linux_pack_is_four_distinctly_identified_rules() {
    let pack = EventRule::linux_pack();
    assert_eq!(pack.len(), 4);

    let mut ids: Vec<&str> = pack.iter().map(|r| r.rule_id.as_str()).collect();
    ids.sort();
    assert_eq!(
        ids,
        vec![
            "linux-ata-reset",
            "linux-hung-task",
            "linux-oom-kill",
            "linux-thermal-throttle"
        ]
    );

    for rule in &pack {
        assert!(
            !rule.summary.is_empty(),
            "{} says what it means",
            rule.rule_id
        );
        assert!(
            matches!(
                rule.severity,
                AlertSeverity::Warning | AlertSeverity::Critical
            ),
            "{} is not merely informational",
            rule.rule_id
        );
    }
}

#[test]
fn a_machine_that_can_read_its_log_reports_every_rule_as_watching() {
    let reported =
        EventPack::coverage(&EventRule::linux_pack(), &ServiceErrorRule::default(), true);

    let ids: Vec<&str> = reported.iter().map(|c| c.rule_id.as_str()).collect();
    assert!(ids.contains(&"linux-oom-kill"));
    assert!(ids.contains(&"linux-service-errors"));
    assert_eq!(
        reported.len(),
        EventRule::linux_pack().len() + 1,
        "every curated rule reports, and so does the repeated-error count"
    );
    assert!(
        reported
            .iter()
            .all(|c| c.state == RuleCoverageState::Active),
        "a machine that can read its own log is watching for all of them"
    );
}

#[test]
fn a_machine_that_cannot_read_its_log_says_so_rather_than_going_silent() {
    let reported = EventPack::coverage(
        &EventRule::linux_pack(),
        &ServiceErrorRule::default(),
        false,
    );

    assert_eq!(reported.len(), EventRule::linux_pack().len() + 1);
    assert!(
        reported
            .iter()
            .all(|c| c.state == RuleCoverageState::Unsupported),
        "claiming a rule watches a machine it produces nothing for is the failure coverage prevents"
    );
}
