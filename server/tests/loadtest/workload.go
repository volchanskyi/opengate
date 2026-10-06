package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type fixtureRequest struct {
	baseURL   string
	account   string
	password  string
	size      string
	seed      uint64
	profile   *Profile
	bootstrap bool
	// runFor is how long the load is applied, which the fleet's enrolment credential must outlive.
	runFor time.Duration
}

// builtFleet is the fleet the fixture step built, the administrator session that files
// each arriving machine, and the credential those machines spend.
type builtFleet struct {
	fixture *BuiltFixture
	client  *FixtureClient
	token   string
}

// buildFixtureIfAsked builds the fleet the run measures against and returns its enrolment token.
// A run given no administrator builds nothing and measures what is already there.
func buildFixtureIfAsked(request fixtureRequest) builtFleet {
	if request.account == "" || request.baseURL == "" {
		return builtFleet{}
	}

	size := FixtureSize(request.size)
	if size == "" && request.profile != nil {
		size = request.profile.Fixture
	}
	plan, err := PlanFixture(size, request.seed)
	if err != nil {
		log.Fatalf("fixture: %v", err)
	}

	client := NewFixtureClientForRun(request.baseURL, request.runFor)
	if err := client.EnsureAdmin(request.account, request.password, request.bootstrap); err != nil {
		log.Fatalf("fixture: %v", err)
	}
	built, err := client.BuildFixture(plan)
	if err != nil {
		log.Fatalf("fixture: %v", err)
	}

	fmt.Printf("Fixture built: %d customers, %d sites, %d accounts, %d machines to enrol\n",
		len(built.Customers), built.Sites, len(built.Users), built.PlannedDevices)
	return builtFleet{fixture: &built, client: client, token: built.EnrollmentToken}
}

// filerFor builds the estate's filer, or nil when there is no fleet to file.
// The run's held credentials give the filer the certificate a machine dials with.
func (b builtFleet) filerFor(credentials agentCredentials, estate int, profile *Profile) *estateFiler {
	if b.fixture == nil || b.client == nil {
		return nil
	}
	return newEstateFiler(b.client, *b.fixture, credentials, estate, filingLevel(profile, estate))
}

// reportingFleet is a fleet that can be wound down and asked what happened.
type reportingFleet interface {
	Fleet
	Stop()
	Results() []agentResult
}

// runWorkload walks the profile's phases, or offers the whole fleet at once when there is none.
// The fleet and the filer share the held credentials, so no machine is filed under a new identity.
func runWorkload(profile *Profile, agents int, agentPlan []tenantAgent,
	credentials agentCredentials, addr string, opts loadOptions, readings PhaseReadings,
	filer *estateFiler,
) ([]agentResult, []PhaseResult, *float64, string) {
	if profile == nil {
		return runFlat(agents, agentPlan, credentials, addr, opts, readings.Busy, filer)
	}

	// The estate is fixed and its machines enrol once, so a level beyond it is a mis-sized run.
	if err := checkEstateHolds(profile, len(agentPlan)); err != nil {
		log.Fatalf("phases: %v", err)
	}

	roster := newAgentRoster(agentPlan)

	fleet := NewQUICFleetWithProbe(
		estateStart(roster, credentials, addr, opts, filer),
		phaseProbe(agentPlan, credentials, addr, opts))

	results, phases, err := runProfile(profile, fleet, NewRealClock(), VenueNodeReading, readings)
	if err != nil {
		log.Fatalf("phases: %v", err)
	}
	// A walked run carries busy-ness per phase, so the whole-run pair stays absent.
	return results, phases, nil, ""
}

// estateStart is one machine's start: it takes a machine nobody is connected as and gives it back.
// With none free it reports ErrEstateExhausted, since two live connections on one identity clash.
func estateStart(roster *agentRoster, credentials agentCredentials, addr string, opts loadOptions,
	filer *estateFiler,
) StartAgent {
	return func(ctx context.Context, _ int, presence fleetPresence) agentResult {
		machine, giveBack, ok := roster.take()
		if !ok {
			return agentResult{err: ErrEstateExhausted}
		}
		defer giveBack()
		return runAgentWithContext(ctx, credentials, addr, machine, opts,
			fleetPresence{Arrived: arrivalOf(presence.Arrived, filer, machine), Left: presence.Left})
	}
}

// runProfile walks a profile's phases and returns what the fleet did.
// The fleet is wound down before its results are read, since a machine reports when its life ends.
func runProfile(profile *Profile, fleet reportingFleet, clock Clock, read SafetyReader, readings PhaseReadings,
) ([]agentResult, []PhaseResult, error) {
	// The shared node is read between phases, and a run past its profile's limits stops there.
	phases, err := RunPhasesWatched(profile, fleet, clock, read, readings)
	fleet.Stop()
	return fleet.Results(), phases, err
}

// runFlat offers every machine at once and waits for all of them.
// The one phase is the whole run, so the target's busy-ness brackets all of it.
func runFlat(agents int, agentPlan []tenantAgent, credentials agentCredentials,
	addr string, opts loadOptions, busy TargetBusy, filer *estateFiler,
) ([]agentResult, []PhaseResult, *float64, string) {
	closeBusy := busy.Bracket()
	startedAt := time.Now()

	results := make([]agentResult, agents)
	var wg sync.WaitGroup
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			machine := agentPlan[idx]
			results[idx] = runAgent(credentials, addr, machine, opts,
				fleetPresence{Arrived: arrivalOf(nil, filer, machine)})
		}(i)
	}
	wg.Wait()

	busyPercent, busyAbsent := closeBusy(time.Since(startedAt))
	return results, nil, busyPercent, busyAbsent
}

// phaseProbe is one live machine round trip: connect, handshake, register, hang up.
// It holds nothing, so the timed trip is the one a machine returning after an outage makes.
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

	// One distinct hostname keeps the probe apart from held machines and enrols no device per trip.
	plan := tenantAgent{
		tenantIndex: agentPlan[0].tenantIndex,
		agentIndex:  agentPlan[0].agentIndex,
		hostname:    agentPlan[0].hostname + "-probe",
	}
	return func(ctx context.Context) (time.Duration, error) {
		result := runAgentWithContext(ctx, credentials, addr, plan, probeOpts, fleetPresence{})
		if result.err != nil {
			return 0, result.err
		}
		return result.connectDur + result.handshakeDur + result.registerDur, nil
	}
}

// readJourneys loads the technician-side journeys timed this night; no export gives nil.
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

// readFixtureWeight loads what the fleet cost on disk; no file gives nil.
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

// readServerRegistration reads how long registration took on the server; no address gives nil.
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

// loadLastsFor is how long the run applies load: the profile's walk, else the machines' hold.
// The enrolment credential must outlive it, since every machine a phase starts spends it.
func loadLastsFor(profile *Profile, holdFor time.Duration) time.Duration {
	if profile != nil {
		return profile.TotalDuration()
	}
	return holdFor
}
