package authority

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectPrintsRolesAndFingerprintsAndNoKey(t *testing.T) {
	lines, err := Inspect(laidOut(t, kit(t), PhaseChangeover, LayoutOptions{}))
	require.NoError(t, err)
	text := strings.Join(lines, "\n")
	assert.Contains(t, text, "issuer")
	assert.Contains(t, text, "accepted")
	assert.Contains(t, text, "chain")
	assert.NotContains(t, text, "PRIVATE KEY")
	for _, line := range lines {
		assert.Regexp(t, `[0-9a-f]{12}`, line, "each role names its fingerprint")
	}
}
