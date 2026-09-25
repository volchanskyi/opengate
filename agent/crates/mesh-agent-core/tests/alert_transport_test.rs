//! Turning an alert this machine raised into the message the server admits.
//!
//! The server refuses an alert it cannot identify, cannot place in time, or
//! cannot read the evidence of, and it counts every refusal under its own
//! reason. A machine whose alerts are all refused is indistinguishable from a
//! machine that raised none — so what is under test here is that every field
//! the far end requires is present and means what that end reads it as.

use mesh_agent_core::alerts::{
    alert_message, AlertOrigin, AlertSeverity, EdgeAlert, EVIDENCE_CODEC,
};
use mesh_protocol::{AlertSeverity as WireSeverity, ControlMessage};

const SECOND: i64 = 1_000_000;

/// Whole seconds, so nothing here depends on how a fraction rounds.
const FIRED_AT: i64 = 1_763_000_000;

/// A live firing: the reading held over the line for five minutes and then the
/// rule fired.
fn live() -> EdgeAlert {
    EdgeAlert {
        rule_id: "disk-critical".to_string(),
        rule_version: 3,
        severity: AlertSeverity::Critical,
        ts_micros: FIRED_AT * SECOND,
        window_start_micros: (FIRED_AT - 300) * SECOND,
        window_end_micros: FIRED_AT * SECOND,
        metric: "disk.used_percent".to_string(),
        value: Some(91.4),
        subject: "/data".to_string(),
        summary: "a disk is nearly full".to_string(),
        evidence: vec![0x01, 0x02, 0x03],
        evidence_codec: EVIDENCE_CODEC.to_string(),
        origin: AlertOrigin::Live,
    }
}

/// Pulls the alert fields out of the message, or fails loudly: every case below
/// is about what one `AgentAlert` carries.
fn sent(alert: &EdgeAlert) -> ControlMessage {
    let msg = alert_message(alert);
    assert!(
        matches!(msg, ControlMessage::AgentAlert { .. }),
        "an alert must travel as an AgentAlert and nothing else"
    );
    msg
}

/// The identity the server deduplicates on is (machine, rule, revision, window
/// start). The machine half of that is the connection; the other three have to
/// be on the message, and a reconnect replaying the same alert has to produce
/// the same three or the replay inserts a second row.
#[test]
fn an_alert_carries_the_identity_the_server_resolves_it_by() {
    let alert = live();
    let ControlMessage::AgentAlert {
        alert_id,
        rule_id,
        rule_version,
        window_start_ts,
        ..
    } = sent(&alert)
    else {
        unreachable!()
    };

    assert_eq!(rule_id, "disk-critical");
    assert_eq!(rule_version, 3, "a revision of nothing is refused outright");
    assert_eq!(
        window_start_ts,
        FIRED_AT - 300,
        "the window start is part of the identity, so it is the rule's own window"
    );
    assert!(
        !alert_id.is_empty(),
        "the machine names its own report so one can be traced end to end"
    );
}

/// Replaying the same raised alert produces the same identity. The holding area
/// hands the same value back after a failed send, and the server's duplicate
/// check is what makes that safe — but only while the three fields do not move.
#[test]
fn the_same_alert_sent_twice_resolves_to_one_row() {
    let alert = live();
    let (first, second) = (sent(&alert), sent(&alert));

    let identity = |msg: ControlMessage| match msg {
        ControlMessage::AgentAlert {
            rule_id,
            rule_version,
            window_start_ts,
            ..
        } => (rule_id, rule_version, window_start_ts),
        _ => unreachable!(),
    };
    assert_eq!(identity(first), identity(second));
}

/// The window runs forwards and both ends are stated. A window whose end
/// precedes its start describes no interval and is refused; so is one whose
/// ends are nothing.
#[test]
fn the_window_runs_forwards_and_both_ends_are_stated() {
    let ControlMessage::AgentAlert {
        window_start_ts,
        window_end_ts,
        observed_ts,
        ..
    } = sent(&live())
    else {
        unreachable!()
    };

    assert!(
        window_start_ts > 0,
        "a window starting at nothing is refused"
    );
    assert!(
        window_end_ts >= window_start_ts,
        "a window that ends before it starts describes no interval"
    );
    assert!(observed_ts > 0, "an alert nobody saw is refused");
}

/// An event with no duration still has a window: the instant it happened, at
/// both ends. A log record is one moment, not a stretch.
#[test]
fn an_event_with_no_duration_still_states_a_window() {
    let alert = EdgeAlert {
        rule_id: "linux-oom-kill".to_string(),
        metric: String::new(),
        value: None,
        window_start_micros: FIRED_AT * SECOND,
        window_end_micros: FIRED_AT * SECOND,
        ..live()
    };
    let ControlMessage::AgentAlert {
        window_start_ts,
        window_end_ts,
        metric,
        value,
        ..
    } = sent(&alert)
    else {
        unreachable!()
    };

    assert_eq!(window_start_ts, FIRED_AT);
    assert_eq!(window_end_ts, FIRED_AT);
    assert!(
        metric.is_empty(),
        "a rule watching words watches no reading"
    );
    assert!(
        value.is_none(),
        "and so it has no value that crossed a line"
    );
}

/// A finding out of history says so, and is stamped with the minute it
/// happened. The far end widens its clock window for exactly this, and sorts
/// the incident by that time — a freeze from three weeks ago that arrived
/// stamped today would sort as today's problem.
#[test]
fn a_finding_out_of_history_is_stamped_when_it_happened() {
    let three_weeks = 21 * 24 * 3600;
    let happened = FIRED_AT - three_weeks;
    let alert = EdgeAlert {
        origin: AlertOrigin::Backfilled,
        ts_micros: happened * SECOND,
        window_start_micros: (happened - 300) * SECOND,
        window_end_micros: happened * SECOND,
        ..live()
    };

    let ControlMessage::AgentAlert {
        backfilled,
        observed_ts,
        window_end_ts,
        ..
    } = sent(&alert)
    else {
        unreachable!()
    };

    assert!(backfilled, "a finding out of history says which it is");
    assert_eq!(
        observed_ts, happened,
        "stamped with the minute it happened, not the minute it was found"
    );
    assert_eq!(window_end_ts, happened);
}

/// Severity is always stated, and all three travel. An absent severity reads as
/// a broken sender at the far end rather than as a quiet machine, and reading a
/// critical alert as the mildest of the three would file it where nobody looks.
#[test]
fn every_severity_travels_as_itself() {
    for (raised, expected) in [
        (AlertSeverity::Info, WireSeverity::Info),
        (AlertSeverity::Warning, WireSeverity::Warning),
        (AlertSeverity::Critical, WireSeverity::Critical),
    ] {
        let alert = EdgeAlert {
            severity: raised,
            ..live()
        };
        let ControlMessage::AgentAlert { severity, .. } = sent(&alert) else {
            unreachable!()
        };
        assert_eq!(
            severity, expected,
            "{raised:?} must not arrive as anything else"
        );
    }
}

/// Evidence names the codec that produced it. The far end refuses a blob under
/// a codec it cannot read rather than storing something unreadable beside an
/// alert, so an unnamed codec costs the alert everything behind it.
#[test]
fn evidence_names_the_codec_that_packed_it() {
    let ControlMessage::AgentAlert {
        evidence,
        evidence_codec,
        ..
    } = sent(&live())
    else {
        unreachable!()
    };

    assert_eq!(evidence, vec![0x01, 0x02, 0x03]);
    assert_eq!(evidence_codec, EVIDENCE_CODEC);
}

/// A machine that had nothing to attach still says it is in trouble. Evidence
/// is optional, and an empty blob names no codec — a codec on nothing would
/// read as evidence that exists.
#[test]
fn an_alert_with_nothing_behind_it_still_travels() {
    let alert = EdgeAlert {
        evidence: Vec::new(),
        evidence_codec: String::new(),
        ..live()
    };
    let ControlMessage::AgentAlert {
        evidence,
        evidence_codec,
        rule_id,
        ..
    } = sent(&alert)
    else {
        unreachable!()
    };

    assert!(evidence.is_empty());
    assert!(
        evidence_codec.is_empty(),
        "a codec naming an empty blob reads as evidence that exists"
    );
    assert_eq!(
        rule_id, "disk-critical",
        "and the alert itself still arrives"
    );
}

/// The holding area keeps microseconds and the wire carries seconds. A window
/// shorter than a second must not collapse into one that ends before it starts.
#[test]
fn sub_second_precision_does_not_invert_the_window() {
    let alert = EdgeAlert {
        window_start_micros: FIRED_AT * SECOND + 999_000,
        window_end_micros: (FIRED_AT + 1) * SECOND + 1_000,
        ..live()
    };
    let ControlMessage::AgentAlert {
        window_start_ts,
        window_end_ts,
        ..
    } = sent(&alert)
    else {
        unreachable!()
    };

    assert!(
        window_end_ts >= window_start_ts,
        "rounding may not turn a real window into one the server refuses"
    );
}
