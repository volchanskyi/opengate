//! The in-process alert sink bounds its queue and an hourly per-device ceiling, and counts
//! every alert either limit drops.

use mesh_agent_core::alerts::{AlertOrigin, AlertSeverity, AlertSink, EdgeAlert, PushOutcome};

const SECOND: i64 = 1_000_000;
const HOUR: i64 = 3_600 * SECOND;

fn alert(id: &str) -> EdgeAlert {
    EdgeAlert {
        rule_id: id.to_string(),
        rule_version: 1,
        severity: AlertSeverity::Warning,
        ts_micros: 0,
        window_start_micros: 0,
        window_end_micros: 0,
        metric: String::new(),
        value: None,
        subject: "kernel".to_string(),
        summary: "something happened".to_string(),
        evidence: Vec::new(),
        evidence_codec: String::new(),
        origin: AlertOrigin::Live,
    }
}

fn backfilled(id: &str) -> EdgeAlert {
    EdgeAlert {
        origin: AlertOrigin::Backfilled,
        ..alert(id)
    }
}

fn roomy() -> AlertSink {
    AlertSink::new(64, 20)
}

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

#[test]
fn the_ceiling_window_rolls() {
    let sink = AlertSink::new(64, 2);
    sink.push(alert("a"), 0);
    sink.push(alert("b"), SECOND);
    assert_eq!(
        sink.push(alert("c"), 2 * SECOND),
        PushOutcome::SuppressedByCeiling
    );

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

#[test]
fn a_sink_with_no_capacity_counts_everything_it_refuses() {
    let sink = AlertSink::new(0, 20);
    assert_eq!(sink.push(alert("a"), 0), PushOutcome::DroppedOldest);
    assert!(sink.drain().is_empty());
    assert_eq!(sink.stats().dropped_oldest, 1);
}

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

    let origins: Vec<AlertOrigin> = sink.drain().into_iter().map(|a| a.origin).collect();
    assert_eq!(origins, vec![AlertOrigin::Live, AlertOrigin::Backfilled]);
}

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

#[test]
fn a_ceiling_of_nothing_is_ignored() {
    let sink = AlertSink::new(64, 2);
    sink.set_ceiling(0);
    assert_eq!(sink.push(alert("a"), 0), PushOutcome::Queued);
}

#[test]
fn alerts_handed_back_after_a_failed_send_are_queued_again() {
    let sink = roomy();
    for id in ["first", "second", "third"] {
        sink.push(alert(id), 0);
    }

    let mut drained = sink.drain();
    let sent = drained.remove(0);
    assert_eq!(sent.rule_id, "first");
    sink.return_unsent(drained);

    let rule_ids: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(
        rule_ids,
        vec!["second".to_string(), "third".to_string()],
        "what did not go stays queued, in the order it was raised"
    );
}

#[test]
fn handing_an_alert_back_does_not_spend_the_allowance_twice() {
    let sink = AlertSink::new(64, 3);
    for id in ["a", "b", "c"] {
        sink.push(alert(id), 0);
    }
    assert_eq!(
        sink.push(alert("over"), 0),
        PushOutcome::SuppressedByCeiling,
        "three is the whole allowance"
    );

    sink.return_unsent(sink.drain());

    assert_eq!(sink.stats().queued, 3, "all three are held again");
    assert_eq!(
        sink.stats().suppressed_by_ceiling,
        1,
        "and the hand-back cost nothing further"
    );
}

#[test]
fn alerts_handed_back_go_in_front_of_what_arrived_meanwhile() {
    let sink = roomy();
    sink.push(alert("older"), 0);
    let unsent = sink.drain();
    sink.push(alert("newer"), 0);

    sink.return_unsent(unsent);

    let rule_ids: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(rule_ids, vec!["older".to_string(), "newer".to_string()]);
}

#[test]
fn a_hand_back_past_the_bound_still_gives_way_at_the_old_end() {
    let sink = AlertSink::new(2, 20);
    sink.push(alert("one"), 0);
    sink.push(alert("two"), 0);
    let unsent = sink.drain();
    sink.push(alert("three"), 0);
    sink.push(alert("four"), 0);

    sink.return_unsent(unsent);

    let rule_ids: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(
        rule_ids,
        vec!["three".to_string(), "four".to_string()],
        "what the machine is doing now outranks what it could not deliver"
    );
    assert_eq!(
        sink.stats().dropped_oldest,
        2,
        "and what the bound cost is counted rather than lost quietly"
    );
}
