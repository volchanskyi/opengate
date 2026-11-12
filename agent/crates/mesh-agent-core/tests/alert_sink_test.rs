//! The in-process alert sink bounds its queue and an hourly per-device ceiling, and counts
//! every alert either limit drops.

use mesh_agent_core::alerts::{AlertOrigin, AlertSeverity, AlertSink, EdgeAlert, PushOutcome};
use mesh_protocol::AlertAnswerReason;

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
fn alerts_left_unanswered_are_queued_again() {
    let sink = roomy();
    for id in ["first", "second", "third"] {
        sink.push(alert(id), 0);
    }

    let offers = sink.offer();
    assert_eq!(offers[0].alert.rule_id, "first");
    sink.answer(&offers[0].id, AlertAnswerReason::Stored);
    sink.reoffer_awaiting();

    let rule_ids: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(
        rule_ids,
        vec!["second".to_string(), "third".to_string()],
        "what was not answered stays queued, in the order it was raised"
    );
}

#[test]
fn offering_an_alert_again_does_not_spend_the_allowance_twice() {
    let sink = AlertSink::new(64, 3);
    for id in ["a", "b", "c"] {
        sink.push(alert(id), 0);
    }
    assert_eq!(
        sink.push(alert("over"), 0),
        PushOutcome::SuppressedByCeiling,
        "three is the whole allowance"
    );

    let _offers = sink.offer();
    sink.reoffer_awaiting();

    assert_eq!(sink.stats().queued, 3, "all three are held again");
    assert_eq!(
        sink.stats().suppressed_by_ceiling,
        1,
        "and the offer again cost nothing further"
    );
}

#[test]
fn alerts_offered_again_go_in_front_of_what_arrived_meanwhile() {
    let sink = roomy();
    sink.push(alert("older"), 0);
    let _offers = sink.offer();
    sink.push(alert("newer"), 0);

    sink.reoffer_awaiting();

    let rule_ids: Vec<String> = sink.drain().into_iter().map(|a| a.rule_id).collect();
    assert_eq!(rule_ids, vec!["older".to_string(), "newer".to_string()]);
}

#[test]
fn unanswered_alerts_past_the_bound_give_way_at_the_old_end() {
    let sink = AlertSink::new(2, 20);
    sink.push(alert("one"), 0);
    sink.push(alert("two"), 0);
    let _offers = sink.offer();
    sink.push(alert("three"), 0);
    sink.push(alert("four"), 0);

    sink.reoffer_awaiting();

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

fn rule_ids(offers: &[mesh_agent_core::alerts::Offer]) -> Vec<String> {
    offers.iter().map(|o| o.alert.rule_id.clone()).collect()
}

#[test]
fn an_offered_alert_waits_for_its_answer_under_a_fresh_id() {
    let sink = roomy();
    sink.push(alert("disk-critical"), 0);
    sink.push(alert("cpu-saturated"), SECOND);

    let offers = sink.offer();
    assert_eq!(rule_ids(&offers), vec!["disk-critical", "cpu-saturated"]);
    assert_ne!(
        offers[0].id, offers[1].id,
        "each offer has an id of its own"
    );
    assert!(
        sink.offer().is_empty(),
        "an offered alert is not offered twice"
    );
    let stats = sink.stats();
    assert_eq!(stats.queued, 0);
    assert_eq!(stats.awaiting, 2, "both wait for an answer");
}

#[test]
fn stored_and_refused_for_good_end_an_alert() {
    let sink = roomy();
    sink.push(alert("disk-critical"), 0);
    sink.push(alert("bad-rule"), SECOND);
    let offers = sink.offer();

    sink.answer(&offers[0].id, AlertAnswerReason::Stored);
    sink.answer(&offers[1].id, AlertAnswerReason::RefusedForGood);

    assert_eq!(sink.stats().awaiting, 0);
    sink.reoffer_awaiting();
    assert!(
        sink.offer().is_empty(),
        "an answered alert is never offered again"
    );
}

#[test]
fn try_again_puts_the_alert_back_first_in_line() {
    let sink = roomy();
    sink.push(alert("disk-critical"), 0);
    let first = sink.offer();
    sink.push(alert("cpu-saturated"), SECOND);

    sink.answer(&first[0].id, AlertAnswerReason::TryAgain);

    assert_eq!(
        rule_ids(&sink.offer()),
        vec!["disk-critical", "cpu-saturated"],
        "the alert to try again goes out before the ones raised after it"
    );
}

#[test]
fn a_new_session_offers_every_unanswered_alert_again_oldest_first() {
    let sink = roomy();
    sink.push(alert("disk-critical"), 0);
    sink.push(alert("cpu-saturated"), SECOND);
    let offered = sink.offer();
    sink.push(alert("memory-low"), 2 * SECOND);

    sink.reoffer_awaiting();

    let again = sink.offer();
    assert_eq!(
        rule_ids(&again),
        vec!["disk-critical", "cpu-saturated", "memory-low"]
    );
    assert!(
        again.iter().all(|o| offered.iter().all(|p| p.id != o.id)),
        "an alert offered again carries a new id, so a late answer to the old one changes nothing"
    );
}

#[test]
fn an_answer_for_an_id_the_sink_does_not_hold_changes_nothing() {
    let sink = roomy();
    sink.push(alert("disk-critical"), 0);
    let offers = sink.offer();

    sink.answer("an-id-from-another-session", AlertAnswerReason::Stored);
    sink.answer("an-id-from-another-session", AlertAnswerReason::TryAgain);

    assert_eq!(sink.stats().awaiting, 1);
    assert_eq!(sink.stats().queued, 0);
    sink.answer(&offers[0].id, AlertAnswerReason::Stored);
    assert_eq!(sink.stats().awaiting, 0);
}

#[test]
fn alerts_awaiting_an_answer_count_against_the_capacity() {
    let sink = AlertSink::new(3, 20);
    sink.push(alert("rule-0"), 0);
    sink.push(alert("rule-1"), SECOND);
    let _offered = sink.offer();
    for i in 2..5 {
        sink.push(alert(&format!("rule-{i}")), i * SECOND);
    }

    let stats = sink.stats();
    assert_eq!(
        stats.queued + stats.awaiting,
        3,
        "the sink holds at most its capacity"
    );
    assert_eq!(
        stats.dropped_oldest, 2,
        "the two oldest are dropped and counted"
    );
    sink.reoffer_awaiting();
    assert_eq!(rule_ids(&sink.offer()), vec!["rule-2", "rule-3", "rule-4"]);
}
