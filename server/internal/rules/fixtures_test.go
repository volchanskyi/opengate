package rules

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/settings"
)

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

func orgBinding(org uuid.UUID, ruleID string, params map[string]float64) Binding {
	return newBinding(org, ruleID, settings.LevelOrganization, org, params)
}

func targeted(b Binding, selector Selector, precedence int) Binding {
	b.Selector, b.Precedence = selector, precedence
	return b
}

func threshold(v float64) map[string]float64 {
	return map[string]float64{"threshold": v}
}

func diskCritical(t *testing.T) Definition {
	t.Helper()
	return shippedRule(t, "disk-critical")
}

func shippedRule(t *testing.T, id string) Definition {
	t.Helper()
	cat, err := Embedded()
	require.NoError(t, err)
	def, ok := cat.Lookup(id)
	require.True(t, ok, "the shipped catalogue must contain %s", id)
	return def
}

func catalogueWith(t *testing.T, defs ...Definition) *Catalogue {
	t.Helper()
	cat := &Catalogue{byID: make(map[string]Definition, len(defs))}
	for _, def := range defs {
		cat.byID[def.ID] = def
		cat.order = append(cat.order, def.ID)
	}
	return cat
}

func refusesBinding(t *testing.T, def Definition, b Binding, want error, because string) {
	t.Helper()
	err := ValidateBinding(def, b)
	require.Errorf(t, err, "%s must be refused", because)
	require.ErrorIsf(t, err, want, "%s must be refused as %v", because, want)
}

func mustListBindings(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) []Binding {
	t.Helper()
	got, err := s.ListBindings(ctx, org)
	require.NoError(t, err)
	return got
}

func mustCountUnsupported(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) map[string]int {
	t.Helper()
	got, err := s.CountUnsupported(ctx, org)
	require.NoError(t, err)
	return got
}

func mustListRollouts(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) map[string]Rollout {
	t.Helper()
	got, err := s.ListRollouts(ctx, org)
	require.NoError(t, err)
	return got
}
