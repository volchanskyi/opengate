package rules

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/settings"
)

func TestValidateBindingAcceptsAValueInsideTheRulesBounds(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	b := orgBinding(uuid.New(), def.ID, map[string]float64{"threshold": 95, "clear": 90})
	require.NoError(t, ValidateBinding(def, b))
}

// A binding is validated on write, so a value the rule's author never allowed
// is refused where an operator can still see why — not at 5000 endpoints later.
func TestValidateBindingRejectsAValueOutsideTheRulesBounds(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()
	for _, value := range []float64{49, 100} {
		refusesBinding(t, def, orgBinding(org, def.ID, threshold(value)),
			ErrParamOutOfBounds, fmt.Sprintf("a threshold of %v", value))
	}
}

// A rule decides what about it is tunable. Anything else is not a setting the
// operator is allowed to reach, whether or not the grammar could carry it.
func TestValidateBindingRejectsAParameterTheRuleDoesNotDeclareTunable(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()

	// window_secs is a real grammar field that disk-critical does not offer;
	// metric is not a parameter at all. Both are refused the same way.
	for _, name := range []string{"window_secs", "metric"} {
		refusesBinding(t, def, orgBinding(org, def.ID, map[string]float64{name: 300}),
			ErrParamNotTunable, name)
	}
}

func TestValidateBindingRejectsAMismatchedOrUnusableRule(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()

	refusesBinding(t, def, orgBinding(org, "cpu-saturated", nil),
		ErrRuleMismatch, "a binding naming a different rule")

	// The tenancy ladder's floor is not a rung anything can be stored against.
	refusesBinding(t, def, newBinding(org, def.ID, settings.LevelShipped, org, nil),
		ErrInvalidLevel, "a binding filed on the ladder's floor")
}

// A selector is a targeting aid, not an authoring surface: it is a small set of
// exact tag matches, and anything larger is refused rather than stored.
func TestValidateBindingBoundsTheSelector(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()

	oversized := Selector{}
	for i := range maxSelectorTags + 1 {
		oversized[string(rune('a'+i))] = "x"
	}

	tooLarge := map[string]Selector{
		"more tags than a selector may name": oversized,
		"a tag value past its bound":         {"role": strings.Repeat("x", maxSelectorValueLen+1)},
		"an empty tag key":                   {"": "x"},
	}
	for name, selector := range tooLarge {
		refusesBinding(t, def, targeted(orgBinding(org, def.ID, nil), selector, 0),
			ErrInvalidSelector, name)
	}
}

func TestSelectorMatchesEveryTagItNames(t *testing.T) {
	t.Parallel()

	tags := map[string]string{"role": "file-server", "env": "prod"}

	assert.True(t, Selector(nil).Matches(tags), "a binding with no selector covers the whole level")
	assert.True(t, Selector{"role": "file-server"}.Matches(tags))
	assert.True(t, Selector{"role": "file-server", "env": "prod"}.Matches(tags))
	assert.False(t, Selector{"role": "workstation"}.Matches(tags))
	// Every named tag must match: a selector is a conjunction, not a hint.
	assert.False(t, Selector{"role": "file-server", "env": "staging"}.Matches(tags))
	assert.False(t, Selector{"role": "file-server"}.Matches(nil))
}

// The typed errors are what an API layer turns into a 4xx an operator can read,
// so they must survive wrapping.
func TestBindingErrorsAreTyped(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()
	err := ValidateBinding(def,
		newBinding(org, def.ID, settings.LevelDevice, uuid.New(), threshold(1000)))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParamOutOfBounds))
}
