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

// Each stage's tail travels separately. Folding them into one aggregate hides
// which of them a slow run was slow in, and they are different pieces of work.
func TestTheBundleCarriesThePerStageLatencies(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	series := observedSeries(bundle)
	for _, want := range []string{"connect_p95_ms", "handshake_p95_ms"} {
		assert.True(t, series[want], "bundle must observe %s", want)
	}
}

// Registration is the server's figure or it is nothing. The harness's own clock
// stops when the frame reaches a local send buffer, and the row is written later
// somewhere else — so a number from here cannot move however slow that write
// becomes, and two ceilings sat on exactly that.
func TestABundleWithoutAServerReadingPublishesNoRegistrationFigure(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	assert.False(t, observedSeries(bundle)["register_p95_ms"],
		"a figure the harness cannot stand behind is worse than an absent one")
}

func TestABundleCarriesTheServersOwnRegistrationFigure(t *testing.T) {
	in := runBundleInputs{
		Results:    harnessResults(),
		StartedAt:  time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		Total:      3 * time.Second,
		AgentCount: 3,
		Target:     "opengate-staging-server:9090",
	}
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)
	in.Registration = &reading

	bundle := buildRunBundle(in)
	series := observedSeries(bundle)
	assert.True(t, series["register_p95_ms"])
	// The pool travels beside it: a registration queued behind a connection and
	// one executing slowly are the same latency until the pool says which.
	assert.True(t, series["db_pool_in_use"])
	assert.True(t, series["register_rejected"])
}

// The phases a run walked are the phases it reports.
func TestABundleReportsTheProfilesOwnPhases(t *testing.T) {
	in := runBundleInputs{
		Results:    harnessResults(),
		StartedAt:  time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		Total:      3 * time.Second,
		AgentCount: 3,
		Target:     "opengate-staging-server:9090",
		Phases: []PhaseResult{
			{Name: "ramp", StartedAt: time.Now(), FinishedAt: time.Now().Add(time.Minute), AchievedConnectedAgents: 250},
			{Name: "steady", StartedAt: time.Now(), FinishedAt: time.Now().Add(time.Minute), AchievedConnectedAgents: 500},
		},
	}
	bundle := buildRunBundle(in)

	require.Len(t, bundle.Phases, 2)
	assert.Equal(t, "ramp", bundle.Phases[0].Name)
	assert.Equal(t, "steady", bundle.Phases[1].Name)
}

// A run that built its own fleet says what is in it, rather than inferring the
// shape from how many machines it happened to dial.
func TestABundleCountsTheFixtureTheRunBuilt(t *testing.T) {
	plan, err := PlanFixture(FixtureLarge, 3)
	require.NoError(t, err)
	built := BuiltFixture{
		Size:           plan.Size,
		Customers:      []BuiltCustomer{{ID: "a"}, {ID: "b"}},
		Users:          []string{"one@x.invalid", "two@x.invalid"},
		Sites:          9,
		PlannedDevices: plan.Devices,
	}

	bundle := buildRunBundle(runBundleInputs{
		Results:    harnessResults(),
		StartedAt:  time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		Total:      time.Second,
		AgentCount: 3,
		Target:     "opengate-staging-server:9090",
		Fixture:    &built,
	})

	assert.Equal(t, FixtureLarge, bundle.Fixture.Size)
	assert.Equal(t, 2, bundle.Fixture.Customers)
	assert.Equal(t, 9, bundle.Fixture.Sites)
	assert.Equal(t, plan.Devices, bundle.Fixture.Devices)
}

// observedSeries is the set of series a bundle carries.
func observedSeries(bundle *Bundle) map[string]bool {
	series := map[string]bool{}
	for _, observation := range bundle.Observations {
		series[observation.Series] = true
	}
	return series
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

	require.NoError(t, writeRunBundle(buildRunBundle(runBundleInputs{
		Profile:    p,
		Results:    harnessResults(),
		StartedAt:  time.Now(),
		Total:      time.Second,
		AgentCount: 3,
		Target:     "opengate-staging-server:9090",
	}), dir))

	read, err := LoadBundle(filepath.Join(dir, "bundle.json"))
	require.NoError(t, err)
	assert.NoError(t, read.Validate())
}

// A phase named "connect" that ends when the run ends is not describing the
// connect. A held fleet's run is eight minutes of holding after a second of
// arriving, so a connect phase spanning the whole run reports the hold under
// the arrival's name — and anything reading the phase back for an arrival rate
// divides by the wrong number.
func TestTheConnectPhaseEndsWhenTheFleetIsUp(t *testing.T) {
	start := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	results := []agentResult{
		{connectDur: 10 * time.Millisecond, registerDur: 5 * time.Millisecond, arrivedAt: start.Add(180 * time.Millisecond)},
		{connectDur: 12 * time.Millisecond, registerDur: 6 * time.Millisecond, arrivedAt: start.Add(420 * time.Millisecond)},
	}
	bundle := buildRunBundle(runBundleInputs{
		Results:    results,
		StartedAt:  start,
		Total:      8 * time.Minute,
		AgentCount: len(results),
		Target:     "opengate-staging-server:9090",
	})

	require.Len(t, bundle.Phases, 1)
	phase := bundle.Phases[0]
	assert.Equal(t, "connect", phase.Name)
	assert.Equal(t, start.Add(420*time.Millisecond), phase.FinishedAt,
		"the connect ends at the last arrival; the hold that follows is not part of it")
	assert.Equal(t, start.Add(8*time.Minute), bundle.Run.FinishedAt,
		"the run still ends when it ended — only the phase is bounded to the arrival")
}
