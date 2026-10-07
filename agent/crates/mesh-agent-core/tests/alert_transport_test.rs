//! A raised alert maps to a message carrying every field the server requires.

use mesh_agent_core::alerts::{
    alert_message, AlertOrigin, AlertSeverity, EdgeAlert, EVIDENCE_CODEC,
};
use mesh_protocol::{AlertSeverity as WireSeverity, ControlMessage};

const SECOND: i64 = 1_000_000;

const FIRED_AT: i64 = 1_763_000_000;

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

fn sent(alert: &EdgeAlert) -> ControlMessage {
    let msg = alert_message(alert);
    assert!(
        matches!(msg, ControlMessage::AgentAlert { .. }),
        "an alert must travel as an AgentAlert and nothing else"
    );
    msg
}

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
