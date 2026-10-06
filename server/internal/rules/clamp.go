package rules

import (
	"time"

	"github.com/google/uuid"
)

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

// Clamp is one tuned value a rule version disallows, and where it moved.
// It stays until an administrator acknowledges it.
type Clamp struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	BindingID      uuid.UUID
	RuleID         string
	// RuleVersion is the version that narrowed the range.
	RuleVersion int
	Param       string
	// From is the customer's value; To is where the new version put it.
	From float64
	To   float64

	ClampedAt time.Time
	// AcknowledgedAt is zero while the move is outstanding.
	AcknowledgedAt time.Time
	AcknowledgedBy string
}

// Outstanding reports whether the move is still waiting to be seen.
func (c Clamp) Outstanding() bool { return c.AcknowledgedAt.IsZero() }

// ClampBinding returns the binding as the definition honours it, and the moves made.
// A parameter the definition does not offer is dropped and recorded as a move to the shipped value.
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

// ClampBindings reads a whole customer's tuning against the current pack.
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

// clampOf records one move under a fresh id; the stored row is keyed on binding, parameter and
// version, so re-reading an upgrade keeps the first record.
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
