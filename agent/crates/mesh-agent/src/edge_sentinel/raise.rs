//! Raising an alert at the moment a rule starts firing, with everything this
//! machine knows about why attached.

use mesh_agent_core::alerts::{
    pack_evidence, AlertEvaluator, AlertOrigin, AlertSeverity, AlertSink, DimSeries, EdgeAlert,
    EvidenceSource, Firing, SERIES_DIMS, SERIES_SPAN_SECS,
};
use mesh_agent_core::correlate::{
    correlate_snapshot, CorrelationLimits, CorrelationWindow, Ranked,
};
use mesh_agent_core::ml::sampler::MetricSample;
use mesh_agent_core::ml::store_sink::dim_series;
use mesh_protocol::{HistoryPoint, ProcessReportEntry};
use tracing::debug;

use super::SharedSink;
use crate::event_watch::EventCoverage;

/// How far either side of the firing instant the evidence's readings reach.
/// Matches the span the composition contracts for, so what a technician opens
/// looks the same whichever producer raised it.
const EVIDENCE_SPAN_SECS: i64 = SERIES_SPAN_SECS;

/// How much history the ranking compares the event against. Long enough to say
/// what normal looked like on this machine, short enough that a slow drift over
/// a week is not mistaken for the machine's baseline.
const EVIDENCE_BASELINE_SECS: i64 = 1_800;

/// Microseconds in a second: the sampler works in one, the alert queue in the
/// other.
pub(super) const MICROS_PER_SEC: i64 = 1_000_000;

/// What every rule is doing on this machine: the ones this sampler evaluates,
/// and the ones the log watch answers for. The estate counts every rule against
/// the whole fleet, so a rule reported by neither would read as a rule nobody
/// pushed rather than one watching every machine.
pub(super) fn all_coverage(
    evaluator: &AlertEvaluator,
    events: &EventCoverage,
) -> Vec<mesh_protocol::RuleCoverage> {
    let mut coverage = evaluator.coverage();
    if let Ok(reported) = events.lock() {
        coverage.extend(reported.iter().cloned());
    }
    coverage
}

/// Raise one alert for a rule that has just started firing.
///
/// The evidence is assembled here rather than at delivery, because this is the
/// only moment it exists: central keeps a sixty-second average per dimension
/// and there is no path for asking the machine later, so a ten-second collapse
/// that explains the incident is on this message or it is nowhere.
///
/// The window is the stretch the rule actually held over, so a re-delivery
/// after a broken link resolves to the row already written.
pub(super) fn raise_alert(
    alerts: &AlertSink,
    store: Option<&SharedSink>,
    sample: &MetricSample,
    firing: &Firing,
    now: i64,
) {
    let ranked = ranked_dimensions(store, firing.at);
    let packed = pack_evidence(&EvidenceSource {
        ranked: &ranked,
        readings: &readings_behind(store, &ranked, firing.at),
        processes: &running_now(sample),
        log_lines: &[],
        event_ts: firing.at,
    });
    let outcome = alerts.push(
        EdgeAlert {
            rule_id: firing.rule_id.clone(),
            // The revision the machine is actually running, which is the one
            // the server sent with the rule.
            rule_version: firing.rule_version,
            severity: edge_severity(firing.severity),
            ts_micros: firing.at.saturating_mul(MICROS_PER_SEC),
            window_start_micros: firing.since.saturating_mul(MICROS_PER_SEC),
            window_end_micros: firing.at.saturating_mul(MICROS_PER_SEC),
            metric: firing.metric.clone(),
            value: Some(firing.value),
            subject: firing.metric.clone(),
            summary: String::new(),
            evidence: packed.bytes,
            evidence_codec: packed.codec.to_string(),
            origin: AlertOrigin::Live,
        },
        now.saturating_mul(MICROS_PER_SEC),
    );
    debug!(
        rule_id = %firing.rule_id, metric = %firing.metric, value = firing.value,
        ?outcome, "edge-sentinel raised an alert"
    );
}

/// How this machine spells a severity the server sent it. The set is closed on
/// both sides, so this is a spelling rather than a decision.
fn edge_severity(severity: mesh_protocol::AlertSeverity) -> AlertSeverity {
    match severity {
        mesh_protocol::AlertSeverity::Info => AlertSeverity::Info,
        mesh_protocol::AlertSeverity::Critical => AlertSeverity::Critical,
        _ => AlertSeverity::Warning,
    }
}

/// Which of this machine's readings broke pattern around the event, ranked by
/// the machine itself. A machine with no local store answers with nothing
/// rather than with a ranking of one dimension it happened to have.
fn ranked_dimensions(store: Option<&SharedSink>, at: i64) -> Vec<Ranked> {
    let Some(window) = CorrelationWindow::new(
        at.saturating_sub(EVIDENCE_BASELINE_SECS),
        at,
        at.saturating_sub(EVIDENCE_SPAN_SECS),
        at.saturating_add(EVIDENCE_SPAN_SECS),
    ) else {
        return Vec::new();
    };
    let Some(snapshot) = store.and_then(|s| s.lock().ok()?.snapshot().ok()) else {
        return Vec::new();
    };
    correlate_snapshot(&snapshot, &window, &CorrelationLimits::default())
        .map(|ranking| ranking.ranked)
        .unwrap_or_default()
}

/// The readings behind the dimensions that ranked highest. A dimension whose
/// readings the store has already evicted costs the alert its series and
/// nothing else.
fn readings_behind(store: Option<&SharedSink>, ranked: &[Ranked], at: i64) -> Vec<DimSeries> {
    let Some(snapshot) = store.and_then(|s| s.lock().ok()?.snapshot().ok()) else {
        return Vec::new();
    };
    let (from, to) = (
        at.saturating_sub(EVIDENCE_SPAN_SECS),
        at.saturating_add(EVIDENCE_SPAN_SECS).saturating_add(1),
    );
    ranked
        .iter()
        .take(SERIES_DIMS)
        .filter_map(|r| {
            let series = dim_series(&r.dim)?;
            let points = snapshot
                .range_raw(series, from, to)
                .ok()?
                .into_iter()
                .map(|(sample, _anomaly)| HistoryPoint {
                    ts: sample.ts,
                    value: sample.value,
                })
                .collect();
            Some(DimSeries {
                dim: r.dim.clone(),
                points,
            })
        })
        .collect()
}

/// What was running at the instant the rule fired, busiest first. The basenames
/// are redacted by the composer on their way in, because a process name is a
/// free-text field a host chose.
fn running_now(sample: &MetricSample) -> Vec<ProcessReportEntry> {
    sample
        .processes
        .iter()
        .map(|p| ProcessReportEntry {
            rank: u32::from(p.rank),
            basename: p.basename.clone(),
            cmdline_hash: p.cmdline_hash.clone(),
            pid: p.pid,
            cpu: p.cpu,
            mem: p.mem,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::{
        all_coverage, edge_severity, ranked_dimensions, readings_behind, running_now,
        EVIDENCE_BASELINE_SECS, EVIDENCE_SPAN_SECS,
    };
    use crate::edge_sentinel::test_support::{busy_sample, cpu_rule, host_sample, store, T0};
    use crate::edge_sentinel::tick::persist;
    use mesh_agent_core::alerts::AlertSeverity;
    use mesh_protocol::{RuleCoverage, RuleCoverageState};
    use std::sync::{Arc, Mutex};

    /// With no store the evidence has no ranking and no readings, rather than a
    /// ranking of whatever one dimension happened to be at hand.
    #[test]
    fn a_machine_without_a_store_attaches_no_ranking() {
        assert!(ranked_dimensions(None, T0).is_empty());
        assert!(readings_behind(None, &[], T0).is_empty());
    }

    /// With a store, the dimension that broke pattern ranks, and the readings
    /// behind it travel with the alert.
    #[test]
    fn a_store_ranks_what_moved_and_hands_over_its_readings() {
        let dir = tempfile::tempdir().unwrap();
        let sink = store(&dir);
        let at = T0 + EVIDENCE_BASELINE_SECS;
        for ts in T0..=at + EVIDENCE_SPAN_SECS {
            let cpu = if ts >= at - 30 {
                97.0
            } else {
                10.0 + (ts % 7) as f32
            };
            persist(&sink, ts, &host_sample(cpu), false);
        }

        let ranked = ranked_dimensions(Some(&sink), at);
        assert!(
            ranked.iter().any(|r| r.dim == "cpu.total"),
            "the processor broke pattern (ranked: {ranked:?})"
        );
        let readings = readings_behind(Some(&sink), &ranked, at);
        let cpu = readings
            .iter()
            .find(|d| d.dim == "cpu.total")
            .expect("the processor's readings travel with the alert");
        assert!(cpu.points.iter().any(|p| p.value == 97.0));
    }

    #[test]
    fn severities_are_spelled_the_way_the_server_sent_them() {
        use mesh_protocol::AlertSeverity as Wire;
        assert_eq!(edge_severity(Wire::Info), AlertSeverity::Info);
        assert_eq!(edge_severity(Wire::Warning), AlertSeverity::Warning);
        assert_eq!(edge_severity(Wire::Critical), AlertSeverity::Critical);
    }

    #[test]
    fn what_was_running_keeps_every_field_the_technician_reads() {
        let rows = running_now(&busy_sample(50.0));
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].rank, 1);
        assert_eq!(rows[0].basename, "pg_dump");
        assert_eq!(rows[0].pid, 4242);
        assert_eq!(rows[0].cpu, 88.0);
        assert_eq!(rows[0].mem, 3.5);
    }

    #[test]
    fn coverage_counts_the_sampler_rules_and_the_log_watch_rules() {
        let mut eval = mesh_agent_core::alerts::AlertEvaluator::new(vec![cpu_rule()]);
        eval.evaluate(&host_sample(10.0), T0);
        let events = Arc::new(Mutex::new(vec![RuleCoverage {
            rule_id: "linux-hung-task".to_string(),
            state: RuleCoverageState::Active,
        }]));
        let ids: Vec<_> = all_coverage(&eval, &events)
            .into_iter()
            .map(|c| c.rule_id)
            .collect();
        assert_eq!(ids, vec!["cpu-saturated", "linux-hung-task"]);
    }
}
