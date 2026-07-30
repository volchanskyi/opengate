package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// healthTSPath is the web client's copy of the edge-health band boundaries.
const healthTSPath = "../../../web/src/features/devices/health.ts"

var tsThresholdRE = regexp.MustCompile(`export const (WATCH_THRESHOLD|ANOMALOUS_THRESHOLD) = ([0-9.]+);`)

// TestHealthBandThresholdsMatchWebClient keeps the two copies of the band
// boundaries in step. Go feeds them to the PromQL behind the dashboard rollup;
// TypeScript classifies each device badge in the grid and the detail panel.
// A silent drift would make the dashboard tiles disagree with the badges they
// link to, so moving one threshold without the other fails here.
func TestHealthBandThresholdsMatchWebClient(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Clean(healthTSPath))
	require.NoError(t, err, "the web client's health.ts must be readable from the server tree")

	found := map[string]float64{}
	for _, m := range tsThresholdRE.FindAllStringSubmatch(string(src), -1) {
		v, parseErr := strconv.ParseFloat(m[2], 64)
		require.NoError(t, parseErr, "threshold %s must be a number", m[1])
		found[m[1]] = v
	}

	require.Len(t, found, 2, "health.ts must declare exactly WATCH_THRESHOLD and ANOMALOUS_THRESHOLD")
	assert.Equal(t, watchThreshold, found["WATCH_THRESHOLD"],
		"watchThreshold in Go and WATCH_THRESHOLD in health.ts must agree")
	assert.Equal(t, anomalousThreshold, found["ANOMALOUS_THRESHOLD"],
		"anomalousThreshold in Go and ANOMALOUS_THRESHOLD in health.ts must agree")
	assert.Less(t, found["WATCH_THRESHOLD"], found["ANOMALOUS_THRESHOLD"],
		"watch must be the lower boundary")
}
