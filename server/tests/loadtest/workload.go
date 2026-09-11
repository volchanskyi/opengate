package main

import (
	"context"
	"fmt"
	"log"
	"sync"
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
	// runFor is how long the load will be applied for, which is what the
	// credential the fleet enrols with has to outlive.
	runFor time.Duration
}

// builtFleet is what the fixture step leaves behind: the fleet that now exists,
// the administrator session that created it — which is also the session that
// files each machine as it arrives — and the credential those machines spend.
type builtFleet struct {
	fixture *BuiltFixture
	client  *FixtureClient
	token   string
}

// buildFixtureIfAsked builds the fleet the run measures against, and returns the
// enrollment token the machines are to spend. A run given no administrator
// builds nothing and measures against whatever is already there.
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

// filerFor builds the estate's filer, or nothing when there is no fleet to file.
//
// The credentials are the run's held ones, so the filer reads the certificate a
// machine actually dials with rather than minting a second identity for it.
func (b builtFleet) filerFor(credentials agentCredentials, estate int, profile *Profile) *estateFiler {
	if b.fixture == nil || b.client == nil {
		return nil
	}
	return newEstateFiler(b.client, *b.fixture, credentials, estate, filingLevel(profile, estate))
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
//
// The credentials handed in are already the run's held ones, minted once per
// machine and reused after. Both the fleet and the filer read them, and they
// have to be the same wrapper: a filer given the raw source would mint a second
// identity for every machine it filed, and file a fleet the run never
// connected.
func runWorkload(profile *Profile, agents int, agentPlan []tenantAgent,
	credentials agentCredentials, addr string, opts loadOptions, busy TargetBusy,
	filer *estateFiler,
) ([]agentResult, []PhaseResult, *float64, string) {
	if profile == nil {
		return runFlat(agents, agentPlan, credentials, addr, opts, busy, filer)
	}

	// The estate is fixed and its machines enrol once, so a level the estate
	// cannot reach is a mis-sized run rather than a finding about the system —
	// and the two are indistinguishable once the walk has started.
	if err := checkEstateHolds(profile, len(agentPlan)); err != nil {
		log.Fatalf("phases: %v", err)
	}

	roster := newAgentRoster(agentPlan)

	fleet := NewQUICFleetWithProbe(
		estateStart(roster, credentials, addr, opts, filer),
		phaseProbe(agentPlan, credentials, addr, opts))

	results, phases, err := runProfile(profile, fleet, NewRealClock(), VenueNodeReading, busy)
	if err != nil {
		log.Fatalf("phases: %v", err)
	}
	// A walked run carries its busy-ness per phase, so the whole-run pair below
	// belongs to the flat shape alone and stays absent here.
	return results, phases, nil, ""
}

// estateStart is one machine's start, drawn from the estate: it takes a machine
// nobody is currently connected as, runs its whole life, and gives it back when
// it leaves. The machine's index is the fleet's own bookkeeping and says nothing
// about which machine this is — the estate decides that, so a level that came
// down and went back up brings the same machines back rather than enrolling new
// ones over them.
//
// A start that could not be given a machine is a machine that did not arrive,
// and it says so. The alternative — handing one identity to two live
// connections — is worse than the re-enrolment this closes: the server knows a
// machine by its certificate, so it keeps whichever registered last and the
// level drops by the one that was displaced, with nothing anywhere reporting it.
func estateStart(roster *agentRoster, credentials agentCredentials, addr string, opts loadOptions,
	filer *estateFiler,
) StartAgent {
	return func(ctx context.Context, _ int, noteArrival func()) agentResult {
		machine, giveBack, ok := roster.take()
		if !ok {
			return agentResult{err: ErrEstateExhausted}
		}
		defer giveBack()
		return runAgentWithContext(ctx, credentials, addr, machine, opts,
			arrivalOf(noteArrival, filer, machine))
	}
}

// runProfile walks a profile's phases and returns what the fleet did.
//
// The fleet is wound down before its results are read, and the order is the
// whole point: a machine reports once, when its own life ends, so a fleet still
// holding its level has reported nothing. Reading first gives a run that held
// five hundred machines for six minutes the same account as one that connected
// nobody — no successes, no failures — and a run with no failures reads as a
// clean run.
func runProfile(profile *Profile, fleet reportingFleet, clock Clock, read SafetyReader, busy TargetBusy,
) ([]agentResult, []PhaseResult, error) {
	// The machine the run shares is looked at between phases, and a run that has
	// pushed it past what its profile said it would accept stops there. On the
	// throwaway stack the profile declares no limits, so nothing is gated; on
	// staging the node carries production too.
	phases, err := RunPhasesWatched(profile, fleet, clock, read, busy)
	fleet.Stop()
	return fleet.Results(), phases, err
}

// runFlat offers every machine at once and waits for all of them.
//
// It has one phase and that phase is the run, so the target's own busy-ness is
// bracketed around the whole of it — the same reading a walked phase takes,
// over the only window this shape has.
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
				arrivalOf(nil, filer, machine))
		}(i)
	}
	wg.Wait()

	busyPercent, busyAbsent := closeBusy(time.Since(startedAt))
	return results, nil, busyPercent, busyAbsent
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

	// Its own name, so a probe is never mistaken for one of the machines the
	// phase is holding and never takes a held machine's place. One name rather
	// than one per round trip, because a probe is a machine that keeps coming
	// back: numbering them enrolled a new device every step of every ramp, which
	// over a five-hour soak is the fleet growing by the measurement of it.
	plan := tenantAgent{
		tenantIndex: agentPlan[0].tenantIndex,
		agentIndex:  agentPlan[0].agentIndex,
		hostname:    agentPlan[0].hostname + "-probe",
	}
	return func(ctx context.Context) (time.Duration, error) {
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

// loadLastsFor is how long this run will be applying load: the profile's own
// walk when there is one, and otherwise the hold every machine was given, which
// is what a flat run's length is.
//
// It is the figure the enrolment credential has to outlive, because every
// machine a phase starts spends that credential.
func loadLastsFor(profile *Profile, holdFor time.Duration) time.Duration {
	if profile != nil {
		return profile.TotalDuration()
	}
	return holdFor
}
