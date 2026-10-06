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

func TestValidateBindingRejectsAValueOutsideTheRulesBounds(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()
	for _, value := range []float64{49, 100} {
		refusesBinding(t, def, orgBinding(org, def.ID, threshold(value)),
			ErrParamOutOfBounds, fmt.Sprintf("a threshold of %v", value))
	}
}

func TestValidateBindingRejectsAParameterTheRuleDoesNotDeclareTunable(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()

	// window_secs is a real grammar field disk-critical does not offer; metric is no parameter.
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

	refusesBinding(t, def, newBinding(org, def.ID, settings.LevelShipped, org, nil),
		ErrInvalidLevel, "a binding filed on the ladder's floor")
}

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
	assert.False(t, Selector{"role": "file-server", "env": "staging"}.Matches(tags))
	assert.False(t, Selector{"role": "file-server"}.Matches(nil))
}

func TestBindingErrorsAreTyped(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()
	err := ValidateBinding(def,
		newBinding(org, def.ID, settings.LevelDevice, uuid.New(), threshold(1000)))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParamOutOfBounds))
}
