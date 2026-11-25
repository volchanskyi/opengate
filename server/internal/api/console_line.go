package api

import (
	"context"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"time"
)

// consoleRequestsPerProcessor is how many signed-in console requests run at once per processor of
// the container's allowance while machines wait to sign in.
const consoleRequestsPerProcessor = 4

const (
	// consoleLineRecheck is how often a waiting request asks whether machines still wait.
	consoleLineRecheck = 100 * time.Millisecond
	// consoleLineReplyMargin ends a wait before the request's own deadline, so the line answers
	// with a retry time ahead of the timeout handler's bare 503.
	consoleLineReplyMargin = 250 * time.Millisecond
	// consoleLineRetryAfterSeconds is the retry time a request turned away is given.
	consoleLineRetryAfterSeconds = 5
)

// MachineLine says whether returning machines wait in the sign-in line.
type MachineLine interface {
	MachinesWaiting() bool
}

// consoleLine gives returning machines the processor first: while any waits to sign in, a set
// number of signed-in console requests run at once and the rest wait their turn.
type consoleLine struct {
	machines MachineLine
	places   chan struct{}
}

// newConsoleLine sizes the line for the processor share, every processor of the machine when the
// share is zero, never below one place.
func newConsoleLine(machines MachineLine, processorShare float64) *consoleLine {
	if processorShare <= 0 {
		processorShare = float64(runtime.NumCPU())
	}
	places := max(1, int(math.Round(consoleRequestsPerProcessor*processorShare)))
	return &consoleLine{machines: machines, places: make(chan struct{}, places)}
}

// admit holds a signed-in request while machines wait and every place is taken; a request still
// waiting at its deadline is answered 503 with a retry time. Other requests pass at once.
func (l *consoleLine) admit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l.machines == nil || ContextClaims(r.Context()) == nil || !l.machines.MachinesWaiting() {
			next.ServeHTTP(w, r)
			return
		}
		holdsPlace, admitted := l.wait(r.Context())
		if !admitted {
			w.Header().Set("Retry-After", strconv.Itoa(consoleLineRetryAfterSeconds))
			writeError(w, http.StatusServiceUnavailable, "machines are signing in; try again shortly")
			return
		}
		if holdsPlace {
			defer func() { <-l.places }()
		}
		next.ServeHTTP(w, r)
	})
}

// wait returns once a place is free or no machine waits, reporting whether the request holds a
// place and whether it was let through before its time to wait ran out.
func (l *consoleLine) wait(ctx context.Context) (holdsPlace, admitted bool) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultRequestTimeout)
	}
	waitCtx, cancel := context.WithDeadline(ctx, deadline.Add(-consoleLineReplyMargin))
	defer cancel()
	recheck := time.NewTicker(consoleLineRecheck)
	defer recheck.Stop()
	for {
		select {
		case l.places <- struct{}{}:
			return true, true
		case <-waitCtx.Done():
			return false, false
		case <-recheck.C:
			if !l.machines.MachinesWaiting() {
				return false, true
			}
		}
	}
}
