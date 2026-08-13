//! System-event rule pack: what the curated rules match, what they refuse, and
//! what a bounded poll with an overlapping window is allowed to fire twice.
//!
//! The host log reader is a bounded on-demand read, not a stream, so successive
//! polls overlap and re-present the same records. Every test here is written
//! against that fact: matching is only half the pack, and the half that decides
//! whether an operator trusts it is what happens on the second look at the same
//! record.

use mesh_agent_core::alerts::{
    AlertSeverity, EventLevel, EventMatcher, EventPack, EventRule, HostEvent, ServiceErrorRule,
};

/// One second in the microsecond scale the pack orders records on.
const SECOND: i64 = 1_000_000;

/// The pack starts watching at this instant; anything older than it belongs to
/// the log's history rather than to this process's watch.
const START: i64 = 1_000 * SECOND;

fn event<'a>(ts: i64, level: &'a str, unit: &'a str, message: &'a str) -> HostEvent<'a> {
    HostEvent {
        ts_micros: ts,
        level,
        unit,
        message,
    }
}

/// The shipped pack with the rolling per-service counter turned down to three
/// errors so a test corpus stays readable. The window stays at its real 24 h.
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

/// One record as a reader hands it over: its level and its text.
type Record = (&'static str, &'static str);

/// A rule, the record that must fire it, and the near-miss that must not.
type Case = (&'static str, Record, Record);

/// A matching record for each rule in the pack, paired with the near-miss that
/// must not fire it. The near-miss is the half that earns the pack its keep: a
/// rule matching on a substring alone looks perfectly green until the day a
/// recovery message pages someone at 03:00.
///
/// Each pair is `(rule id, matching record, near-miss record)`, both at the
/// level the kernel actually emits them at.
fn corpus() -> Vec<Case> {
    vec![
        (
            "linux.hung_task",
            (
                "ERROR",
                "INFO: task nfsd:1234 blocked for more than 120 seconds.",
            ),
            // The same shape the kernel prints for information rather than for a
            // stall. Substring matching alone cannot tell these apart.
            (
                "INFO",
                "INFO: task systemd:1 blocked for more than 120 seconds.",
            ),
        ),
        (
            "linux.oom_kill",
            (
                "ERROR",
                "Out of memory: Killed process 4242 (mysqld) total-vm:8192kB",
            ),
            // An application complaining about its own memory budget is not the
            // kernel reclaiming memory by killing something.
            (
                "WARN",
                "cache is out of memory budget, evicting cold entries",
            ),
        ),
        (
            "linux.ata_reset",
            (
                "ERROR",
                "ata3.00: exception Emask 0x0 SAct 0x0 SErr 0x0 action 0x6 frozen",
            ),
            // A link coming up is the healthy half of the same subsystem.
            (
                "INFO",
                "ata3: SATA link up 6.0 Gbps (SStatus 133 SControl 300)",
            ),
        ),
        (
            "linux.thermal_throttle",
            (
                "ERROR",
                "CPU2: Core temperature above threshold, cpu clock throttled (total events = 12)",
            ),
            // The recovery message names the same subsystem and the same core.
            ("INFO", "CPU2: Core temperature/speed normal"),
        ),
    ]
}

/// Every rule fires exactly once for one matching record.
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
            alerts[0].evidence.len(),
            1,
            "{rule_id} carries the record that fired it"
        );
        assert!(
            !alerts[0].summary.is_empty(),
            "{rule_id} says what it means in words"
        );
    }
}

/// The negative half: a near-miss per rule fires nothing at all. Not "fires
/// something else" — nothing.
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

/// The whole corpus at once fires each rule once and nothing else — a rule
/// whose matcher is loose enough to catch a neighbour's record shows up here
/// even when each rule looks correct in isolation.
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

/// Two overlapping polls present the same record twice. It fires once.
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
        vec!["linux.oom_kill".to_string()],
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

/// Several records share the boundary instant the cursor lands on. All of them
/// fire on first sight, and none of them fires again — a cursor that dedups by
/// timestamp alone would swallow every record after the first.
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

/// Once the cursor has advanced past a record, that record fires not at all —
/// including a record the pack never saw, which is what a poll that lost the
/// oldest end of its window looks like on the next look.
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

/// History predating the watch is not this process's to fire. An agent that
/// started a minute ago must not page anyone for yesterday's OOM kill just
/// because the reader's window reaches back past its own start.
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

/// A poll that came back at the reader's line cap saw only the newest end of
/// its window. How many records fell off the old end is unknowable, so the pack
/// counts the poll rather than inventing a number for it — and the records it
/// did get still fire.
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

/// The rolling counter fires once when a service crosses the threshold inside
/// the window, and does not fire again while it stays above it.
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
        vec!["linux.service_errors".to_string()],
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

/// The window slides: errors ageing out of it lower the count, so a service
/// that trickles errors slower than the window never fires.
#[test]
fn errors_ageing_out_of_the_window_lower_the_count() {
    let day = 24 * 60 * 60 * SECOND;
    let mut pack = pack();

    for i in 0..6 {
        // A shade over twelve hours apart, so any three of them span more than
        // the window and the oldest has always aged out by the time the newest
        // arrives. Exactly twelve would not do: three errors twenty-four hours
        // apart end to end are three errors inside a twenty-four-hour window.
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

/// Services are counted separately: two services with two errors each is not
/// one service with four.
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

/// The tracked service set is capped, and a service the cap turned away is
/// counted rather than silently untracked.
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

/// Only errors feed the counter. A service logging warnings all day is not a
/// service failing all day.
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

/// Kernel records carry no service, so they cannot be attributed to one. They
/// must not all pile into a single unnamed bucket that then fires as though one
/// service were failing.
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

/// Maintenance mode suppresses the window rather than deferring it. An admin
/// rebooting a host at 02:00 produces exactly the records this pack matches;
/// holding them until maintenance ends would page someone for the maintenance
/// itself, which is what maintenance mode exists to prevent.
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

/// Every log-derived field an alert carries is redacted before the alert
/// exists. The corpus is hostile on purpose: each shape here is one the edge
/// redactor is expected to catch, and an alert is the one path that lifts a raw
/// log line off the host outside the Logs pane.
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
    let evidence = alerts[0].evidence.join(" ");
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

/// The matcher's own decisions, without the pack around it: a level floor, the
/// alternatives, and the exclusions.
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

/// The pack states the least severe record any of its rules could act on, so
/// the reader can bound what it reads from the rules themselves. A hardcoded
/// floor at the call site would be a trap: the first rule added with a lower
/// one would match nothing, and match nothing silently.
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
            rule_id: "test.warn".into(),
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

/// The shipped pack is the four Linux rules, each with a distinct id and a
/// severity that says how bad it is. A duplicate id would make one rule
/// unreachable through a binding.
#[test]
fn the_linux_pack_is_four_distinctly_identified_rules() {
    let pack = EventRule::linux_pack();
    assert_eq!(pack.len(), 4);

    let mut ids: Vec<&str> = pack.iter().map(|r| r.rule_id.as_str()).collect();
    ids.sort();
    assert_eq!(
        ids,
        vec![
            "linux.ata_reset",
            "linux.hung_task",
            "linux.oom_kill",
            "linux.thermal_throttle"
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
