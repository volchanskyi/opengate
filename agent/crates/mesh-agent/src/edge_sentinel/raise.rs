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

/// Seconds either side of the firing instant that the evidence's readings reach.
const EVIDENCE_SPAN_SECS: i64 = SERIES_SPAN_SECS;

/// Seconds of history the ranking compares the event against.
const EVIDENCE_BASELINE_SECS: i64 = 1_800;

/// Microseconds in a second; the sampler works in seconds and the alert queue in microseconds.
pub(super) const MICROS_PER_SEC: i64 = 1_000_000;

/// Coverage of every rule on this machine: those the sampler evaluates plus those the log
/// watch answers for.
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

/// Raises one alert for a rule that just started firing, assembling its evidence at this
/// moment and windowing it over the stretch the rule held so a re-delivery maps to one row.
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

/// Maps the server's severity to the local alert severity; unknown values become warning.
fn edge_severity(severity: mesh_protocol::AlertSeverity) -> AlertSeverity {
    match severity {
        mesh_protocol::AlertSeverity::Info => AlertSeverity::Info,
        mesh_protocol::AlertSeverity::Critical => AlertSeverity::Critical,
        _ => AlertSeverity::Warning,
    }
}

/// Ranks the readings that broke pattern around the event; empty when there is no local store.
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

/// The readings behind the highest-ranked dimensions, skipping any dimension with no stored series.
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

/// The processes running when the rule fired, busiest first; the composer redacts the
/// basenames because a process name is host-chosen free text.
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

    #[test]
    fn a_machine_without_a_store_attaches_no_ranking() {
        assert!(ranked_dimensions(None, T0).is_empty());
        assert!(readings_behind(None, &[], T0).is_empty());
    }

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
