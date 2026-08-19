use mesh_protocol::{
    AlertBreach, AlertComparator, RuleCoverage, RuleCoverageState, ThresholdRule, MAX_RULE_TERMS,
};

use crate::ml::sampler::MetricSample;
use crate::ml::store_sink::DimReadings;

mod condition;

use condition::{predicate_cost, Condition, Reading, RuleState};

pub(super) use condition::window_is_expressible;

/// The readings one rule may touch per second on this machine before the machine
/// stops running it.
///
/// It is the figure the shipped pack is bounded by in CI, so the two gates
/// agree: the most expensive rule the pack can contain stays comfortably under
/// this, and a rule that reaches an endpoint without having passed that gate is
/// stopped by the machine paying for it.
pub const RULE_BUDGET_READINGS_PER_SEC: u64 = 3600;

/// The span an allowance is granted over. Long enough that a rule is judged on a
/// minute of behavior rather than on one unlucky second, short enough that a
/// rule which is genuinely too expensive is stopped inside a minute of arriving.
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

/// What one rule may cost the machine it runs on, and what it has spent.
///
/// The pack is cost-bounded before it ships, but the endpoint is what pays, and
/// a rule can reach one without having come through that gate — an operator's
/// own rule, a provider that is not the catalogue, a version skew. So the
/// machine enforces its own ceiling over what a rule actually touched rather
/// than over what it declared, and stops the rule when it is spent.
///
/// The stop is hard and it is per rule: evaluation ends for that rule and for
/// nothing else. One expensive rule silencing the cheap ones would turn a bad
/// rollout into blanket blindness while still looking contained.
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

    /// Charge one evaluation's work and report whether the rule is now stopped.
    /// A timestamp outside the current span — including one that went backwards
    /// over a clock correction — opens a fresh one, so a rule is judged on what
    /// it spends in a minute rather than since the agent started.
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

/// A rule plus its live evaluation state.
struct RuleEntry {
    rule: ThresholdRule,
    state: RuleState,
    /// Every condition the rule requires, its own first. `None` when the rule is
    /// outside the grammar, which makes it permanently unsupported.
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

    /// Evaluate every condition and advance the state machine, returning the
    /// number the rule's own condition produced while it is firing.
    fn step(&mut self, sample: &DimReadings, ts: i64) -> Option<f64> {
        // A rule that spent past its allowance is not evaluated again on this
        // machine. Only a different rule arriving re-arms it, so a reconnect
        // re-pushing the same ruleset cannot spend the allowance a second time.
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

        // The work is already done by the time its cost is known, so the second
        // it overspends is the last one it gets: what it produced here is
        // dropped along with the readings it was holding.
        if self.budget.charge(ts, work) {
            self.coverage = RuleCoverageState::Throttled;
            self.state = RuleState::Clear;
            for condition in conditions.iter_mut() {
                condition.reset();
            }
            return None;
        }

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

    /// Evaluate every rule against the second `sample` describes and return the
    /// firing breaches. A rule this device cannot evaluate never fires.
    pub fn evaluate(&mut self, sample: &MetricSample, ts: i64) -> Vec<AlertBreach> {
        self.evaluate_readings(&DimReadings::of_sample(sample), ts)
    }

    /// Evaluate every rule against one instant's readings at `ts`.
    ///
    /// The live path arrives here with the second the sampler just took, and a
    /// retroactive scan with a minute rebuilt from the local store — the same
    /// state machine either way, so a rule cannot mean one thing now and
    /// something else over history.
    pub fn evaluate_readings(&mut self, readings: &DimReadings, ts: i64) -> Vec<AlertBreach> {
        let mut breaches = Vec::new();
        for entry in &mut self.entries {
            if let Some(value) = entry.step(readings, ts) {
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
