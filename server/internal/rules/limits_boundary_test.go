package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectorAcceptsItsLimitsExactly(t *testing.T) {
	t.Parallel()

	require.Len(t, selectorsPast(0)["tag count"], maxSelectorTags)
	for name, selector := range selectorsPast(0) {
		assert.NoError(t, selector.Validate(), "a selector at exactly the limit is allowed: "+name)
	}
}

func TestSelectorRefusesOnePastEachLimit(t *testing.T) {
	t.Parallel()

	for name, selector := range selectorsPast(1) {
		assert.ErrorIs(t, selector.Validate(), ErrInvalidSelector,
			"a selector one past the limit is refused: "+name)
	}
}

func selectorsPast(over int) map[string]Selector {
	tags := Selector{}
	for i := range maxSelectorTags + over {
		tags[string(rune('a'+i))] = "x"
	}
	return map[string]Selector{
		"tag count": tags,
		"tag key":   {strings.Repeat("k", maxSelectorKeyLen+over): "x"},
		"tag value": {"role": strings.Repeat("v", maxSelectorValueLen+over)},
	}
}

func TestABindingAtTheParameterLimitIsJudgedOnItsParameters(t *testing.T) {
	t.Parallel()

	def, org := diskCritical(t), uuid.New()

	params := map[string]float64{}
	for i := range maxBindingParams {
		params["made-up-"+strconv.Itoa(i)] = 1
	}
	require.Len(t, params, maxBindingParams)

	err := ValidateBinding(def, orgBinding(org, def.ID, params))
	require.ErrorIs(t, err, ErrParamNotTunable)
	assert.Contains(t, err.Error(), "made-up-",
		"the refusal names the parameter that cannot be set, not how many there were")
}

func TestValidateLabelAcceptsItsLimitsExactly(t *testing.T) {
	t.Parallel()

	org := uuid.New()

	for _, tc := range []struct {
		name       string
		key, value string
	}{
		{"key", strings.Repeat("k", maxSelectorKeyLen), "production"},
		{"value", "env", strings.Repeat("v", maxSelectorValueLen)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, ValidateLabel(newLabel(org, tc.key, tc.value)),
				"a label part of exactly the maximum length is allowed")
		})
	}
}

func TestValidateRolloutAcceptsBothEndsOfItsRange(t *testing.T) {
	t.Parallel()

	org := uuid.New()

	for _, tc := range []struct {
		name   string
		adjust func(*Rollout)
	}{
		{"reaching nobody", func(r *Rollout) { r.RolloutPercent = 0 }},
		{"finished", func(r *Rollout) { r.RolloutPercent = 100 }},
		{"longest canary group", func(r *Rollout) { r.CanaryGroup = strings.Repeat("g", maxCanaryGroupLen) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rollout := DefaultRollout(org, "disk-critical")
			tc.adjust(&rollout)
			assert.NoError(t, ValidateRollout(rollout))
		})
	}
}

func TestSaturatingAddReturnsTheSumWhenItDoesNotWrap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uint64(7), saturatingAdd(7, 0), "adding nothing leaves the total alone")
	assert.Equal(t, uint64(0), saturatingAdd(0, 0))
	assert.Equal(t, uint64(12), saturatingAdd(7, 5))
	assert.Equal(t, ^uint64(0), saturatingAdd(^uint64(0), 1), "a wrap saturates instead")
}
