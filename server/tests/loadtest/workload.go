package main

import (
	"context"
	"fmt"
	"log"
	"sync"
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

// runWorkload walks the profile's phases when there is one, and otherwise offers
// the whole fleet at once — which is a real event, a site whose link came back,
// and the only shape available before profiles existed.
func runWorkload(profile *Profile, agents int, agentPlan []tenantAgent,
	credentials agentCredentials, addr string, opts loadOptions,
) ([]agentResult, []PhaseResult) {
	if profile == nil {
		return runFlat(agents, agentPlan, credentials, addr, opts), nil
	}

	fleet := NewQUICFleet(func(ctx context.Context, index int) agentResult {
		plan := agentPlan[index%len(agentPlan)]
		return runAgentWithContext(ctx, credentials, addr, plan, opts)
	})
	defer fleet.Stop()

	// The machine the run shares is looked at between phases, and a run that has
	// pushed it past what its profile said it would accept stops there. On the
	// throwaway stack the profile declares no limits, so nothing is gated; on
	// staging the node carries production too.
	phases, err := RunPhasesWatched(profile, fleet, NewRealClock(), LocalNodeReading)
	if err != nil {
		log.Fatalf("phases: %v", err)
	}
	return fleet.Results(), phases
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
