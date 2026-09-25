//! Turning an alert this machine raised into the message that carries it.
//!
//! The far end admits an alert or refuses it under exactly one counted reason,
//! and a machine whose alerts are all refused reads the same as a machine that
//! raised none. So this is where the two vocabularies are reconciled, once,
//! rather than at each producer:
//!
//! - **Time is seconds on the wire and microseconds here.** The window start is
//!   part of the alert's identity, so it is floored and the end is raised to
//!   meet it — a window shorter than a second must not arrive ending before it
//!   began, which describes no interval and is refused.
//! - **Severity is always stated.** "Nothing said" and "not serious" must never
//!   look alike at the far end, so the field is never left to a default.
//! - **A codec names something.** Evidence and its codec are empty together: a
//!   codec on an empty blob reads as evidence that exists.
//!
//! Nothing is redacted here. Every free-text field on an alert was redacted by
//! the producer that composed it, before the alert existed.

use uuid::Uuid;

use mesh_protocol::{AlertSeverity as WireSeverity, ControlMessage};

use super::sink::{AlertOrigin, AlertSeverity, EdgeAlert};

/// Microseconds in a second. The sink keeps one, the wire carries the other.
const MICROS_PER_SEC: i64 = 1_000_000;

/// The message that carries one alert off this machine.
///
/// The machine mints an id for its own report so one can be traced from the
/// agent log to the row a technician opens. It is not the alert's identity —
/// that is the rule, its revision and the window — so a lost or unreadable id
/// costs a trace and never the alert.
#[must_use]
pub fn alert_message(alert: &EdgeAlert) -> ControlMessage {
    let start = floor_seconds(alert.window_start_micros);
    ControlMessage::AgentAlert {
        alert_id: Uuid::new_v4().to_string(),
        rule_id: alert.rule_id.clone(),
        rule_version: alert.rule_version,
        severity: wire_severity(alert.severity),
        metric: alert.metric.clone(),
        value: alert.value,
        window_start_ts: start,
        // Raised to meet its own start rather than floored independently: the
        // start is identity and cannot move, so a sub-second window has to give
        // its end instead of arriving inverted.
        window_end_ts: floor_seconds(alert.window_end_micros).max(start),
        observed_ts: floor_seconds(alert.ts_micros),
        backfilled: matches!(alert.origin, AlertOrigin::Backfilled),
        evidence_codec: alert.evidence_codec.clone(),
        evidence: alert.evidence.clone(),
    }
}

/// Whole seconds from microseconds, rounding toward the epoch's past so a
/// window start is stable across every send of the same alert.
fn floor_seconds(micros: i64) -> i64 {
    micros.div_euclid(MICROS_PER_SEC)
}

/// How this machine's severities are spelled on the wire. The set is closed on
/// both sides, so this is a spelling rather than a decision.
fn wire_severity(severity: AlertSeverity) -> WireSeverity {
    match severity {
        AlertSeverity::Info => WireSeverity::Info,
        AlertSeverity::Warning => WireSeverity::Warning,
        AlertSeverity::Critical => WireSeverity::Critical,
    }
}
