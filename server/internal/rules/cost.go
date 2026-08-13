package rules

import "github.com/volchanskyi/opengate/server/internal/protocol"

// What a rule costs an endpoint, and the budget that bounds it.
//
// The number is the readings a rule retains and may touch. It mirrors
// `rule_cost` in the agent's evaluator exactly — the agent sizes its ring buffer
// from the same figure, so what a rule costs to evaluate is what it costs to
// hold. Because it is computable from a rule's declared fields alone, a
// predicate whose cost cannot be worked out statically is one the grammar
// cannot express, and the budget below is enforceable before a rule is ever
// pushed.
const (
	// MaxRuleCost is the readings one rule may ask an endpoint to hold: an hour
	// of per-second samples. A rule needing more than an hour of history is not
	// a threshold rule, it is a query, and belongs on the aggregates instead.
	MaxRuleCost uint64 = 3600
	// MaxCatalogueCost bounds what the whole shipped pack asks of one endpoint.
	// The per-rule ceiling alone does not bound an agent — enough rules just
	// inside it would still sink one — so the total is capped as well.
	MaxCatalogueCost uint64 = 20000
)

// predicateCost is the readings one predicate retains. An instant reading needs
// only the current one; a windowed predicate holds every second of its window
// plus the second that closes it, because the rate needs both ends and the
// aggregates need the whole run.
func predicateCost(predicate protocol.RulePredicate, windowSecs uint32) uint64 {
	if predicate == protocol.RulePredicateInstant {
		return 1
	}
	return uint64(windowSecs) + 1
}

// RuleCost is a rule's whole evaluation cost: its own condition plus every
// extra one it requires.
func RuleCost(def Definition) uint64 {
	cost := predicateCost(def.Predicate(), def.WindowSecs)
	for _, term := range def.All {
		cost = saturatingAdd(cost, predicateCost(term.Predicate(), term.WindowSecs))
	}
	return cost
}

// saturatingAdd keeps a pathological catalogue from wrapping the budget check
// around zero and passing a gate it should fail.
func saturatingAdd(a, b uint64) uint64 {
	if sum := a + b; sum >= a {
		return sum
	}
	return ^uint64(0)
}
