package main

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// harnessResults is two arrivals, each carrying the registration moment, and one failed dial.
func harnessResults() []agentResult {
	start := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	return []agentResult{
		{connectDur: 10 * time.Millisecond, handshakeDur: 20 * time.Millisecond,
			registerDur: 5 * time.Millisecond, arrivedAt: start.Add(35 * time.Millisecond)},
		{connectDur: 12 * time.Millisecond, handshakeDur: 22 * time.Millisecond,
			registerDur: 6 * time.Millisecond, arrivedAt: start.Add(40 * time.Millisecond)},
		{err: errors.New("dial: timeout")},
	}
}

func bundleFrom(t *testing.T, results []agentResult, withProfile bool) *Bundle {
	t.Helper()
	in := measuredRun()
	in.Results = results
	in.AgentCount = len(results)
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

func TestABundleIsProducedWithoutAProfile(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), false)

	assert.NoError(t, bundle.Validate())
	assert.Equal(t, "ad-hoc", bundle.Run.ProfileName)
}

func TestTheBundleRecordsWhatWasOfferedAndWhatArrived(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	require.Len(t, bundle.Phases, 1)
	phase := bundle.Phases[0]
	assert.Equal(t, 3, phase.OfferedConnectedAgents)
	assert.Equal(t, 2, phase.AchievedConnectedAgents)
	assert.InDelta(t, 1.0/3.0, phase.ErrorRate, 0.001)
}

func TestTheBundleCarriesThePerStageLatencies(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	series := observedSeries(bundle)
	for _, want := range []string{"connect_p95_ms", "handshake_p95_ms"} {
		assert.True(t, series[want], "bundle must observe %s", want)
	}
}

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
	assert.True(t, series["register_p50_ms"])
	assert.True(t, series["db_pool_in_use"])
	assert.True(t, series["register_rejected"])
}

func TestARegistrationTailPastTheTopBucketIsMarkedPastTheScale(t *testing.T) {
	in := runBundleInputs{
		Results:    harnessResults(),
		StartedAt:  time.Date(2026, 9, 29, 13, 50, 0, 0, time.UTC),
		Total:      3 * time.Second,
		AgentCount: 3,
		Target:     "compose stack",
	}
	in.Registration = &ServerRegistration{
		Accepted: 100,
		Buckets: []bucketBound{
			{le: 1, count: 40},
			{le: 60, count: 90},
			{le: math.Inf(1), count: 100},
		},
	}

	bundle := buildRunBundle(in)
	marks := map[string]string{}
	values := map[string]float64{}
	for _, o := range bundle.Observations {
		marks[o.Series] = o.Labels["reading"]
		values[o.Series] = o.Value
	}
	assert.Equal(t, "past the scale", marks["register_p95_ms"],
		"a tail in the open bucket is marked as a floor")
	assert.InDelta(t, 60000.0, values["register_p95_ms"], 0.001,
		"and carries the widest bound the server can describe")
	assert.Empty(t, marks["register_p50_ms"], "a middle case inside the scale is a reading")
}

func TestABundleReportsTheProfilesOwnPhases(t *testing.T) {
	in := measuredRun()
	in.Phases = []PhaseResult{
		{Name: "ramp", StartedAt: time.Now(), FinishedAt: time.Now().Add(time.Minute), AchievedConnectedAgents: 250},
		{Name: "steady", StartedAt: time.Now(), FinishedAt: time.Now().Add(time.Minute), AchievedConnectedAgents: 500},
	}
	bundle := buildRunBundle(in)

	require.Len(t, bundle.Phases, 2)
	assert.Equal(t, "ramp", bundle.Phases[0].Name)
	assert.Equal(t, "steady", bundle.Phases[1].Name)
}

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

	in := measuredRun()
	in.Fixture = &built
	bundle := buildRunBundle(in)

	assert.Equal(t, FixtureLarge, bundle.Fixture.Size)
	assert.Equal(t, 2, bundle.Fixture.Customers)
	assert.Equal(t, 9, bundle.Fixture.Sites)
	assert.Equal(t, 2, bundle.Fixture.Users)
	assert.Equal(t, plan.Devices, bundle.Fixture.PlannedDevices)
}

func observedSeries(bundle *Bundle) map[string]bool {
	series := map[string]bool{}
	for _, observation := range bundle.Observations {
		series[observation.Series] = true
	}
	return series
}

func TestTheVerdictFollowsWhetherAnythingWasMeasured(t *testing.T) {
	nothing := bundleFrom(t, []agentResult{{err: errors.New("dial: timeout")}}, true)
	assert.Equal(t, ResultInvalid, nothing.Verdict.Result)
	assert.False(t, nothing.Verdict.EntersTrend())

	healthy := bundleFrom(t, []agentResult{
		{connectDur: time.Millisecond, handshakeDur: time.Millisecond, registerDur: time.Millisecond,
			arrivedAt: time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)},
	}, true)
	assert.Equal(t, ResultValid, healthy.Verdict.Result)
}

func TestWriteRunBundlePutsTheEvidenceOnDisk(t *testing.T) {
	dir := t.TempDir()
	p, err := ParseProfile([]byte(minimalProfile))
	require.NoError(t, err)

	in := measuredRun()
	in.Profile = p
	in.StartedAt = time.Now()
	require.NoError(t, writeRunBundle(buildRunBundle(in), dir))

	read, err := LoadBundle(filepath.Join(dir, "bundle.json"))
	require.NoError(t, err)
	assert.NoError(t, read.Validate())
}

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

func TestAMachineSeveredAfterArrivingIsStillOneThatArrived(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 13, 12, 32, 0, 0, time.UTC)
	results := []agentResult{
		// Survived to the wind-down.
		{connectDur: 10 * time.Millisecond, handshakeDur: 4 * time.Millisecond,
			registerDur: 5 * time.Millisecond, arrivedAt: start.Add(100 * time.Millisecond)},
		// Arrived, then lost its connection under load.
		{connectDur: 200 * time.Millisecond, handshakeDur: 90 * time.Millisecond,
			registerDur: 60 * time.Millisecond, arrivedAt: start.Add(200 * time.Millisecond),
			err: ErrHeldPeerGone},
		// Never got in.
		{err: errors.New("dial: timeout")},
	}

	bundle := buildRunBundle(runBundleInputs{
		Results:    results,
		StartedAt:  start,
		Total:      5 * time.Minute,
		AgentCount: len(results),
		Target:     "opengate-perf-server:9090",
	})

	assert.Equal(t, 2, bundle.Fixture.Devices,
		"the fleet that exists is the machines that registered, not the ones that outlived the load")

	series := map[string]float64{}
	for _, observation := range bundle.Observations {
		series[observation.Series] = observation.Value
	}
	assert.InDelta(t, 200.0, series["connect_p95_ms"], 0.001,
		"a machine that took 200ms to connect and was later severed still took 200ms to connect")
	assert.InDelta(t, 90.0, series["handshake_p95_ms"], 0.001)
	assert.InDelta(t, 1.0, series["agents_severed_mid_hold"], 0.001,
		"the severance is still counted, separately, where it belongs")
}

func TestTheBundleCountsMachinesSeveredMidHold(t *testing.T) {
	t.Parallel()

	held := buildRunBundle(runBundleInputs{
		Results:    []agentResult{{}, {}, {err: ErrHeldPeerGone}, {err: errors.New("dial: refused")}},
		StartedAt:  time.Now().Add(-time.Minute),
		Total:      time.Minute,
		AgentCount: 4,
		Target:     "127.0.0.1:9090",
	})

	assert.Equal(t, 1.0, observationValue(t, held, "agents_severed_mid_hold"),
		"only the machine whose hold was severed counts; a machine that never connected took nothing")
}

func observationValue(t *testing.T, bundle *Bundle, series string) float64 {
	t.Helper()
	for _, observation := range bundle.Observations {
		if observation.Series == series {
			return observation.Value
		}
	}
	require.FailNowf(t, "series not found", "the bundle carries no %q observation", series)
	return 0
}
