//! Converts an alert raised on this machine into the wire message that carries it.

use uuid::Uuid;

use mesh_protocol::{AlertSeverity as WireSeverity, ControlMessage};

use super::sink::{AlertOrigin, AlertSeverity, EdgeAlert};

const MICROS_PER_SEC: i64 = 1_000_000;

/// The wire message for one alert, with a fresh trace id that is not part of the alert's identity.
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
        // The start is part of the alert's identity, so a sub-second window raises its end to meet it.
        window_end_ts: floor_seconds(alert.window_end_micros).max(start),
        observed_ts: floor_seconds(alert.ts_micros),
        backfilled: matches!(alert.origin, AlertOrigin::Backfilled),
        evidence_codec: alert.evidence_codec.clone(),
        evidence: alert.evidence.clone(),
    }
}

/// Whole seconds from microseconds, floored so a window start is stable across resends.
fn floor_seconds(micros: i64) -> i64 {
    micros.div_euclid(MICROS_PER_SEC)
}

fn wire_severity(severity: AlertSeverity) -> WireSeverity {
    match severity {
        AlertSeverity::Info => WireSeverity::Info,
        AlertSeverity::Warning => WireSeverity::Warning,
        AlertSeverity::Critical => WireSeverity::Critical,
    }
}
