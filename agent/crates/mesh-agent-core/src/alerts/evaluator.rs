use mesh_protocol::{AlertBreach, AlertComparator, ThresholdRule};

use crate::ml::sampler::MetricSample;

/// Per-rule evaluation state. A rule advances Clear → Pending → Firing → Clear as
/// the watched metric breaches, sustains, and finally recovers past the
/// hysteresis boundary.
#[derive(Debug, Clone, Copy, PartialEq)]
enum RuleState {
    /// The metric is on the safe side of the threshold (or of the hysteresis
    /// clear boundary while recovering).
    Clear,
    /// The metric is breaching but the sustain window has not yet elapsed; holds
    /// the unix-second timestamp of the breach onset.
    Pending { since: i64 },
    /// The breach has sustained and is firing; it is hysteresis-latched until the
    /// metric recovers past the clear boundary.
    Firing,
}

/// A rule plus its live evaluation state.
struct RuleEntry {
    rule: ThresholdRule,
    state: RuleState,
}

impl RuleEntry {
    /// Advance the state machine for one sample value at `ts` and return whether
    /// the rule is firing afterwards.
    fn step(&mut self, value: f64, ts: i64) -> bool {
        let breaching = compare(self.rule.comparator, value, self.rule.threshold);
        // Cleared = the value is on the safe side of the clear boundary. With a
        // clear boundary equal to the threshold this collapses to plain
        // threshold crossing (no hysteresis band).
        let cleared = !compare(self.rule.comparator, value, self.rule.clear);
        self.state = match self.state {
            RuleState::Clear if breaching => {
                if self.rule.sustain_secs == 0 {
                    RuleState::Firing
                } else {
                    RuleState::Pending { since: ts }
                }
            }
            RuleState::Clear => RuleState::Clear,
            RuleState::Pending { .. } if !breaching => RuleState::Clear,
            RuleState::Pending { since }
                if ts.saturating_sub(since) >= i64::from(self.rule.sustain_secs) =>
            {
                RuleState::Firing
            }
            RuleState::Pending { since } => RuleState::Pending { since },
            RuleState::Firing if cleared => RuleState::Clear,
            RuleState::Firing => RuleState::Firing,
        };
        matches!(self.state, RuleState::Firing)
    }
}

/// Stateful evaluator for a tenant-scoped threshold-alert ruleset (WS-19). Feed
/// it one [`MetricSample`] per window with the sample's unix-second timestamp;
/// it returns the set of currently-firing breaches.
pub struct AlertEvaluator {
    entries: Vec<RuleEntry>,
}

impl AlertEvaluator {
    /// Create an evaluator for `rules`, all starting in the Clear state.
    pub fn new(rules: Vec<ThresholdRule>) -> Self {
        Self {
            entries: rules
                .into_iter()
                .map(|rule| RuleEntry {
                    rule,
                    state: RuleState::Clear,
                })
                .collect(),
        }
    }

    /// Replace the active ruleset. Evaluation state is preserved for any rule
    /// whose definition is unchanged — an identical re-push (e.g. on reconnect)
    /// must not reset a firing breach — while added or changed rules start Clear
    /// and dropped rules are discarded.
    pub fn set_rules(&mut self, rules: Vec<ThresholdRule>) {
        let previous = std::mem::take(&mut self.entries);
        self.entries = rules
            .into_iter()
            .map(|rule| {
                let state = previous
                    .iter()
                    .find(|entry| entry.rule == rule)
                    .map_or(RuleState::Clear, |entry| entry.state);
                RuleEntry { rule, state }
            })
            .collect();
    }

    /// Evaluate every rule against `sample` at `ts` and return the firing
    /// breaches. A rule whose metric is not present in the sample never fires.
    pub fn evaluate(&mut self, sample: &MetricSample, ts: i64) -> Vec<AlertBreach> {
        let mut breaches = Vec::new();
        for entry in &mut self.entries {
            let Some(value) = metric_value(&entry.rule.metric, sample) else {
                entry.state = RuleState::Clear;
                continue;
            };
            if entry.step(value, ts) {
                breaches.push(AlertBreach {
                    rule_id: entry.rule.id.clone(),
                    metric: entry.rule.metric.clone(),
                    value,
                });
            }
        }
        breaches
    }
}

/// Map a rule's declared metric name to the matching sampler dimension. The
/// vocabulary is the three percent gauges the sampler exposes; an unrecognized
/// name yields `None`, so a rule referencing it never fires.
fn metric_value(metric: &str, sample: &MetricSample) -> Option<f64> {
    match metric {
        "cpu.total" => Some(f64::from(sample.cpu_total_percent)),
        "mem.used" => Some(f64::from(sample.memory_used_percent)),
        "disk.used" => Some(f64::from(sample.disk_used_percent)),
        _ => None,
    }
}

/// Apply a comparator between the sample value and a boundary.
fn compare(comparator: AlertComparator, value: f64, bound: f64) -> bool {
    match comparator {
        AlertComparator::Gt => value > bound,
        AlertComparator::Lt => value < bound,
        AlertComparator::Gte => value >= bound,
        AlertComparator::Lte => value <= bound,
        // A future comparator an older agent does not understand is treated as
        // "not breaching", so an unknown rule fails safe (never fires).
        _ => false,
    }
}
