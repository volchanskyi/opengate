package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundsContainBothOfTheirEnds(t *testing.T) {
	t.Parallel()

	b := Bounds{Min: 50, Max: 99}
	assert.True(t, b.Contains(50), "the minimum is inside the bounds")
	assert.True(t, b.Contains(99), "the maximum is inside the bounds")
	assert.True(t, b.Contains(75))
	assert.False(t, b.Contains(49.9))
	assert.False(t, b.Contains(99.1))
}

func TestBoundsRenderAsPlainNumbers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "[50, 99]", Bounds{Min: 50, Max: 99}.String())
	assert.Equal(t, "[0.5, 1.25]", Bounds{Min: 0.5, Max: 1.25}.String())
}

func TestCatalogueAcceptsARuleIDOfExactlyTheMaximumLength(t *testing.T) {
	t.Parallel()

	_, err := loadEdited(t, "id: disk-critical", "id: "+strings.Repeat("a", maxRuleIDLen))
	assert.NoError(t, err, "an id of exactly the maximum length is a legal id")

	_, err = loadEdited(t, "id: disk-critical", "id: "+strings.Repeat("a", maxRuleIDLen+1))
	assert.Error(t, err, "one character past the maximum is refused")
}

func TestCatalogueAcceptsATunablePinnedToOneValue(t *testing.T) {
	t.Parallel()

	_, err := loadEdited(t, "threshold: {min: 50, max: 99}", "threshold: {min: 90, max: 90}")
	assert.NoError(t, err, "bounds whose ends are equal pin the parameter, they do not invert it")
}

func TestAShippedValueOutsideItsBoundsIsReportedWithThePlainNumbers(t *testing.T) {
	t.Parallel()

	_, err := loadEdited(t, "threshold: {min: 50, max: 99}", "threshold: {min: 95, max: 99}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "of 90 ", "the shipped value is printed as the author wrote it")
	assert.Contains(t, err.Error(), "[95, 99]", "the declared range is printed as the author wrote it")
}

func TestARefusalNamesTheRuleByIDOrByPosition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, old, replacement, want string
	}{
		{"by id", "version: 1", "version: 0", "rule disk-critical:"},
		{"by position", "id: disk-critical", `id: ""`, "rule #0:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadEdited(t, tc.old, tc.replacement)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func loadEdited(t *testing.T, old, replacement string) (*Catalogue, error) {
	t.Helper()
	return loadFixture(t, strings.Replace(validYAML, old, replacement, 1))
}
