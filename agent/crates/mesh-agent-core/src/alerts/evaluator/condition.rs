//! One condition: the window of readings it keeps, the statistic it derives
//! from them, and what asking that question costs the machine.
//!
//! A rule is a primary condition plus its extra terms, and each condition is
//! this: a bounded ring of `(timestamp, value)` for the span the rule asks
//! about, a derivation over that ring, and the hysteresis that decides when a
//! breach starts and when it has recovered. A window a stored rollup cannot
//! express is refused here rather than answered at the wrong resolution.

use std::collections::VecDeque;

use mesh_protocol::{
    canonical_rule_metric, AlertComparator, RulePredicate, RuleTerm, ThresholdRule,
    MAX_RULE_WINDOW_SECS,
};

use crate::ml::store_sink::DimReadings;

use super::compare;

/// Per-rule evaluation state. A rule advances Clear → Pending → Firing → Clear as
/// the watched metric breaches, sustains, and finally recovers past the
/// hysteresis boundary.
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
    /// It can, but not enough seconds have passed to span the window. Not an
    /// answer — a rule that fired here would page someone for every agent
    /// restart.
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
    /// Build a condition from a declared shape, or `None` when the grammar
    /// cannot express it — an unknown metric, a window past the bound, a
    /// windowed predicate with no window, or an instant one carrying a window it
    /// would silently ignore.
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

    /// Take this instant's reading and derive the number the comparators see,
    /// alongside the readings that cost — one for looking this second's value up,
    /// plus whatever the predicate had to run over to answer. That second figure
    /// is what the per-rule allowance is spent against, so what a rule costs is
    /// measured rather than assumed from its declared shape.
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

    /// Append this second's reading and drop everything older than the window.
    /// The capacity bound is the same number [`predicate_cost`] charges, so what
    /// the rule costs to evaluate is what it costs to hold.
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

    /// Whether `value` has recovered past this condition's clear boundary. With a
    /// clear boundary equal to the threshold this collapses to plain threshold
    /// crossing (no hysteresis band).
    pub(super) fn cleared(&self, value: f64) -> bool {
        !compare(self.comparator, value, self.clear)
    }

    /// Forget every retained reading, so a rule resuming after a gap decides on
    /// seconds it actually observed.
    pub(super) fn reset(&mut self) {
        self.history.clear();
    }
}

/// Whether a predicate and window pair is a shape the grammar states. One shape,
/// one meaning: a window on an instant reading would be a field the evaluator
/// ignores and a rule nobody can predict from its own text.
///
/// Shared with the retroactive planner, so a shape refused live is refused over
/// history too — two copies of this rule would let a scan evaluate something the
/// live evaluator will not.
pub(crate) fn window_is_expressible(predicate: RulePredicate, window_secs: u32) -> bool {
    match predicate {
        RulePredicate::Instant => window_secs == 0,
        _ => (1..=MAX_RULE_WINDOW_SECS).contains(&window_secs),
    }
}

/// Reduce a spanned window to the one number the predicate compares. The window
/// is non-empty and spans at least one second, which is what makes the rate's
/// divisor safe.
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
        // An instant reading never reaches here; a predicate an older agent does
        // not understand derives nothing rather than guessing.
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

/// The readings one predicate retains and may touch — the bound the CI cost
/// analysis compares against its budget. Monotone in the window, and computable
/// from a rule's declared fields alone, which is what makes a predicate whose
/// cost cannot be computed statically impossible to express.
#[must_use]
pub(super) fn predicate_cost(predicate: RulePredicate, window_secs: u32) -> u64 {
    match predicate {
        RulePredicate::Instant => 1,
        // A windowed predicate holds every second of its window plus the second
        // that closes it: the rate needs both ends, the aggregates the whole run.
        _ => u64::from(window_secs) + 1,
    }
}
