package main

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"
)

// Whether a machine stays through an outage is a property of what the run is
// measuring, not of the harness.
//
// A run measuring what a server carries wants a severed machine reported: the
// question is how many connections the server held, and a harness that quietly
// repaired its own fleet would answer it with a number nobody could interpret.
// A run standing machines behind a link that is about to go dark wants the
// opposite — a site whose machines never came back is not a site, and the
// catch-up it was there to create never happens.
//
// So the two behaviours live side by side and the run picks. The load
// generator's is the default, because it is the one a wrong answer is expensive
// in: a severance nobody reported is a measurement of a fleet that was not
// there.

// redialBase and redialCap are the shipped agent's own backoff window — full
// jitter from a base of one second to a cap of thirty. A simulated machine
// returns the way a real one does, so twenty of them behind one link come back
// spread out rather than together, which is what a site actually looks like
// when its broadband returns.
const (
	redialBase = 1 * time.Second
	redialCap  = 30 * time.Second
)

// persistThrough runs a machine's life on one connection and, when the run asks
// it to stay through an outage, runs it again after that connection breaks —
// until the run winds down.
//
// What it answers is the first connection's result, because that is the machine
// arriving and the fleet's arrival window is measured from it. Carried with it
// is how many times the machine had to come back, which is the harness's own
// account of how much of the herd the outage took.
//
// A machine still away when the run ends reports the severance it never
// recovered from. That is deliberate: the alternative reports a lost machine as
// a machine that behaved, and losing the herd silently is the defect this whole
// path exists to close.
func persistThrough(ctx context.Context, opts loadOptions, live func(context.Context) agentResult) agentResult {
	res := live(ctx)
	if !opts.reconnect {
		return res
	}

	for attempt := 0; res.err != nil && ctx.Err() == nil; attempt++ {
		select {
		case <-ctx.Done():
			return res
		case <-time.After(redialDelay(attempt)):
		}

		again := live(ctx)
		res.redials++
		res.err = again.err
		if !again.arrivedAt.IsZero() {
			res.reconnected++
		}
	}
	return res
}

// redialDelay is one full-jitter wait: uniform between nothing and the window
// for this attempt, where the window doubles from the base to the cap.
//
// Uniform-from-zero rather than a fixed schedule because the point is to spread
// a herd. A fixed backoff moves twenty machines together; it does not stop them
// arriving at once.
func redialDelay(attempt int) time.Duration {
	window := redialCap
	if attempt < 30 {
		if scaled := redialBase << attempt; scaled > 0 && scaled < redialCap {
			window = scaled
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(window)+1))
	if err != nil {
		// The system's randomness is not something a load run can go on
		// without an answer to, and the whole window is the safe answer: it
		// waits longer rather than dialling harder.
		return window
	}
	return time.Duration(n.Int64())
}
