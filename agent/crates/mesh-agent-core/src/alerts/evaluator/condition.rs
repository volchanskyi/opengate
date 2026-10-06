//! One rule condition: a bounded ring of `(timestamp, value)` readings, the statistic derived from
//! it, and the hysteresis that decides when a breach starts and recovers.

use std::collections::VecDeque;

use mesh_protocol::{
    canonical_rule_metric, AlertComparator, RulePredicate, RuleTerm, ThresholdRule,
    MAX_RULE_WINDOW_SECS,
};

use crate::ml::store_sink::DimReadings;

use super::compare;

/// Per-rule evaluation state, advancing Clear → Pending → Firing → Clear.
#[derive(Debug, Clone, Copy, PartialEq)]
pub(super) enum RuleState {
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

/// What one condition produced this second.
#[derive(Debug, Clone, Copy, PartialEq)]
pub(super) enum Reading {
    /// A number to compare.
    Value(f64),
    /// This host cannot answer the condition at all.
    Unsupported,
    /// The host can answer, but too few seconds have passed to span the window.
    NotEnoughData,
}

/// One condition's declared shape, flattened out of either the rule itself or one
/// of its extra terms so both are evaluated by the same code.
pub(super) struct Condition {
    /// Canonical metric name, already resolved through the alias map.
    pub(super) metric: &'static str,
    comparator: AlertComparator,
    threshold: f64,
    clear: f64,
    predicate: RulePredicate,
    window_secs: u32,
    /// Readings retained for a windowed predicate, oldest first. Empty for an
    /// instant condition, which needs no history.
    history: VecDeque<(i64, f64)>,
}

impl Condition {
    /// Builds a condition from a declared shape, or `None` for an unknown metric or an invalid
    /// predicate and window pair.
    pub(super) fn new(
        metric: &str,
        comparator: AlertComparator,
        threshold: f64,
        clear: f64,
        predicate: RulePredicate,
        window_secs: u32,
    ) -> Option<Self> {
        let metric = canonical_rule_metric(metric)?;
        if !window_is_expressible(predicate, window_secs) {
            return None;
        }
        Some(Self {
            metric,
            comparator,
            threshold,
            clear,
            predicate,
            window_secs,
            history: VecDeque::new(),
        })
    }

    /// The rule's own condition.
    pub(super) fn primary(rule: &ThresholdRule) -> Option<Self> {
        Self::new(
            &rule.metric,
            rule.comparator,
            rule.threshold,
            rule.clear,
            rule.predicate,
            rule.window_secs,
        )
    }

    /// One of the rule's extra conditions.
    pub(super) fn extra(term: &RuleTerm) -> Option<Self> {
        Self::new(
            &term.metric,
            term.comparator,
            term.threshold,
            term.clear,
            term.predicate,
            term.window_secs,
        )
    }

    /// Derives the number the comparators see, plus the readings touched, which are charged
    /// against the per-rule allowance.
    pub(super) fn step(&mut self, readings: &DimReadings, ts: i64) -> (Reading, u64) {
        let Some(value) = readings.of_metric(self.metric) else {
            return (Reading::Unsupported, 1);
        };
        if self.predicate == RulePredicate::Instant {
            return (Reading::Value(value), 1);
        }
        self.retain(ts, value);
        match self.spanned_window() {
            None => (Reading::NotEnoughData, 1),
            Some(window) => {
                let touched = derive_cost(self.predicate, window.len());
                (Reading::Value(derive(self.predicate, window)), 1 + touched)
            }
        }
    }

    /// Appends this second's reading and drops everything older than the window.
    pub(super) fn retain(&mut self, ts: i64, value: f64) {
        self.history.push_back((ts, value));
        let oldest_kept = ts.saturating_sub(i64::from(self.window_secs));
        while self
            .history
            .front()
            .is_some_and(|&(front_ts, _)| front_ts < oldest_kept)
        {
            self.history.pop_front();
        }
        let cap =
            usize::try_from(predicate_cost(self.predicate, self.window_secs)).unwrap_or(usize::MAX);
        while self.history.len() > cap {
            self.history.pop_front();
        }
    }

    /// The retained readings, once they actually span the declared window.
    pub(super) fn spanned_window(&self) -> Option<&VecDeque<(i64, f64)>> {
        let (oldest, _) = *self.history.front()?;
        let (newest, _) = *self.history.back()?;
        (newest.saturating_sub(oldest) >= i64::from(self.window_secs)).then_some(&self.history)
    }

    /// Whether `value` is on the breaching side of this condition's threshold.
    pub(super) fn breaching(&self, value: f64) -> bool {
        compare(self.comparator, value, self.threshold)
    }

    /// Whether `value` has recovered past this condition's clear boundary.
    pub(super) fn cleared(&self, value: f64) -> bool {
        !compare(self.comparator, value, self.clear)
    }

    /// Forget every retained reading, so a rule resuming after a gap decides on
    /// seconds it actually observed.
    pub(super) fn reset(&mut self) {
        self.history.clear();
    }
}

/// Whether a predicate and window pair is valid, shared by the live evaluator and the
/// retroactive planner.
pub(crate) fn window_is_expressible(predicate: RulePredicate, window_secs: u32) -> bool {
    match predicate {
        RulePredicate::Instant => window_secs == 0,
        _ => (1..=MAX_RULE_WINDOW_SECS).contains(&window_secs),
    }
}

/// Reduces a non-empty window spanning at least one second to the number the predicate compares.
pub(super) fn derive(predicate: RulePredicate, window: &VecDeque<(i64, f64)>) -> f64 {
    match predicate {
        RulePredicate::WindowMax => window.iter().map(|&(_, v)| v).fold(f64::MIN, f64::max),
        RulePredicate::WindowMean => {
            window.iter().map(|&(_, v)| v).sum::<f64>() / window.len() as f64
        }
        RulePredicate::Rate => {
            let (oldest_ts, oldest) = window.front().copied().unwrap_or((0, 0.0));
            let (newest_ts, newest) = window.back().copied().unwrap_or((0, 0.0));
            let elapsed = newest_ts.saturating_sub(oldest_ts);
            if elapsed <= 0 {
                return 0.0;
            }
            (newest - oldest) / elapsed as f64
        }
        // Instant predicates never reach here; an unrecognised predicate derives 0.0.
        _ => 0.0,
    }
}

/// The readings a predicate runs over to answer once. A rate needs the two ends
/// of its window whatever is between them; an aggregate reads the whole run.
pub(super) fn derive_cost(predicate: RulePredicate, window_len: usize) -> u64 {
    match predicate {
        RulePredicate::Rate => 2,
        _ => window_len as u64,
    }
}

/// The readings one predicate retains and may touch, computed from the declared fields alone.
#[must_use]
pub(super) fn predicate_cost(predicate: RulePredicate, window_secs: u32) -> u64 {
    match predicate {
        RulePredicate::Instant => 1,
        // A windowed predicate holds every second of its window plus the second
        // that closes it: the rate needs both ends, the aggregates the whole run.
        _ => u64::from(window_secs) + 1,
    }
}
