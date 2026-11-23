package main

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"
)

// redialBase and redialCap bound the full-jitter backoff window a simulated machine redials in.
const (
	redialBase = 1 * time.Second
	redialCap  = 30 * time.Second
)

// persistThrough redials a persistent machine until the run ends and any other once more; every
// machine obeys each "come back later", which is neither a failed dial nor a redial.
func persistThrough(ctx context.Context, opts loadOptions, live func(context.Context) agentResult) agentResult {
	res, told := liveThroughComeBacks(ctx, live)
	if !opts.reconnect {
		if res.err == nil || !res.arrivedAt.IsZero() {
			res.toldToComeBack = told
			return res
		}
		select {
		case <-ctx.Done():
			res.toldToComeBack = told
			return res
		case <-time.After(redialDelay(0)):
		}
		again, toldAgain := liveThroughComeBacks(ctx, live)
		again.retried = 1
		again.toldToComeBack = told + toldAgain
		return again
	}

	for attempt := 0; res.err != nil && ctx.Err() == nil; attempt++ {
		select {
		case <-ctx.Done():
			res.toldToComeBack = told
			return res
		case <-time.After(redialDelay(attempt)):
		}

		again, toldAgain := liveThroughComeBacks(ctx, live)
		told += toldAgain
		res.redials++
		res.err = again.err
		if !again.arrivedAt.IsZero() {
			res.reconnected++
		}
	}
	res.toldToComeBack = told
	return res
}

// comeBackJitter is the most the agent adds to the server's whole-second wait.
const comeBackJitter = time.Second

// liveThroughComeBacks dials until the server stops telling the machine to come back later,
// keeping each wait it is given as the agent does, and returns the last dial and the come-backs.
func liveThroughComeBacks(ctx context.Context, live func(context.Context) agentResult) (agentResult, int) {
	told := 0
	for {
		res := live(ctx)
		var later *toldToComeBack
		if !errors.As(res.err, &later) {
			return res, told
		}
		told++
		select {
		case <-ctx.Done():
			return res, told
		case <-time.After(later.wait + jitterUpTo(comeBackJitter)):
		}
	}
}

// redialDelay is one full-jitter wait, uniform up to a window that doubles
// from the base to the cap.
func redialDelay(attempt int) time.Duration {
	window := redialCap
	if attempt < 30 {
		if scaled := redialBase << attempt; scaled > 0 && scaled < redialCap {
			window = scaled
		}
	}
	return jitterUpTo(window)
}

// jitterUpTo is a uniform wait from nought to most; a failed random read waits the whole of most.
func jitterUpTo(most time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(most)+1))
	if err != nil {
		return most
	}
	return time.Duration(n.Int64())
}
