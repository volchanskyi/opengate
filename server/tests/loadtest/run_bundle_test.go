package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The harness's own account of a run has to become a bundle, or the run's
// evidence is a block of text in a workflow log that nothing can read back.

func harnessResults() []agentResult {
	return []agentResult{
		{connectDur: 10 * time.Millisecond, handshakeDur: 20 * time.Millisecond, registerDur: 5 * time.Millisecond},
		{connectDur: 12 * time.Millisecond, handshakeDur: 22 * time.Millisecond, registerDur: 6 * time.Millisecond},
		{err: errors.New("dial: timeout")},
	}
}

// bundleFrom builds a run's evidence from the results it produced, so each case
// below differs only in the run it describes.
func bundleFrom(t *testing.T, results []agentResult, withProfile bool) *Bundle {
	t.Helper()
	in := runBundleInputs{
		Results:    results,
		StartedAt:  time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		Total:      3 * time.Second,
		AgentCount: len(results),
		Target:     "opengate-staging-server:9090",
	}
	if withProfile {
		p, err := ParseProfile([]byte(minimalProfile))
		require.NoError(t, err)
		in.Profile = p
	}
	return buildRunBundle(in)
}

func TestARunBecomesACompleteBundle(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	require.NoError(t, bundle.Validate())
	assert.Equal(t, "normal", bundle.Run.ProfileName)
	assert.Equal(t, FamilyNormal, bundle.Run.Family)
	assert.Equal(t, bundle.Run.StartedAt.Add(3*time.Second), bundle.Run.FinishedAt)
}

// A run with no profile still produces a bundle. The alternative is evidence
// that exists only when somebody remembered a flag.
func TestABundleIsProducedWithoutAProfile(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), false)

	assert.NoError(t, bundle.Validate())
	assert.Equal(t, "ad-hoc", bundle.Run.ProfileName)
}

// Offered and achieved are both recorded, because a harness that could only
// connect a third of the fleet reads exactly like a server that refused two
// thirds of it.
func TestTheBundleRecordsWhatWasOfferedAndWhatArrived(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	require.Len(t, bundle.Phases, 1)
	phase := bundle.Phases[0]
	assert.Equal(t, 3, phase.OfferedConnectedAgents)
	assert.Equal(t, 2, phase.AchievedConnectedAgents)
	assert.InDelta(t, 1.0/3.0, phase.ErrorRate, 0.001)
}

// Each phase's tail travels separately. Folding them into one aggregate hides
// which of the three a slow run was slow in, and they are three different
// pieces of work.
func TestTheBundleCarriesThePerPhaseLatencies(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	series := map[string]bool{}
	for _, observation := range bundle.Observations {
		series[observation.Series] = true
	}
	for _, want := range []string{"connect_p95_ms", "handshake_p95_ms", "register_p95_ms"} {
		assert.True(t, series[want], "bundle must observe %s", want)
	}
}

// A run where every machine failed is invalid, not merely bad: nothing about
// the server was measured, so it must not move a window median. One where they
// all connected is valid.
func TestTheVerdictFollowsWhetherAnythingWasMeasured(t *testing.T) {
	nothing := bundleFrom(t, []agentResult{{err: errors.New("dial: timeout")}}, true)
	assert.Equal(t, ResultInvalid, nothing.Verdict.Result)
	assert.False(t, nothing.Verdict.EntersTrend())

	healthy := bundleFrom(t, []agentResult{
		{connectDur: time.Millisecond, handshakeDur: time.Millisecond, registerDur: time.Millisecond},
	}, true)
	assert.Equal(t, ResultValid, healthy.Verdict.Result)
}

func TestWriteRunBundlePutsTheEvidenceOnDisk(t *testing.T) {
	dir := t.TempDir()
	p, err := ParseProfile([]byte(minimalProfile))
	require.NoError(t, err)

	require.NoError(t, writeRunBundle(dir, p, harnessResults(),
		time.Now(), time.Second, 3, "opengate-staging-server:9090"))

	read, err := LoadBundle(filepath.Join(dir, "bundle.json"))
	require.NoError(t, err)
	assert.NoError(t, read.Validate())
}
