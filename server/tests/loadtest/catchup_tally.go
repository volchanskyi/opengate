package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CatchUpCount is how the server answered the run's catch-up batches, how many machines caught up
// whole, and when, counted from the run's start, the last batch was stored.
type CatchUpCount struct {
	Stored            int64   `json:"stored"`
	Deferred          int64   `json:"deferred"`
	Unanswered        int64   `json:"unanswered"`
	Complete          int64   `json:"complete,omitempty"`
	LastStoredSeconds float64 `json:"last_stored_seconds,omitempty"`
	// ResendRequests and LiveConfirmed count what the server asked again and confirmed live.
	ResendRequests int64 `json:"resend_requests,omitempty"`
	LiveConfirmed  int64 `json:"live_confirmed,omitempty"`
	// TurnRepeats counts turn requests asked again because their answer never came.
	TurnRepeats int64 `json:"turn_repeats,omitempty"`
	// Endings is why each machine's catch-up stopped, a machine counted once however often it
	// reconnected.
	Endings *CatchUpEndings `json:"endings,omitempty"`
}

// CatchUpEndings counts machines by how their catch-up last ended; one that caught up on any
// connection counts as caught up.
type CatchUpEndings struct {
	CaughtUp int64 `json:"caught_up"`
	// NeverGranted asked for a turn and the connection ended before the scheduler answered.
	NeverGranted int64 `json:"never_granted"`
	// ToldToWait stopped asking after more waits than a machine takes on one connection.
	ToldToWait int64 `json:"told_to_wait"`
	// ConnectionEnded lost the connection, or a batch's answer, after its turn was granted.
	ConnectionEnded int64 `json:"connection_ended"`
	// RunEnded was waiting to ask again when the run ended.
	RunEnded int64 `json:"run_ended"`
	// ToldToWaitTimes maps how many waits a machine was told to the machines that stopped there.
	ToldToWaitTimes map[int]int64 `json:"told_to_wait_times,omitempty"`
}

// catchUpEnding is why one machine's catch-up on one connection stopped.
type catchUpEnding int

const (
	endedCaughtUp catchUpEnding = iota
	endedNeverGranted
	endedToldToWait
	endedConnection
	endedRun
)

// catchUpEnd is one catch-up's ending and how many times it was told to wait on the way.
type catchUpEnd struct {
	why   catchUpEnding
	waits int
}

// catchUpTally is the run-wide count every machine's catch-up adds to.
type catchUpTally struct {
	stored, deferred, unanswered        atomic.Int64
	resends, liveConfirmed, turnRepeats atomic.Int64
	// lastStored is the wall clock of the newest stored batch, in Unix nanoseconds.
	lastStored atomic.Int64
	started    time.Time

	// endingsMu guards byMachine, each machine's ending as the run last saw it.
	endingsMu sync.Mutex
	byMachine map[string]catchUpEnd
}

// count is the tally for the bundle, or nil when no batch was sent, which differs from nought
// stored of nought.
func (t *catchUpTally) count() *CatchUpCount {
	if t == nil {
		return nil
	}
	c := &CatchUpCount{
		Stored: t.stored.Load(), Deferred: t.deferred.Load(), Unanswered: t.unanswered.Load(),
		ResendRequests: t.resends.Load(), LiveConfirmed: t.liveConfirmed.Load(),
		TurnRepeats: t.turnRepeats.Load(), Endings: t.endingCounts(),
	}
	if c.Endings != nil {
		c.Complete = c.Endings.CaughtUp
	}
	if c.Stored+c.Deferred+c.Unanswered == 0 && c.Endings == nil {
		return nil
	}
	if last := t.lastStored.Load(); last > 0 && !t.started.IsZero() {
		c.LastStoredSeconds = time.Unix(0, last).Sub(t.started).Seconds()
	}
	return c
}

// endingCounts is the machines by how their catch-up ended, or nil when none has.
func (t *catchUpTally) endingCounts() *CatchUpEndings {
	t.endingsMu.Lock()
	defer t.endingsMu.Unlock()
	if len(t.byMachine) == 0 {
		return nil
	}
	e := &CatchUpEndings{}
	for _, end := range t.byMachine {
		switch end.why {
		case endedCaughtUp:
			e.CaughtUp++
		case endedNeverGranted:
			e.NeverGranted++
		case endedToldToWait:
			e.ToldToWait++
			if e.ToldToWaitTimes == nil {
				e.ToldToWaitTimes = map[int]int64{}
			}
			e.ToldToWaitTimes[end.waits]++
		case endedConnection:
			e.ConnectionEnded++
		default:
			e.RunEnded++
		}
	}
	return e
}

// ended records how one machine's catch-up on a connection ended; a machine that caught up once
// stays caught up, and a nil tally counts nothing.
func (t *catchUpTally) ended(machine string, end catchUpEnd) {
	if t == nil {
		return
	}
	t.endingsMu.Lock()
	defer t.endingsMu.Unlock()
	if t.byMachine == nil {
		t.byMachine = map[string]catchUpEnd{}
	}
	if prior, seen := t.byMachine[machine]; seen && prior.why == endedCaughtUp {
		return
	}
	t.byMachine[machine] = end
}

// confirmed counts one live window the server said it stored.
func (t *catchUpTally) confirmed() {
	if t != nil {
		t.liveConfirmed.Add(1)
	}
}

// askedAgain counts one turn request asked again after its answer never came.
func (t *catchUpTally) askedAgain() {
	if t != nil {
		t.turnRepeats.Add(1)
	}
}

// resent counts one request to send history again.
func (t *catchUpTally) resent() {
	if t != nil {
		t.resends.Add(1)
	}
}

// record counts one answer; a nil tally counts nothing.
func (t *catchUpTally) record(answer batchAnswer) {
	if t == nil {
		return
	}
	switch answer {
	case batchStored:
		t.stored.Add(1)
		t.lastStored.Store(time.Now().UnixNano())
	case batchDeferred:
		t.deferred.Add(1)
	default:
		t.unanswered.Add(1)
	}
}

// writeCatchUp prints how the run's catch-up batches were answered; nil prints nothing.
func writeCatchUp(w io.Writer, c *CatchUpCount) {
	if c == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Catch-up:    %d stored, %d told to wait, %d never answered\n",
		c.Stored, c.Deferred, c.Unanswered)
	if e := c.Endings; e != nil {
		_, _ = fmt.Fprintf(w, "Catch-up ended: %d caught up, %d never granted a turn, %d told to wait%s, "+
			"%d on a broken connection, %d with the run; %d turns asked again\n",
			e.CaughtUp, e.NeverGranted, e.ToldToWait, waitTimes(e.ToldToWaitTimes), e.ConnectionEnded, e.RunEnded,
			c.TurnRepeats)
	}
}

// waitTimes lists, fewest waits first, how many machines gave up after each number of waits.
func waitTimes(byWaits map[int]int64) string {
	if len(byWaits) == 0 {
		return ""
	}
	waits := slices.Sorted(maps.Keys(byWaits))
	parts := make([]string, len(waits))
	for i, n := range waits {
		parts[i] = fmt.Sprintf("%d times: %d", n, byWaits[n])
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// batchAnswer is what became of one catch-up batch.
type batchAnswer int

const (
	batchUnanswered batchAnswer = iota
	batchStored
	batchDeferred
)
