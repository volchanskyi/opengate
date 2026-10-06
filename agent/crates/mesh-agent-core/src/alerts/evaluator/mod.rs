use mesh_protocol::{
    AlertBreach, AlertComparator, AlertSeverity, RuleCoverage, RuleCoverageState, ThresholdRule,
    MAX_RULE_TERMS,
};

use crate::ml::sampler::MetricSample;
use crate::ml::store_sink::DimReadings;

mod condition;

use condition::{predicate_cost, Condition, Reading, RuleState};

pub(super) use condition::window_is_expressible;

/// The readings one rule may touch per second before the evaluator stops running it.
pub const RULE_BUDGET_READINGS_PER_SEC: u64 = 3600;

/// The span, in seconds, an allowance is granted over.
pub const RULE_BUDGET_WINDOW_SECS: i64 = 60;

/// The whole allowance one rule may spend inside a window.
const RULE_BUDGET_PER_WINDOW: u64 =
    RULE_BUDGET_READINGS_PER_SEC * RULE_BUDGET_WINDOW_SECS.unsigned_abs();

/// A rule's whole evaluation cost: its own condition plus every extra one.
#[must_use]
pub fn rule_cost(rule: &ThresholdRule) -> u64 {
    let mut cost = predicate_cost(rule.predicate, rule.window_secs);
    for term in &rule.all {
        cost = cost.saturating_add(predicate_cost(term.predicate, term.window_secs));
    }
    cost
}

/// What one rule may cost the machine, enforced over the readings it touched; a spent rule stops
/// and other rules keep evaluating.
struct RuleBudget {
    /// Start of the span the current allowance is being spent against.
    window_start: Option<i64>,
    spent: u64,
    throttled: bool,
}

impl RuleBudget {
    fn new() -> Self {
        Self {
            window_start: None,
            spent: 0,
            throttled: false,
        }
    }

    /// Charges one evaluation's work and reports whether the rule is now stopped.
    /// A timestamp outside the current span, including a backwards clock step, opens a new span.
    fn charge(&mut self, ts: i64, work: u64) -> bool {
        let within = self
            .window_start
            .is_some_and(|start| ts >= start && ts - start < RULE_BUDGET_WINDOW_SECS);
        if !within {
            self.window_start = Some(ts);
            self.spent = 0;
        }
        self.spent = self.spent.saturating_add(work);
        if self.spent > RULE_BUDGET_PER_WINDOW {
            self.throttled = true;
        }
        self.throttled
    }
}

/// One rule firing on this instant's readings; `started` marks the instant it began firing.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
pub struct Firing {
    /// Which rule is firing.
    pub rule_id: String,
    /// The revision of the rule this machine runs, carried onto every alert it raises.
    pub rule_version: u32,
    /// The severity the rule declares, put on the alert unchanged.
    pub severity: AlertSeverity,
    /// The dimension it watched, under the name the fleet collects it by.
    pub metric: String,
    /// The reading that crossed the line.
    pub value: f64,
    /// When the breach began holding; equals `at` for a rule with no hold.
    pub since: i64,
    /// The instant these readings were taken.
    pub at: i64,
    /// Whether this is the instant the rule started firing.
    pub started: bool,
}

/// A rule plus its live evaluation state.
struct RuleEntry {
    rule: ThresholdRule,
    state: RuleState,
    /// Every condition the rule requires, its own first; `None` marks the rule unsupported.
    conditions: Option<Vec<Condition>>,
    /// What the last evaluation concluded this rule is doing here.
    coverage: RuleCoverageState,
    /// What this rule may cost this machine, and what it has spent.
    budget: RuleBudget,
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
            budget: RuleBudget::new(),
        }
    }

    /// Evaluates every condition and advances the state machine; while firing, returns the
    /// primary value, the breach start and whether it began this second.
    fn step(&mut self, sample: &DimReadings, ts: i64) -> Option<(f64, i64, bool)> {
        // A throttled rule stays stopped until a different rule definition replaces it.
        if self.budget.throttled {
            self.coverage = RuleCoverageState::Throttled;
            return None;
        }

        let Some(conditions) = self.conditions.as_mut() else {
            self.coverage = RuleCoverageState::Unsupported;
            self.state = RuleState::Clear;
            return None;
        };

        let mut work: u64 = 0;
        let readings: Vec<Reading> = conditions
            .iter_mut()
            .map(|condition| {
                let (reading, cost) = condition.step(sample, ts);
                work = work.saturating_add(cost);
                reading
            })
            .collect();

        // The overspending second is the last one evaluated; its result and history are dropped.
        if self.budget.charge(ts, work) {
            self.coverage = RuleCoverageState::Throttled;
            self.state = RuleState::Clear;
            for condition in conditions.iter_mut() {
                condition.reset();
            }
            return None;
        }

        if readings.contains(&Reading::Unsupported) {
            // An unreadable condition marks the rule unsupported and drops any latched state.
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
        // The breach is over once any one condition has recovered past its own boundary.
        let cleared = conditions
            .iter()
            .zip(&values)
            .any(|(condition, &value)| condition.cleared(value));

        let before = self.state;
        self.state = advance(self.state, self.rule.sustain_secs, breaching, cleared, ts);
        if !matches!(self.state, RuleState::Firing) {
            return None;
        }
        // Where the breach began: the instant the hold started counting, or
        // this one for a rule with no hold, which goes straight to firing.
        let since = match before {
            RuleState::Pending { since } => since,
            _ => ts,
        };
        Some((values[0], since, !matches!(before, RuleState::Firing)))
    }
}

/// Every condition a rule requires, its own first, or `None` when any is invalid or the rule has
/// too many extra conditions.
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

/// Stateful evaluator for a threshold-alert ruleset, fed one [`MetricSample`] per second with its
/// unix-second timestamp.
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

    /// Replaces the active ruleset, keeping state for unchanged rules; added or changed rules
    /// start Clear and dropped rules are discarded.
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

    /// Evaluate every rule against the second `sample` describes and return the
    /// firing breaches. A rule this device cannot evaluate never fires.
    pub fn evaluate(&mut self, sample: &MetricSample, ts: i64) -> Vec<Firing> {
        self.evaluate_readings(&DimReadings::of_sample(sample), ts)
    }

    /// Evaluates every rule against one instant's readings at `ts`, for both the live path and
    /// retroactive scans.
    pub fn evaluate_readings(&mut self, readings: &DimReadings, ts: i64) -> Vec<Firing> {
        let mut firing = Vec::new();
        for entry in &mut self.entries {
            if let Some((value, since, started)) = entry.step(readings, ts) {
                firing.push(Firing {
                    rule_id: entry.rule.id.clone(),
                    rule_version: entry.rule.version,
                    severity: entry.rule.severity,
                    // The canonical metric name, whatever alias the rule was written in.
                    metric: entry
                        .conditions
                        .as_ref()
                        .and_then(|conditions| conditions.first())
                        .map_or_else(|| entry.rule.metric.clone(), |c| c.metric.to_string()),
                    value,
                    since,
                    at: ts,
                    started,
                });
            }
        }
        firing
    }

    /// What every firing rule is doing across the whole episode.
    #[must_use]
    pub fn breaches(firing: &[Firing]) -> Vec<AlertBreach> {
        firing
            .iter()
            .map(|f| AlertBreach {
                rule_id: f.rule_id.clone(),
                metric: f.metric.clone(),
                value: f.value,
            })
            .collect()
    }

    /// What every installed rule is doing, one entry per rule; unsupported rules are included.
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

/// Apply a comparator between the sample value and a boundary.
fn compare(comparator: AlertComparator, value: f64, bound: f64) -> bool {
    match comparator {
        AlertComparator::Gt => value > bound,
        AlertComparator::Lt => value < bound,
        AlertComparator::Gte => value >= bound,
        AlertComparator::Lte => value <= bound,
        // An unrecognised comparator never breaches.
        _ => false,
    }
}
