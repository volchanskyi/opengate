use std::collections::VecDeque;

use mesh_protocol::{
    canonical_rule_metric, AlertBreach, AlertComparator, RuleCoverage, RuleCoverageState,
    RulePredicate, RuleTerm, ThresholdRule, MAX_RULE_TERMS, MAX_RULE_WINDOW_SECS,
};

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

/// What one condition produced this second.
#[derive(Debug, Clone, Copy, PartialEq)]
enum Reading {
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
struct Condition {
    /// Canonical metric name, already resolved through the alias map.
    metric: &'static str,
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
    fn new(
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
    fn primary(rule: &ThresholdRule) -> Option<Self> {
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
    fn extra(term: &RuleTerm) -> Option<Self> {
        Self::new(
            &term.metric,
            term.comparator,
            term.threshold,
            term.clear,
            term.predicate,
            term.window_secs,
        )
    }

    /// Take this second's reading and derive the number the comparators see.
    fn step(&mut self, sample: &MetricSample, ts: i64) -> Reading {
        let Some(value) = sample_reading(self.metric, sample) else {
            return Reading::Unsupported;
        };
        if self.predicate == RulePredicate::Instant {
            return Reading::Value(value);
        }
        self.retain(ts, value);
        match self.spanned_window() {
            None => Reading::NotEnoughData,
            Some(window) => Reading::Value(derive(self.predicate, window)),
        }
    }

    /// Append this second's reading and drop everything older than the window.
    /// The capacity bound is the same number [`predicate_cost`] charges, so what
    /// the rule costs to evaluate is what it costs to hold.
    fn retain(&mut self, ts: i64, value: f64) {
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
    fn spanned_window(&self) -> Option<&VecDeque<(i64, f64)>> {
        let (oldest, _) = *self.history.front()?;
        let (newest, _) = *self.history.back()?;
        (newest.saturating_sub(oldest) >= i64::from(self.window_secs)).then_some(&self.history)
    }

    /// Whether `value` is on the breaching side of this condition's threshold.
    fn breaching(&self, value: f64) -> bool {
        compare(self.comparator, value, self.threshold)
    }

    /// Whether `value` has recovered past this condition's clear boundary. With a
    /// clear boundary equal to the threshold this collapses to plain threshold
    /// crossing (no hysteresis band).
    fn cleared(&self, value: f64) -> bool {
        !compare(self.comparator, value, self.clear)
    }

    /// Forget every retained reading, so a rule resuming after a gap decides on
    /// seconds it actually observed.
    fn reset(&mut self) {
        self.history.clear();
    }
}

/// Whether a predicate and window pair is a shape the grammar states. One shape,
/// one meaning: a window on an instant reading would be a field the evaluator
/// ignores and a rule nobody can predict from its own text.
fn window_is_expressible(predicate: RulePredicate, window_secs: u32) -> bool {
    match predicate {
        RulePredicate::Instant => window_secs == 0,
        _ => (1..=MAX_RULE_WINDOW_SECS).contains(&window_secs),
    }
}

/// Reduce a spanned window to the one number the predicate compares. The window
/// is non-empty and spans at least one second, which is what makes the rate's
/// divisor safe.
fn derive(predicate: RulePredicate, window: &VecDeque<(i64, f64)>) -> f64 {
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

/// The readings one predicate retains and may touch — the bound the CI cost
/// analysis compares against its budget. Monotone in the window, and computable
/// from a rule's declared fields alone, which is what makes a predicate whose
/// cost cannot be computed statically impossible to express.
#[must_use]
fn predicate_cost(predicate: RulePredicate, window_secs: u32) -> u64 {
    match predicate {
        RulePredicate::Instant => 1,
        // A windowed predicate holds every second of its window plus the second
        // that closes it: the rate needs both ends, the aggregates the whole run.
        _ => u64::from(window_secs) + 1,
    }
}

/// A rule's whole evaluation cost: its own condition plus every extra one.
#[must_use]
pub fn rule_cost(rule: &ThresholdRule) -> u64 {
    let mut cost = predicate_cost(rule.predicate, rule.window_secs);
    for term in &rule.all {
        cost = cost.saturating_add(predicate_cost(term.predicate, term.window_secs));
    }
    cost
}

/// A rule plus its live evaluation state.
struct RuleEntry {
    rule: ThresholdRule,
    state: RuleState,
    /// Every condition the rule requires, its own first. `None` when the rule is
    /// outside the grammar, which makes it permanently unsupported.
    conditions: Option<Vec<Condition>>,
    /// What the last evaluation concluded this rule is doing here.
    coverage: RuleCoverageState,
}

impl RuleEntry {
    fn new(rule: ThresholdRule) -> Self {
        let conditions = build_conditions(&rule);
        let coverage = if conditions.is_some() {
            RuleCoverageState::Active
        } else {
            RuleCoverageState::Unsupported
        };
        Self {
            rule,
            state: RuleState::Clear,
            conditions,
            coverage,
        }
    }

    /// Evaluate every condition and advance the state machine, returning the
    /// number the rule's own condition produced while it is firing.
    fn step(&mut self, sample: &MetricSample, ts: i64) -> Option<f64> {
        let Some(conditions) = self.conditions.as_mut() else {
            self.coverage = RuleCoverageState::Unsupported;
            self.state = RuleState::Clear;
            return None;
        };

        let readings: Vec<Reading> = conditions
            .iter_mut()
            .map(|condition| condition.step(sample, ts))
            .collect();

        if readings.contains(&Reading::Unsupported) {
            // A rule half of which cannot be read here is not a rule that failed
            // to breach — it is a rule that is not watching this machine. Drop
            // any latched state with it, so a breach nobody is watching cannot
            // survive on the far side of the gap.
            self.coverage = RuleCoverageState::Unsupported;
            self.state = RuleState::Clear;
            for condition in conditions.iter_mut() {
                condition.reset();
            }
            return None;
        }
        self.coverage = RuleCoverageState::Active;

        let mut values = Vec::with_capacity(readings.len());
        for reading in &readings {
            match *reading {
                Reading::Value(value) => values.push(value),
                // Still warming up: no decision this second, and no transition.
                _ => return None,
            }
        }

        let breaching = conditions
            .iter()
            .zip(&values)
            .all(|(condition, &value)| condition.breaching(value));
        // The situation is over as soon as any one side has genuinely recovered
        // past its own boundary. With a single condition this is exactly the
        // hysteresis the rule has always had.
        let cleared = conditions
            .iter()
            .zip(&values)
            .any(|(condition, &value)| condition.cleared(value));

        self.state = advance(self.state, self.rule.sustain_secs, breaching, cleared, ts);
        matches!(self.state, RuleState::Firing).then(|| values[0])
    }
}

/// Every condition a rule requires, its own first, or `None` when any of them is
/// outside the grammar — including a rule that names more extra conditions than
/// the grammar allows.
fn build_conditions(rule: &ThresholdRule) -> Option<Vec<Condition>> {
    if rule.all.len() > MAX_RULE_TERMS {
        return None;
    }
    let mut conditions = Vec::with_capacity(rule.all.len() + 1);
    conditions.push(Condition::primary(rule)?);
    for term in &rule.all {
        conditions.push(Condition::extra(term)?);
    }
    Some(conditions)
}

/// Advance the Clear → Pending → Firing → Clear machine for one decided second.
fn advance(
    state: RuleState,
    sustain_secs: u32,
    breaching: bool,
    cleared: bool,
    ts: i64,
) -> RuleState {
    match state {
        RuleState::Clear if breaching => {
            if sustain_secs == 0 {
                RuleState::Firing
            } else {
                RuleState::Pending { since: ts }
            }
        }
        RuleState::Clear => RuleState::Clear,
        RuleState::Pending { .. } if !breaching => RuleState::Clear,
        RuleState::Pending { since } if ts.saturating_sub(since) >= i64::from(sustain_secs) => {
            RuleState::Firing
        }
        RuleState::Pending { since } => RuleState::Pending { since },
        RuleState::Firing if cleared => RuleState::Clear,
        RuleState::Firing => RuleState::Firing,
    }
}

/// Stateful evaluator for a tenant-scoped threshold-alert ruleset. Feed it one
/// [`MetricSample`] per window with the sample's unix-second timestamp; it
/// returns the set of currently-firing breaches, and reports separately what
/// every rule is doing on this device.
pub struct AlertEvaluator {
    entries: Vec<RuleEntry>,
}

impl AlertEvaluator {
    /// Create an evaluator for `rules`, all starting in the Clear state.
    pub fn new(rules: Vec<ThresholdRule>) -> Self {
        Self {
            entries: rules.into_iter().map(RuleEntry::new).collect(),
        }
    }

    /// Replace the active ruleset. Evaluation state is preserved for any rule
    /// whose definition is unchanged — an identical re-push (e.g. on reconnect)
    /// must not reset a firing breach — while added or changed rules start Clear
    /// and dropped rules are discarded.
    pub fn set_rules(&mut self, rules: Vec<ThresholdRule>) {
        let mut previous = std::mem::take(&mut self.entries);
        self.entries = rules
            .into_iter()
            .map(|rule| {
                match previous
                    .iter()
                    .position(|entry| entry.rule == rule)
                    .map(|index| previous.swap_remove(index))
                {
                    Some(entry) => entry,
                    None => RuleEntry::new(rule),
                }
            })
            .collect();
    }

    /// Evaluate every rule against `sample` at `ts` and return the firing
    /// breaches. A rule this device cannot evaluate never fires.
    pub fn evaluate(&mut self, sample: &MetricSample, ts: i64) -> Vec<AlertBreach> {
        let mut breaches = Vec::new();
        for entry in &mut self.entries {
            if let Some(value) = entry.step(sample, ts) {
                breaches.push(AlertBreach {
                    rule_id: entry.rule.id.clone(),
                    // The canonical name, whatever the rule was written in, so
                    // nothing downstream sees two names for one thing.
                    metric: entry
                        .conditions
                        .as_ref()
                        .and_then(|conditions| conditions.first())
                        .map_or_else(|| entry.rule.metric.clone(), |c| c.metric.to_string()),
                    value,
                });
            }
        }
        breaches
    }

    /// What every installed rule is doing on this device, one entry per rule.
    /// Every rule is here — a rule this host cannot evaluate is reported as
    /// unsupported rather than left out, because a rule missing from the count
    /// is indistinguishable from a rule nobody pushed.
    #[must_use]
    pub fn coverage(&self) -> Vec<RuleCoverage> {
        self.entries
            .iter()
            .map(|entry| RuleCoverage {
                rule_id: entry.rule.id.clone(),
                state: entry.coverage,
            })
            .collect()
    }
}

/// Read one canonical dimension out of a sample, or `None` when this host did not
/// produce it.
///
/// An absent reading means the rule is producing no answer here, and that is
/// what it is reported as. It covers a permanent gap (a kernel with no pressure
/// information, a container whose disk counters are its neighbours') and a
/// passing one (a disk that completed no I/O has no service time, because 0 ms
/// would read as instantaneous), and one sample cannot tell those apart. The
/// choice is deliberately the conservative one: claiming a rule is watching a
/// machine it is producing nothing for is the failure coverage exists to
/// prevent, while a rule that starts answering reports itself active on its
/// next reading.
fn sample_reading(metric: &str, sample: &MetricSample) -> Option<f64> {
    match metric {
        "cpu.total" => Some(f64::from(sample.cpu_total_percent)),
        "mem.used_percent" => Some(f64::from(sample.memory_used_percent)),
        "disk.used_percent" => sample.disk_used_percent.map(f64::from),
        "disk.mounts_critical" => sample.disk_mounts_critical.map(f64::from),
        "net.rx_bps" => sample.network_rx_bps,
        "net.tx_bps" => sample.network_tx_bps,
        "stall.cpu.some" => sample.stall_cpu_some.map(f64::from),
        "stall.mem.some" => sample.stall_mem_some.map(f64::from),
        "stall.mem.full" => sample.stall_mem_full.map(f64::from),
        "stall.io.some" => sample.stall_io_some.map(f64::from),
        "stall.io.full" => sample.stall_io_full.map(f64::from),
        "disk.await_ms" => sample.disk_await_ms.map(f64::from),
        "disk.queue_depth" => sample.disk_queue_depth.map(f64::from),
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
