package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every bound in this package is a refusal an operator runs into, so the value
// that matters is the one at the limit rather than the one well past it. A
// selector of exactly the maximum number of tags, a key of exactly the maximum
// length, a percentage of exactly zero: each is something somebody legitimately
// asks for, and each is refused by an off-by-one that a test using "the limit
// plus fifty" cannot see. The cases below sit on the limit itself.

// A selector may name up to the maximum number of tags, and a key and a value
// may each run to exactly their maximum length.
func TestSelectorAcceptsItsLimitsExactly(t *testing.T) {
	t.Parallel()

	atTheTagLimit := Selector{}
	for i := range maxSelectorTags {
		atTheTagLimit[string(rune('a'+i))] = "x"
	}
	require.Len(t, atTheTagLimit, maxSelectorTags)
	assert.NoError(t, atTheTagLimit.Validate(),
		"a selector naming exactly the maximum number of tags is allowed")

	longestKey := strings.Repeat("k", maxSelectorKeyLen)
	assert.NoError(t, Selector{longestKey: "x"}.Validate(),
		"a tag key of exactly the maximum length is allowed")

	longestValue := strings.Repeat("v", maxSelectorValueLen)
	assert.NoError(t, Selector{"role": longestValue}.Validate(),
		"a tag value of exactly the maximum length is allowed")
}

// One past each limit is refused, which is the other side of the same pair. The
// key length is the one with no case of its own until now: a selector could
// carry a key of any length as long as its value was short enough.
func TestSelectorRefusesOnePastEachLimit(t *testing.T) {
	t.Parallel()

	pastTheTagLimit := Selector{}
	for i := range maxSelectorTags + 1 {
		pastTheTagLimit[string(rune('a'+i))] = "x"
	}
	assert.ErrorIs(t, pastTheTagLimit.Validate(), ErrInvalidSelector)

	assert.ErrorIs(t, Selector{strings.Repeat("k", maxSelectorKeyLen+1): "x"}.Validate(),
		ErrInvalidSelector, "a tag key one character past the maximum is refused")

	assert.ErrorIs(t, Selector{"role": strings.Repeat("v", maxSelectorValueLen+1)}.Validate(),
		ErrInvalidSelector, "a tag value one character past the maximum is refused")
}

// A binding carrying exactly the maximum number of parameters is past no limit,
// so whatever it is refused for has to be the parameters themselves. The error
// an operator reads must name the parameter it could not set, not the count —
// "8 parameters, at most 8" is a refusal nobody can act on.
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

// A label carries the selector's own limits, and the same pair applies.
func TestValidateLabelAcceptsItsLimitsExactly(t *testing.T) {
	t.Parallel()

	org := uuid.New()

	assert.NoError(t, ValidateLabel(Label{
		OrganizationID: org,
		Key:            strings.Repeat("k", maxSelectorKeyLen),
		Value:          "production",
	}), "a label key of exactly the maximum length is allowed")

	assert.NoError(t, ValidateLabel(Label{
		OrganizationID: org,
		Key:            "env",
		Value:          strings.Repeat("v", maxSelectorValueLen),
	}), "a label value of exactly the maximum length is allowed")
}

// Nought and a hundred are both reachable rollouts: nought is a rule that is on
// but reaching nobody, which is what a rollout starts at, and a hundred is one
// that has finished. Refusing either would make the two ends of a rollout
// unstorable.
func TestValidateRolloutAcceptsBothEndsOfItsRange(t *testing.T) {
	t.Parallel()

	org := uuid.New()

	none := DefaultRollout(org, "disk-critical")
	none.RolloutPercent = 0
	assert.NoError(t, ValidateRollout(none), "a rollout reaching nobody is storable")

	all := DefaultRollout(org, "disk-critical")
	all.RolloutPercent = 100
	assert.NoError(t, ValidateRollout(all), "a rollout that has finished is storable")

	// And a canary group of exactly the maximum length is a name somebody may
	// have given a group, so it is stored rather than refused.
	named := DefaultRollout(org, "disk-critical")
	named.CanaryGroup = strings.Repeat("g", maxCanaryGroupLen)
	assert.NoError(t, ValidateRollout(named))
}

// The budget guard exists so a catalogue cannot wrap the total past zero and
// pass a gate it should fail. Adding nothing is the case that separates a
// saturating add from one that reports every sum as an overflow.
func TestSaturatingAddReturnsTheSumWhenItDoesNotWrap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uint64(7), saturatingAdd(7, 0), "adding nothing leaves the total alone")
	assert.Equal(t, uint64(0), saturatingAdd(0, 0))
	assert.Equal(t, uint64(12), saturatingAdd(7, 5))
	assert.Equal(t, ^uint64(0), saturatingAdd(^uint64(0), 1), "a wrap saturates instead")
}

// Bounds are inclusive at both ends: a rule declaring [50, 99] means an operator
// may set fifty and may set ninety-nine. An exclusive comparison here would make
// the numbers the rule's author wrote down the only two nobody can choose.
func TestBoundsContainBothOfTheirEnds(t *testing.T) {
	t.Parallel()

	b := Bounds{Min: 50, Max: 99}
	assert.True(t, b.Contains(50), "the minimum is inside the bounds")
	assert.True(t, b.Contains(99), "the maximum is inside the bounds")
	assert.True(t, b.Contains(75))
	assert.False(t, b.Contains(49.9))
	assert.False(t, b.Contains(99.1))
}

// The range is rendered for an error somebody has to act on, so it prints the
// numbers the rule's author wrote rather than an exponent form of them.
func TestBoundsRenderAsPlainNumbers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "[50, 99]", Bounds{Min: 50, Max: 99}.String())
	assert.Equal(t, "[0.5, 1.25]", Bounds{Min: 0.5, Max: 1.25}.String())
}

// A rule id of exactly the maximum length is a legal id, so the catalogue loads
// it. The bound is on ids longer than the maximum, not on ids that reach it.
func TestCatalogueAcceptsARuleIDOfExactlyTheMaximumLength(t *testing.T) {
	t.Parallel()

	longest := strings.Repeat("a", maxRuleIDLen)
	_, err := loadFixture(t, strings.Replace(validYAML, "id: disk-critical", "id: "+longest, 1))
	assert.NoError(t, err, "an id of exactly the maximum length is a legal id")

	_, err = loadFixture(t, strings.Replace(validYAML, "id: disk-critical",
		"id: "+strings.Repeat("a", maxRuleIDLen+1), 1))
	assert.Error(t, err, "one character past the maximum is refused")
}

// A parameter pinned to a single value has bounds whose ends are equal. That is
// a rule author saying "this is not yours to move", which is a thing they are
// allowed to say — it is inverted bounds that are the contradiction.
func TestCatalogueAcceptsATunablePinnedToOneValue(t *testing.T) {
	t.Parallel()

	pinned := strings.Replace(validYAML, "threshold: {min: 50, max: 99}", "threshold: {min: 90, max: 90}", 1)
	_, err := loadFixture(t, pinned)
	assert.NoError(t, err, "bounds whose ends are equal pin the parameter, they do not invert it")
}

// The refusal an author reads names the number they shipped and the range they
// declared, both as they wrote them. Rendered any other way — an exponent form,
// say — the message stops matching the file it is about.
func TestAShippedValueOutsideItsBoundsIsReportedWithThePlainNumbers(t *testing.T) {
	t.Parallel()

	_, err := loadFixture(t, strings.Replace(validYAML,
		"threshold: {min: 50, max: 99}", "threshold: {min: 95, max: 99}", 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "of 90 ", "the shipped value is printed as the author wrote it")
	assert.Contains(t, err.Error(), "[95, 99]", "the declared range is printed as the author wrote it")
}

// A refusal names the rule it is about: by its id when the rule has one, and by
// its position in the file when it does not. Those are the only two ways an
// author can find the rule the message means, and swapping them leaves a
// refusal that points at nothing.
func TestARefusalNamesTheRuleByIDOrByPosition(t *testing.T) {
	t.Parallel()

	// A rule with an id, broken elsewhere, is named by its id.
	named := strings.Replace(validYAML, "version: 1", "version: 0", 1)
	_, err := loadFixture(t, named)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule disk-critical:",
		"a rule that has an id is named by it")

	// A rule with no id at all is named by where it sits, because there is
	// nothing else to call it.
	anonymous := strings.Replace(validYAML, "id: disk-critical", `id: ""`, 1)
	_, err = loadFixture(t, anonymous)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule #0:",
		"a rule with no id is named by its position in the file")
}
