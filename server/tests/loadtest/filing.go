package main

import (
	"context"
	"fmt"
	"sync"
)

// estateFiledAnnouncement is the line the run prints once its declared estate is filed,
// which the in-cluster script waits for before starting the browser-side scenarios.
const estateFiledAnnouncement = "Estate filed"

// filingAttemptsBeforeGivingUp is how many refusals with nothing filed make a fleet the run
// cannot file; every refused attempt spends a request from the per-address arrival allowance.
const filingAttemptsBeforeGivingUp = 5

// estateFiler files each machine once, as it arrives.
type estateFiler struct {
	client      *FixtureClient
	built       BuiltFixture
	credentials agentCredentials
	// estate is how many machines the run draws from, which scales a machine's position when
	// its customer is picked.
	estate int
	// readyAt is how many filed machines make the estate worth announcing.
	readyAt int

	mu sync.Mutex
	// filed is keyed by machine name, so a machine that reconnects is not written again.
	filed     map[string]bool
	refused   int
	announced bool
	// gaveUp is set once the run has shown it cannot file at all.
	gaveUp bool
}

// newEstateFiler builds the filer for a run that has a fixture to file against.
func newEstateFiler(client *FixtureClient, built BuiltFixture, credentials agentCredentials,
	estate, readyAt int,
) *estateFiler {
	if readyAt < 1 {
		readyAt = 1
	}
	return &estateFiler{
		client:      client,
		built:       built,
		credentials: credentials,
		estate:      estate,
		readyAt:     readyAt,
		filed:       map[string]bool{},
	}
}

// file files one arrived machine and reports whether this arrival completes the declared level,
// so the caller announces once. A nil filer, for a run with no fixture, files nothing.
func (f *estateFiler) file(ctx context.Context, machine tenantAgent) bool {
	if f == nil {
		return false
	}

	f.mu.Lock()
	if f.gaveUp || f.filed[machine.hostname] {
		f.mu.Unlock()
		return false
	}
	f.mu.Unlock()

	config, err := f.credentials.forAgent(ctx, machine)
	if err != nil {
		return f.refuse(machine, fmt.Errorf("read its credential: %w", err))
	}
	deviceID, ok := deviceIDFrom(config)
	if !ok {
		return f.refuse(machine, fmt.Errorf("its credential carries no identifier"))
	}
	if err := f.client.FileDevice(f.built, machine.agentIndex, f.estate, deviceID); err != nil {
		return f.refuse(machine, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// A machine another arrival already filed adds nothing to the level.
	if f.filed[machine.hostname] {
		return false
	}
	f.filed[machine.hostname] = true
	if f.announced || len(f.filed) < f.readyAt {
		return false
	}
	f.announced = true
	return true
}

// refuse records one machine the run could not file and warns once, on the first refusal.
func (f *estateFiler) refuse(machine tenantAgent, err error) bool {
	f.mu.Lock()
	first := f.refused == 0
	f.refused++
	// Nothing filed after a run of attempts means the run cannot file, and each further attempt
	// spends a request the arrivals need.
	givingUp := false
	if len(f.filed) == 0 && f.refused >= filingAttemptsBeforeGivingUp && !f.gaveUp {
		f.gaveUp = true
		givingUp = true
	}
	attempts := f.refused
	f.mu.Unlock()

	if first {
		fmt.Printf("::warning::could not file %s: %v — the fleet's customer and building are what a scoped read narrows by\n",
			machine.hostname, err)
	}
	if givingUp {
		fmt.Printf("::warning::filing was refused for the first %d machines and none was filed, so this run stops asking — every attempt spends the allowance its arrivals need\n",
			attempts)
	}
	return false
}

// counts is how many machines the run filed and how many it could not.
func (f *estateFiler) counts() (filed, refused int) {
	if f == nil {
		return 0, 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.filed), f.refused
}

// arrivalOf composes what happens when a machine arrives: the fleet counts it in its phase and
// the estate files it. Either half may be nil.
func arrivalOf(noteArrival func(), filer *estateFiler, machine tenantAgent) func() {
	return func() {
		if noteArrival != nil {
			noteArrival()
		}
		if filer.file(context.Background(), machine) {
			fmt.Println(estateFiledAnnouncement)
		}
	}
}

// filingLevel is the first phase's level, or the whole estate when no phase connects anybody.
func filingLevel(profile *Profile, estate int) int {
	if profile == nil || len(profile.Phases) == 0 {
		return estate
	}
	if level := profile.Phases[0].ConnectedAgents; level > 0 {
		return level
	}
	return estate
}
