//! The in-process alert sink: what every edge alert producer writes to while
//! the device is offline, and what it is allowed to lose on the way.
//!
//! Two limits meet here. The queue is bounded, because an agent offline for days
//! cannot hold an unbounded backlog; and a device may emit at most a fixed number
//! of alerts an hour, because one host in a loop must not drown its own fleet.
//! Both limits lose alerts by design, so the thing under test is not that they
//! lose them — it is that every loss is counted and reported, never silent.

use mesh_agent_core::alerts::{AlertOrigin, AlertSeverity, AlertSink, EdgeAlert, PushOutcome};

const SECOND: i64 = 1_000_000;
const HOUR: i64 = 3_600 * SECOND;

fn alert(id: &str) -> EdgeAlert {
    EdgeAlert {
        rule_id: id.to_string(),
        severity: AlertSeverity::Warning,
        ts_micros: 0,
        subject: "kernel".to_string(),
        summary: "something happened".to_string(),
        evidence: vec!["a redacted line".to_string()],
        origin: AlertOrigin::Live,
    }
}

/// The same alert, found by re-running a rule over history rather than as it
/// happened.
fn backfilled(id: &str) -> EdgeAlert {
    EdgeAlert {
        origin: AlertOrigin::Backfilled,
        ..alert(id)
    }
}

/// A sink with room for everything the test pushes, so the ceiling is the only
/// limit in play.
fn roomy() -> AlertSink {
    AlertSink::new(64, 20)
}

/// Alerts come back in the order they were raised: an incident reads forwards.
#[test]
fn alerts_drain_oldest_first() {
    let sink = roomy();
    for i in 0..3 {
        sink.push(alert(&format!("rule-{i}")), i * SECOND);
    }

    let drained: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(drained, vec!["rule-0", "rule-1", "rule-2"]);
    assert!(
        sink.drain().is_empty(),
        "a drained sink hands the same alert over only once"
    );
}

/// A full queue drops its **oldest** entry, not its newest. The newest alert is
/// the one describing what the device is doing now; dropping it to keep an alert
/// from three days ago would answer the wrong question on reconnect.
#[test]
fn a_full_queue_drops_the_oldest_and_counts_it() {
    let sink = AlertSink::new(3, 20);
    for i in 0..5 {
        sink.push(alert(&format!("rule-{i}")), i * SECOND);
    }

    let drained: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(
        drained,
        vec!["rule-2", "rule-3", "rule-4"],
        "the newest three survive"
    );
    assert_eq!(
        sink.stats().dropped_oldest,
        2,
        "both dropped alerts are counted"
    );
}

/// The drop count survives the drain that empties the queue, because the count
/// is what the next summary reports — a backlog that lost entries must say so
/// after it has been handed over, which is the only moment anyone can hear it.
#[test]
fn the_drop_count_survives_the_drain() {
    let sink = AlertSink::new(1, 20);
    sink.push(alert("a"), 0);
    sink.push(alert("b"), SECOND);

    assert_eq!(sink.drain().len(), 1);
    assert_eq!(
        sink.stats().dropped_oldest,
        1,
        "the loss is still reportable after the queue has been emptied"
    );
}

/// The per-device ceiling suppresses the excess of a storm and counts every
/// suppressed alert. A device in a loop is a device with one problem, and the
/// count is what says how loud it was.
#[test]
fn the_hourly_ceiling_suppresses_the_excess_with_a_count() {
    let sink = AlertSink::new(64, 3);
    let mut outcomes = Vec::new();
    for i in 0..5 {
        outcomes.push(sink.push(alert(&format!("rule-{i}")), i * SECOND));
    }

    assert_eq!(
        outcomes,
        vec![
            PushOutcome::Queued,
            PushOutcome::Queued,
            PushOutcome::Queued,
            PushOutcome::SuppressedByCeiling,
            PushOutcome::SuppressedByCeiling,
        ]
    );
    assert_eq!(
        sink.drain().len(),
        3,
        "only the alerts under the ceiling queue"
    );
    assert_eq!(sink.stats().suppressed_by_ceiling, 2);
}

/// The ceiling is an hour rolling, not an hour bucketed: once the earliest
/// alerts age past the hour, the device may raise alerts again. A bucketed
/// ceiling would let a device spend its whole allowance in the first minute and
/// go deaf for fifty-nine.
#[test]
fn the_ceiling_window_rolls() {
    let sink = AlertSink::new(64, 2);
    sink.push(alert("a"), 0);
    sink.push(alert("b"), SECOND);
    assert_eq!(
        sink.push(alert("c"), 2 * SECOND),
        PushOutcome::SuppressedByCeiling
    );

    // An hour and a moment after the first two, the allowance is free again.
    assert_eq!(
        sink.push(alert("d"), HOUR + 2 * SECOND),
        PushOutcome::Queued,
        "alerts older than the window no longer count against it"
    );
    assert_eq!(
        sink.stats().suppressed_by_ceiling,
        1,
        "the one suppression stays counted"
    );
}

/// A suppressed alert is not queued at all — the ceiling is about what leaves
/// the device, so an alert that never counted against the ceiling must not
/// reappear from the queue later.
#[test]
fn a_suppressed_alert_is_not_held_for_later() {
    let sink = AlertSink::new(64, 1);
    sink.push(alert("first"), 0);
    sink.push(alert("suppressed"), SECOND);

    let drained: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(drained, vec!["first"]);
    assert!(
        sink.drain().is_empty(),
        "the suppressed alert is gone, not deferred"
    );
}

/// Every producer holds a clone of the same sink, so a clone must be the same
/// sink and not a copy of it — otherwise the ceiling would be per producer and
/// a device with four producers could emit four times its allowance.
#[test]
fn clones_share_one_sink() {
    let sink = AlertSink::new(64, 20);
    let producer = sink.clone();

    producer.push(alert("from-the-clone"), 0);
    assert_eq!(
        sink.drain().len(),
        1,
        "an alert raised through a clone is in the same queue"
    );

    let ceiling = AlertSink::new(64, 1);
    let other = ceiling.clone();
    ceiling.push(alert("first"), 0);
    assert_eq!(
        other.push(alert("second"), SECOND),
        PushOutcome::SuppressedByCeiling,
        "the ceiling is per device, so clones share the allowance"
    );
}

/// The queued depth is readable without draining, so a summary can report a
/// backlog that has not been handed over yet.
#[test]
fn queued_depth_is_readable_without_draining() {
    let sink = roomy();
    assert_eq!(sink.stats().queued, 0);
    sink.push(alert("a"), 0);
    sink.push(alert("b"), SECOND);
    assert_eq!(sink.stats().queued, 2);
    sink.drain();
    assert_eq!(sink.stats().queued, 0);
}

/// A sink with no room at all holds nothing and says so, rather than panicking
/// or quietly behaving like a sink with room for one.
#[test]
fn a_sink_with_no_capacity_counts_everything_it_refuses() {
    let sink = AlertSink::new(0, 20);
    assert_eq!(sink.push(alert("a"), 0), PushOutcome::DroppedOldest);
    assert!(sink.drain().is_empty());
    assert_eq!(sink.stats().dropped_oldest, 1);
}

/// A finding out of history spends the same allowance as a live alert. A
/// retroactive scan of a rule the fleet just learned can match thousands of
/// minutes, and "but they already happened" is not a reason to let it past the
/// ceiling every other producer shares.
#[test]
fn a_backfilled_finding_spends_the_same_allowance_as_a_live_alert() {
    let sink = AlertSink::new(64, 2);

    assert_eq!(sink.push(alert("live"), 0), PushOutcome::Queued);
    assert_eq!(
        sink.push(backfilled("from-history"), SECOND),
        PushOutcome::Queued
    );
    assert_eq!(
        sink.push(backfilled("also-from-history"), 2 * SECOND),
        PushOutcome::SuppressedByCeiling,
        "a scan cannot raise past the ceiling a live producer already reached"
    );
    assert_eq!(sink.stats().suppressed_by_ceiling, 1);

    // The origin travels with the alert rather than being guessed from how old
    // its timestamp is.
    let origins: Vec<AlertOrigin> = sink.drain().into_iter().map(|a| a.origin).collect();
    assert_eq!(origins, vec![AlertOrigin::Live, AlertOrigin::Backfilled]);
}

/// The per-machine ceiling is the customer's, set on a screen and delivered with
/// the ruleset, so it has to be changeable on a sink that is already running.
/// The alternative is a number that only takes effect when the agent next
/// restarts, which is not a control anybody can use during an incident.
#[test]
fn the_ceiling_can_be_raised_while_the_sink_is_running() {
    let sink = AlertSink::new(64, 2);
    sink.push(alert("a"), 0);
    sink.push(alert("b"), SECOND);
    assert_eq!(
        sink.push(alert("c"), 2 * SECOND),
        PushOutcome::SuppressedByCeiling
    );

    sink.set_ceiling(4);
    assert_eq!(
        sink.push(alert("d"), 3 * SECOND),
        PushOutcome::Queued,
        "the raised allowance counts the alerts already admitted this hour"
    );
    assert_eq!(
        sink.push(alert("e"), 4 * SECOND),
        PushOutcome::Queued,
        "and the rest of the raised allowance is available too"
    );
    assert_eq!(
        sink.push(alert("f"), 5 * SECOND),
        PushOutcome::SuppressedByCeiling,
        "past the new ceiling it suppresses again"
    );

    assert_eq!(
        sink.stats().suppressed_by_ceiling,
        2,
        "what was lost before the raise stays counted"
    );
}

/// Lowering it takes effect on the next alert rather than on the next hour: a
/// customer who has just discovered a machine drowning them does not want the
/// change to land in fifty-nine minutes.
#[test]
fn the_ceiling_can_be_lowered_while_the_sink_is_running() {
    let sink = AlertSink::new(64, 20);
    for i in 0..3 {
        assert_eq!(
            sink.push(alert(&format!("rule-{i}")), i * SECOND),
            PushOutcome::Queued
        );
    }

    sink.set_ceiling(3);
    assert_eq!(
        sink.push(alert("over"), 4 * SECOND),
        PushOutcome::SuppressedByCeiling,
        "a machine already at the new ceiling is at it immediately"
    );
    assert_eq!(sink.stats().suppressed_by_ceiling, 1);
}

/// A ceiling of nothing would silence the machine entirely, which is never what
/// somebody means. The sink keeps the one it has.
#[test]
fn a_ceiling_of_nothing_is_ignored() {
    let sink = AlertSink::new(64, 2);
    sink.set_ceiling(0);
    assert_eq!(sink.push(alert("a"), 0), PushOutcome::Queued);
}
