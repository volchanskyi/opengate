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

// The limits a profile declares are read by one evaluator, and that evaluator
// reads canonical rows. On the venues that run no browser-side generator there
// are no canonical rows at all — what the night produces is this bundle — so
// something has to turn one into the other, and it has to be exercised against
// a bundle the harness actually wrote rather than against a hand-written copy
// of what one looks like.

// canonicalRow is as much of a row as the evaluator reads.
type canonicalRow struct {
	Source       string   `json:"source"`
	Scenario     string   `json:"scenario"`
	Phase        string   `json:"phase"`
	ErrorRate    *float64 `json:"error_rate"`
	LatencyP95Ms *float64 `json:"latency_p95_ms"`
}

// rowsFromRealBundle builds a run's evidence the way a night does, writes it
// through the validator — an incomplete bundle never reaches disk — and runs
// the script the workflows run against the file that lands.
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

// exitOutput is whatever a failed script said on its error stream, so a broken
// case reads as the refusal the script printed rather than as an exit status.
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

	// The three series the machine-side limits name, and nothing else: a row
	// the evaluator never reads is the decoration this exists to remove.
	assert.ElementsMatch(t, []string{"aggregate", "connect", "register"}, keysOf(byPhase))

	require.NotNil(t, byPhase["aggregate"].ErrorRate)
	assert.InDelta(t, 1.0/3.0, *byPhase["aggregate"].ErrorRate, 0.0001)
	require.NotNil(t, byPhase["connect"].LatencyP95Ms)
	assert.Positive(t, *byPhase["connect"].LatencyP95Ms)
	require.NotNil(t, byPhase["register"].LatencyP95Ms)
	assert.Positive(t, *byPhase["register"].LatencyP95Ms)
}

// keysOf is the set of phases the rows name.
func keysOf(rows map[string]canonicalRow) []string {
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	return names
}
