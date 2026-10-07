package main

import (
	"fmt"
	"sync"
)

// agentRoster hands out the estate's machines, never one to two starts at once, because the
// server knows a machine by its certificate and a second live connection displaces the first.
type agentRoster struct {
	mu sync.Mutex
	// plan is the estate in the order the fixture planned it.
	plan []tenantAgent
	// free holds the indexes into plan that nobody is connected as.
	free []int
	// out is the machines a start holds, so a machine given back twice is put back once.
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

// take hands out one unconnected machine with the call that gives it back, or reports none free.
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

// give puts one machine back within reach of the next start; a second give of it is ignored.
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

// ErrEstateExhausted marks a start given no identity; the fleet tallies it apart from a refusal.
var ErrEstateExhausted = fmt.Errorf("the estate holds no machine that is not already connected")

// checkEstateHolds refuses a run whose profile declares a level above the estate's size.
// A run with no profile declares no level.
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
