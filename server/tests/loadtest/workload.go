package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// How a run offers its load, and what it builds before the clock starts.
//
// A run given a profile walks that profile's phases; one without offers every
// machine at once and waits. Both are real events — the second is a site whose
// link came back — and which of the two happened is a property of the run rather
// than of the harness.

// fixtureRequest is everything the fixture step needs, kept together so main
// stays a list of steps rather than a list of arguments.
type fixtureRequest struct {
	baseURL   string
	account   string
	password  string
	size      string
	seed      uint64
	profile   *Profile
	bootstrap bool
}

// buildFixtureIfAsked builds the fleet the run measures against, and returns the
// enrollment token the machines are to spend. A run given no administrator
// builds nothing and measures against whatever is already there.
func buildFixtureIfAsked(request fixtureRequest) (*BuiltFixture, string) {
	if request.account == "" || request.baseURL == "" {
		return nil, ""
	}

	size := FixtureSize(request.size)
	if size == "" && request.profile != nil {
		size = request.profile.Fixture
	}
	plan, err := PlanFixture(size, request.seed)
	if err != nil {
		log.Fatalf("fixture: %v", err)
	}

	client := NewFixtureClient(request.baseURL)
	if err := client.EnsureAdmin(request.account, request.password, request.bootstrap); err != nil {
		log.Fatalf("fixture: %v", err)
	}
	built, err := client.BuildFixture(plan)
	if err != nil {
		log.Fatalf("fixture: %v", err)
	}

	fmt.Printf("Fixture built: %d customers, %d sites, %d accounts, %d machines to enrol\n",
		len(built.Customers), built.Sites, len(built.Users), built.PlannedDevices)
	return &built, built.EnrollmentToken
}

// reportingFleet is a fleet that can be wound down and asked what happened. The
// walk needs only the level-holding half; the reading needs the other two, and
// naming them here is what lets the order between them be tested.
type reportingFleet interface {
	Fleet
	Stop()
	Results() []agentResult
}

// runWorkload walks the profile's phases when there is one, and otherwise offers
// the whole fleet at once — which is a real event, a site whose link came back,
// and the only shape available before profiles existed.
func runWorkload(profile *Profile, agents int, agentPlan []tenantAgent,
	credentials agentCredentials, addr string, opts loadOptions,
) ([]agentResult, []PhaseResult) {
	if profile == nil {
		return runFlat(agents, agentPlan, credentials, addr, opts), nil
	}

	fleet := NewQUICFleetWithProbe(
		func(ctx context.Context, index int, noteArrival func()) agentResult {
			plan := agentPlan[index%len(agentPlan)]
			return runAgentWithContext(ctx, credentials, addr, plan, opts, noteArrival)
		},
		phaseProbe(agentPlan, credentials, addr, opts))

	results, phases, err := runProfile(profile, fleet, NewRealClock(), LocalNodeReading)
	if err != nil {
		log.Fatalf("phases: %v", err)
	}
	return results, phases
}

// runProfile walks a profile's phases and returns what the fleet did.
//
// The fleet is wound down before its results are read, and the order is the
// whole point: a machine reports once, when its own life ends, so a fleet still
// holding its level has reported nothing. Reading first gives a run that held
// five hundred machines for six minutes the same account as one that connected
// nobody — no successes, no failures — and a run with no failures reads as a
// clean run.
func runProfile(profile *Profile, fleet reportingFleet, clock Clock, read SafetyReader,
) ([]agentResult, []PhaseResult, error) {
	// The machine the run shares is looked at between phases, and a run that has
	// pushed it past what its profile said it would accept stops there. On the
	// throwaway stack the profile declares no limits, so nothing is gated; on
	// staging the node carries production too.
	phases, err := RunPhasesWatched(profile, fleet, clock, read)
	fleet.Stop()
	return fleet.Results(), phases, err
}

// runFlat offers every machine at once and waits for all of them.
func runFlat(agents int, agentPlan []tenantAgent, credentials agentCredentials,
	addr string, opts loadOptions,
) []agentResult {
	results := make([]agentResult, agents)
	var wg sync.WaitGroup
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = runAgent(credentials, addr, agentPlan[idx], opts)
		}(i)
	}
	wg.Wait()
	return results
}

// phaseProbe is one live round trip through the machine side: connect,
// handshake, register, hang up.
//
// It is what a phase's latency figure is. The figure used to be the last
// finished machine's connect time, and in a profiled run no machine finishes
// while the walk is running — so every phase of every profiled bundle carried
// no latency at all, under a field that is omitted when empty and complained
// about by nothing.
//
// The probe holds nothing: its options carry no hold and no sessions, so it
// arrives, is counted where the server counts arrivals, and leaves. That is
// deliberate — the round trip being timed is the one a machine coming back
// after an outage actually makes.
func phaseProbe(agentPlan []tenantAgent, credentials agentCredentials, addr string, opts loadOptions) ProbeRoundTrip {
	if len(agentPlan) == 0 {
		return nil
	}
	probeOpts := opts
	probeOpts.holdFor = 0
	probeOpts.relaySessions = false
	probeOpts.defaultTelemetry = false
	probeOpts.metricWindows = 0
	probeOpts.backfillBatches = 0
	probeOpts.answerLogPulls = false

	var next atomic.Int64
	return func(ctx context.Context) (time.Duration, error) {
		// Its own hostname, so a probe is never mistaken for one of the machines
		// the phase is holding and never takes a held machine's place.
		plan := tenantAgent{
			tenantIndex: agentPlan[0].tenantIndex,
			agentIndex:  agentPlan[0].agentIndex,
			hostname:    fmt.Sprintf("%s-probe-%d", agentPlan[0].hostname, next.Add(1)),
		}
		result := runAgentWithContext(ctx, credentials, addr, plan, probeOpts, nil)
		if result.err != nil {
			return 0, result.err
		}
		return result.connectDur + result.handshakeDur + result.registerDur, nil
	}
}

// readJourneys carries the technician-side screens this night timed into the
// evidence. A run given no export has no journeys rather than empty ones.
func readJourneys(path string) []JourneyResult {
	if path == "" {
		return nil
	}
	journeys, err := LoadJourneys(path)
	if err != nil {
		fmt.Printf("::warning::could not read the journeys this night timed: %v\n", err)
		return nil
	}
	return journeys
}

// readFixtureWeight carries what the fleet cost on disk into the evidence. A
// run that weighed nothing reports nothing rather than zero, because zero bytes
// is the emptiest fixture ever built.
func readFixtureWeight(path string) *FixtureWeight {
	if path == "" {
		return nil
	}
	weight, err := LoadFixtureWeight(path)
	if err != nil {
		fmt.Printf("::warning::could not read what the fleet weighed: %v\n", err)
		return nil
	}
	return &weight
}

// readServerRegistration asks the server how long registration actually took. A
// run given no address reports nothing rather than reporting zero, because zero
// would be the fastest night ever recorded.
func readServerRegistration(metricsURL string) *ServerRegistration {
	if metricsURL == "" {
		return nil
	}
	reading, err := FetchServerRegistration(metricsURL)
	if err != nil {
		fmt.Printf("::warning::could not read the server's registration timing: %v\n", err)
		return nil
	}
	return &reading
}
