package rules

import (
	"time"

	"github.com/google/uuid"
)

// What happens to a customer's tuning when the rule it tunes changes.
//
// A rule upgrade applies by itself and keeps whatever the customer set — a
// threshold somebody chose for their file servers is theirs, and a new version
// of the rule is not a reason to take it away. But a new version may narrow what
// it will accept, and then the customer's number is outside the range the rule's
// author now allows.
//
// Three things could happen to it and two of them are wrong. Dropping it reverts
// the estate to a default nobody asked for, silently. Keeping it puts a value on
// the wire the rule's author refused. So it moves to the nearest value the new
// version does allow, the move is recorded, and it stays visible until an
// administrator has seen it. The rule keeps firing throughout, which is the
// whole point: going quiet is the failure this exists to prevent.

// Nearest returns the closest value the bounds contain, and whether the given
// one had to move at all.
func (b Bounds) Nearest(v float64) (float64, bool) {
	switch {
	case v < b.Min:
		return b.Min, true
	case v > b.Max:
		return b.Max, true
	default:
		return v, false
	}
}

// Clamp is one tuned value a rule version no longer allows, and where it went.
// It stays until an administrator acknowledges it, because a move nobody saw is
// indistinguishable from a threshold that was always that number.
type Clamp struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	BindingID      uuid.UUID
	RuleID         string
	// RuleVersion is the version that narrowed the range, so the same upgrade
	// read twice records one move rather than one per read.
	RuleVersion int
	Param       string
	// From is what the customer had set; To is where the new version put it.
	From float64
	To   float64

	ClampedAt time.Time
	// AcknowledgedAt is zero while the move is still outstanding.
	AcknowledgedAt time.Time
	AcknowledgedBy string
}

// Outstanding reports whether the move is still waiting to be seen.
func (c Clamp) Outstanding() bool { return c.AcknowledgedAt.IsZero() }

// ClampBinding returns the binding as the definition will honour it, and the
// moves that took. A parameter the definition no longer offers at all is dropped
// from the result and recorded as a move to the value the rule ships, so the
// rule still runs and the loss is on the screen rather than in a diff.
func ClampBinding(def Definition, b Binding) (Binding, []Clamp) {
	if len(b.Params) == 0 {
		return b, nil
	}

	params := make(map[string]float64, len(b.Params))
	var moves []Clamp

	for _, name := range sortedParamNames(b.Params) {
		value := b.Params[name]
		bounds, tunable := def.Tunable[name]
		if !tunable {
			shipped, _ := def.ShippedParam(name)
			moves = append(moves, clampOf(def, b, name, value, shipped))
			continue
		}
		nearest, moved := bounds.Nearest(value)
		params[name] = nearest
		if moved {
			moves = append(moves, clampOf(def, b, name, value, nearest))
		}
	}

	b.Params = params
	return b, moves
}

// ClampBindings reads a whole customer's tuning against the pack as it now
// stands, which is what a rule upgrade has to be reconciled against.
func ClampBindings(cat Pack, bindings []Binding) []Clamp {
	var moves []Clamp
	for _, b := range bindings {
		def, ok := cat.Lookup(b.RuleID)
		if !ok {
			continue
		}
		if _, clamped := ClampBinding(def, b); len(clamped) > 0 {
			moves = append(moves, clamped...)
		}
	}
	return moves
}

// clampOf records one move. The id is fresh here and the row it is written to is
// keyed on the binding, the parameter and the version — so re-reading the same
// upgrade keeps the first record rather than making a second.
func clampOf(def Definition, b Binding, param string, from, to float64) Clamp {
	return Clamp{
		ID:             uuid.New(),
		OrganizationID: b.OrganizationID,
		BindingID:      b.ID,
		RuleID:         def.ID,
		RuleVersion:    def.Version,
		Param:          param,
		From:           from,
		To:             to,
	}
}
