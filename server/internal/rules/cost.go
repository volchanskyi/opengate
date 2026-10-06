package rules

import "github.com/volchanskyi/opengate/server/internal/protocol"

// The cost limits count retained readings, as `rule_cost` does in the agent's evaluator.
const (
	// MaxRuleCost is the readings one rule may ask an endpoint to hold: an hour of per-second samples.
	MaxRuleCost uint64 = 3600
	// MaxCatalogueCost bounds the total readings the whole shipped pack asks of one endpoint.
	MaxCatalogueCost uint64 = 20000
)

// predicateCost is the readings one predicate retains: one for an instant reading, otherwise
// the window's seconds plus the second that closes it.
func predicateCost(predicate protocol.RulePredicate, windowSecs uint32) uint64 {
	if predicate == protocol.RulePredicateInstant {
		return 1
	}
	return uint64(windowSecs) + 1
}

// RuleCost is a rule's whole evaluation cost: its own condition plus every extra one.
// An event-watching rule costs zero because the agent polls its log once a minute for the pack.
func RuleCost(def Definition) uint64 {
	if def.WatchesEvents() {
		return 0
	}
	cost := predicateCost(def.Predicate(), def.WindowSecs)
	for _, term := range def.All {
		cost = saturatingAdd(cost, predicateCost(term.Predicate(), term.WindowSecs))
	}
	return cost
}

// saturatingAdd caps at the maximum so an overflowing sum cannot wrap to zero and pass the budget.
func saturatingAdd(a, b uint64) uint64 {
	if sum := a + b; sum >= a {
		return sum
	}
	return ^uint64(0)
}
