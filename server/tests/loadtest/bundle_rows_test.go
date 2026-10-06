package main

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalRow is as much of a row as the evaluator reads.
type canonicalRow struct {
	Source       string   `json:"source"`
	Scenario     string   `json:"scenario"`
	Phase        string   `json:"phase"`
	ErrorRate    *float64 `json:"error_rate"`
	LatencyP95Ms *float64 `json:"latency_p95_ms"`
}

func rowsFromRealBundle(t *testing.T) []canonicalRow {
	t.Helper()

	in := measuredRun()
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)
	in.Registration = &reading

	dir := t.TempDir()
	path, err := buildRunBundle(in).WriteTo(dir)
	require.NoError(t, err, "the bundle this reads has to be one a run could have written")

	script := filepath.Join(repoRoot(t), "scripts", "loadtest-bundle-rows.sh")
	out, err := exec.Command(script, path).Output() // #nosec G204 -- both paths are this test's own
	require.NoErrorf(t, err, "reading rows off %s failed: %s", path, exitOutput(err))

	var rows []canonicalRow
	require.NoError(t, json.Unmarshal(out, &rows))
	return rows
}

func exitOutput(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(exit.Stderr)
	}
	return ""
}

func TestABundleBecomesTheRowsTheEvaluatorReads(t *testing.T) {
	rows := rowsFromRealBundle(t)

	byPhase := map[string]canonicalRow{}
	for _, row := range rows {
		assert.Equal(t, "quic", row.Source)
		assert.Equal(t, "quic-agents", row.Scenario)
		byPhase[row.Phase] = row
	}

	assert.ElementsMatch(t, []string{"aggregate", "connect", "register"}, keysOf(byPhase))

	require.NotNil(t, byPhase["aggregate"].ErrorRate)
	assert.InDelta(t, 1.0/3.0, *byPhase["aggregate"].ErrorRate, 0.0001)
	require.NotNil(t, byPhase["connect"].LatencyP95Ms)
	assert.Positive(t, *byPhase["connect"].LatencyP95Ms)
	require.NotNil(t, byPhase["register"].LatencyP95Ms)
	assert.Positive(t, *byPhase["register"].LatencyP95Ms)
}

func keysOf(rows map[string]canonicalRow) []string {
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	return names
}
