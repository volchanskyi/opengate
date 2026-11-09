package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// enrolFirstInFlight is how many machines are installed at once before the walk. Each waits for
// its answer, so the server takes them as fast as it can and no request ages out in its queue.
const enrolFirstInFlight = 8

// enrolFirstCount is what installing the estate before the walk came to.
type enrolFirstCount struct {
	Installed int
	Failed    int
	Took      time.Duration
}

// installFunc is one machine's first life: enrol, connect and register, run whileConnected, and
// hang up. The server writes the row only while the connection is open.
type installFunc func(ctx context.Context, machine tenantAgent, whileConnected func()) error

// firstLifeOf installs a machine as a technician's install does: its row is written at the
// registration, and the walk's dial is then a return with the certificate it holds.
func firstLifeOf(credentials agentCredentials, addr string) installFunc {
	return func(ctx context.Context, machine tenantAgent, whileConnected func()) error {
		ctx, cancel := context.WithTimeout(ctx, agentDeadline)
		defer cancel()
		// Bare options send nothing past the registration and hang up once whileConnected returns.
		presence := fleetPresence{Arrived: func(time.Time) { whileConnected() }}
		return runAgentWithContext(ctx, credentials, addr, machine, loadOptions{}, presence).err
	}
}

// enrolFirst installs and files every planned machine before the clock starts, inFlight at a
// time; a machine whose install failed is enrolled and filed again when it dials.
func enrolFirst(ctx context.Context, plan []tenantAgent, install installFunc,
	filer *estateFiler, inFlight int,
) enrolFirstCount {
	began := time.Now()
	machines := make(chan tenantAgent)
	var (
		tally installTally
		wg    sync.WaitGroup
	)
	for range max(inFlight, 1) {
		wg.Go(func() {
			for machine := range machines {
				tally.note(installOne(ctx, machine, install, filer))
			}
		})
	}
	offerEach(ctx, plan, machines)
	wg.Wait()
	return tally.total(time.Since(began))
}

// installOne installs one machine and files it while its first connection is open, the filing
// waiting for the row the registration writes.
func installOne(ctx context.Context, machine tenantAgent, install installFunc, filer *estateFiler) error {
	return install(ctx, machine, func() {
		if filer.file(ctx, machine) {
			fmt.Println(estateFiledAnnouncement)
		}
	})
}

// offerEach hands the plan to the installers in order and closes the queue, stopping at the end
// of ctx.
func offerEach(ctx context.Context, plan []tenantAgent, machines chan<- tenantAgent) {
	defer close(machines)
	for _, machine := range plan {
		if ctx.Err() != nil {
			return
		}
		select {
		case machines <- machine:
		case <-ctx.Done():
			return
		}
	}
}

// installTally counts installs from several installers at once.
type installTally struct {
	mu    sync.Mutex
	count enrolFirstCount
}

func (t *installTally) note(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.count.Failed++
		return
	}
	t.count.Installed++
}

func (t *installTally) total(took time.Duration) enrolFirstCount {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := t.count
	count.Took = took
	return count
}

// writeEnrolFirst prints what installing before the walk came to.
func writeEnrolFirst(count enrolFirstCount, planned int) {
	fmt.Printf("Installed first: %d of %d machines in %s, %d failed and enrolled again on their dial\n",
		count.Installed, planned, count.Took.Round(time.Second), count.Failed)
}
