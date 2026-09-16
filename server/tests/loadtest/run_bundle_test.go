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

// Two machines that got in and one that never did. The arrivals carry the
// moment they finished registering, because that is what a machine that
// connected, handshook and registered comes back with — a result holding three
// timings and no arrival is a shape no run produces.
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

// bundleFrom builds a run's evidence from the results it produced, so each case
// below differs only in the run it describes.
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
	// And the middle case beside the tail, because they answer different
	// questions about the same queue and only one of them reproduces where the
	// venue is driven hard. At the largest fleet the throwaway stack has been
	// shown to hold, two runs an hour apart under identical load read tails of
	// 5,773 and 9,443 ms while their middle cases read 239 and 255 — so the
	// tail there is the queue and the middle case is the write.
	assert.True(t, series["register_p50_ms"])
	// The pool travels beside it: a registration queued behind a connection and
	// one executing slowly are the same latency until the pool says which.
	assert.True(t, series["db_pool_in_use"])
	assert.True(t, series["register_rejected"])
}

// The phases a run walked are the phases it reports.
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

	in := measuredRun()
	in.Fixture = &built
	bundle := buildRunBundle(in)

	assert.Equal(t, FixtureLarge, bundle.Fixture.Size)
	assert.Equal(t, 2, bundle.Fixture.Customers)
	assert.Equal(t, 9, bundle.Fixture.Sites)
	assert.Equal(t, 2, bundle.Fixture.Users)
	assert.Equal(t, plan.Devices, bundle.Fixture.PlannedDevices)
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

// A machine severed after it arrived is still a machine that arrived.
//
// The bundle of 2026-09-13 says both that the fleet which exists is 439
// machines and that 10,520 of them were filed under a customer. One run, two
// numbers, and both are named for machines that exist. Underneath sat a single
// predicate: the summary counted results whose whole life ended with no error,
// so under a load that severed the fleet it counted the survivors — and it
// dropped the connect, handshake and registration timings of everyone else,
// which is a survivorship filter on the very measurement the night was taken to
// produce. Every healthy night hides it, because on a system that holds, the
// machines that arrived and the machines that ended cleanly are the same
// machines.
//
// Arriving and ending cleanly are separate facts and the run already records
// both: arrivedAt is set when the machine finished registering and is kept
// across every reconnection, and the severance is counted on its own.
func TestAMachineSeveredAfterArrivingIsStillOneThatArrived(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 13, 12, 32, 0, 0, time.UTC)
	results := []agentResult{
		// Survived to the wind-down.
		{connectDur: 10 * time.Millisecond, handshakeDur: 4 * time.Millisecond,
			registerDur: 5 * time.Millisecond, arrivedAt: start.Add(100 * time.Millisecond)},
		// Arrived, worked, and lost its connection under the load.
		{connectDur: 200 * time.Millisecond, handshakeDur: 90 * time.Millisecond,
			registerDur: 60 * time.Millisecond, arrivedAt: start.Add(200 * time.Millisecond),
			err: ErrHeldPeerGone},
		// Never got in at all.
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

	// And the severed machine's own timings are in the series. They are the
	// slowest ones, which is exactly why dropping them flatters the run.
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

// A run that severed nothing says so, and a run that lost machines mid-hold
// says how many. The count is the difference between "the fleet held" and "the
// fleet was gone and every agent reported success", which is the shape a whole
// night was recorded in.
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

// observationValue reads one series out of a bundle's observations.
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
