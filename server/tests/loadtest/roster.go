package main

import (
	"fmt"
	"sync"
)

// The estate is fixed, and its machines come and go.
//
// A profile that winds a level down and back up is an estate losing connections
// and getting them back — a site whose link dropped, a rollout that restarted a
// fleet. A harness that mints a fresh identity for every start models something
// else entirely: a burst of two thousand reconnections becomes a burst of two
// thousand enrolments against a ceiling the server enforces on purpose, the
// customer's fleet grows for as long as the run lasts, and a long run confounds
// itself with the volume dimension.
//
// So a start takes a machine that is not currently connected and gives it back
// when it leaves, and the identity it dials with is minted the first time and
// held after that.

// agentRoster hands out the estate's machines, never two starts the same one.
//
// Handing one out twice at once would be a worse fault than the re-enrolment
// this closes: the server knows a machine by its certificate, so two live
// connections presenting the same one are one device registering twice, and the
// level silently drops by the machine that was displaced.
type agentRoster struct {
	mu sync.Mutex
	// plan is the estate, in the order the fixture planned it.
	plan []tenantAgent
	// free is the machines nobody is currently connected as, by index into
	// plan.
	free []int
	// out is the machines a start currently holds, so a machine given back
	// twice is put back once.
	out map[int]bool
}

// newAgentRoster builds the estate a run draws from.
func newAgentRoster(plan []tenantAgent) *agentRoster {
	free := make([]int, len(plan))
	for i := range plan {
		free[i] = i
	}
	return &agentRoster{plan: plan, free: free, out: map[int]bool{}}
}

// take hands out one machine that is not currently connected, together with the
// call that gives it back. An estate with nobody free reports that rather than
// doubling up.
func (r *agentRoster) take() (tenantAgent, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.free) == 0 {
		return tenantAgent{}, nil, false
	}
	index := r.free[0]
	r.free = r.free[1:]
	r.out[index] = true

	return r.plan[index], func() { r.give(index) }, true
}

// give puts one machine back within reach of the next start. A machine given
// back twice — a fleet winding down races its own machines' returns — is put
// back once, because the second copy would be handed to a second start.
func (r *agentRoster) give(index int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.out[index] {
		return
	}
	delete(r.out, index)
	r.free = append(r.free, index)
}

// size is how many machines the estate holds.
func (r *agentRoster) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.plan)
}

// ErrEstateExhausted is a start that could not be given an identity, which is a
// machine that did not arrive. It is named so the fleet's tally can hold it
// apart from a machine the server refused.
var ErrEstateExhausted = fmt.Errorf("the estate holds no machine that is not already connected")

// checkEstateHolds refuses a run whose profile declares a level the estate
// cannot reach.
//
// The two outcomes look identical from a bundle — an attainment short of its
// offer — and only one of them is a finding about the system, so the one that
// is a mis-sized run is refused before the clock starts. A run with no profile
// offers every machine it was given at once and declares no level.
func checkEstateHolds(profile *Profile, estate int) error {
	if profile == nil {
		return nil
	}
	for _, phase := range profile.Phases {
		if phase.ConnectedAgents > estate {
			return fmt.Errorf(
				"phase %q holds %d machines connected and the estate has %d: "+
					"give -agents at least %d, or the level this profile declares is one the run can never reach",
				phase.Name, phase.ConnectedAgents, estate, phase.ConnectedAgents)
		}
	}
	return nil
}
