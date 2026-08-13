package rules

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/settings"
)

// Shared builders. Almost every case below is "one rule, filed on one rung of
// one customer's ladder, differing in a couple of fields", so they all start
// from the same two helpers and each case states only what it is about.

// newBinding builds a binding for one rule filed on one rung.
func newBinding(org uuid.UUID, ruleID string, level settings.Level, key uuid.UUID, params map[string]float64) Binding {
	return Binding{
		ID:             uuid.New(),
		OrganizationID: org,
		RuleID:         ruleID,
		Level:          level,
		LevelKey:       key,
		Params:         params,
	}
}

// orgBinding is the common case: a binding covering the whole customer.
func orgBinding(org uuid.UUID, ruleID string, params map[string]float64) Binding {
	return newBinding(org, ruleID, settings.LevelOrganization, org, params)
}

// targeted narrows a binding to the machines a selector names, at a stated
// precedence.
func targeted(b Binding, selector Selector, precedence int) Binding {
	b.Selector, b.Precedence = selector, precedence
	return b
}

// threshold is the single-parameter override most cases use.
func threshold(v float64) map[string]float64 {
	return map[string]float64{"threshold": v}
}

// diskCritical is the shipped rule the binding and resolution cases retune:
// threshold 90, tunable within [50, 99].
func diskCritical(t *testing.T) Definition {
	t.Helper()
	return shippedRule(t, "disk-critical")
}

// shippedRule returns one definition from the embedded catalogue.
func shippedRule(t *testing.T, id string) Definition {
	t.Helper()
	cat, err := Embedded()
	require.NoError(t, err)
	def, ok := cat.Lookup(id)
	require.True(t, ok, "the shipped catalogue must contain %s", id)
	return def
}

// refusesBinding asserts a binding is rejected, and rejected for the stated
// reason — the typed error is what an API layer turns into an answer an operator
// can act on, so the case is about which error, not merely that there was one.
func refusesBinding(t *testing.T, def Definition, b Binding, want error, because string) {
	t.Helper()
	err := ValidateBinding(def, b)
	require.Errorf(t, err, "%s must be refused", because)
	require.ErrorIsf(t, err, want, "%s must be refused as %v", because, want)
}

// mustListBindings reads one customer's bindings.
func mustListBindings(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) []Binding {
	t.Helper()
	got, err := s.ListBindings(ctx, org)
	require.NoError(t, err)
	return got
}

// mustCountUnsupported reads how many machines cannot evaluate each rule.
func mustCountUnsupported(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) map[string]int {
	t.Helper()
	got, err := s.CountUnsupported(ctx, org)
	require.NoError(t, err)
	return got
}

// mustListRollouts reads one customer's stored rollout state.
func mustListRollouts(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) map[string]Rollout {
	t.Helper()
	got, err := s.ListRollouts(ctx, org)
	require.NoError(t, err)
	return got
}
