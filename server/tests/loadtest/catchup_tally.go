package main

import (
	"fmt"
	"io"
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
}

// catchUpTally is the run-wide count every machine's catch-up adds to.
type catchUpTally struct {
	stored, deferred, unanswered, finished atomic.Int64
	resends, liveConfirmed                 atomic.Int64
	// lastStored is the wall clock of the newest stored batch, in Unix nanoseconds.
	lastStored atomic.Int64
	started    time.Time
}

// count is the tally for the bundle, or nil when no batch was sent, which differs from nought
// stored of nought.
func (t *catchUpTally) count() *CatchUpCount {
	if t == nil {
		return nil
	}
	c := &CatchUpCount{
		Stored: t.stored.Load(), Deferred: t.deferred.Load(), Unanswered: t.unanswered.Load(),
		Complete: t.finished.Load(), ResendRequests: t.resends.Load(), LiveConfirmed: t.liveConfirmed.Load(),
	}
	if c.Stored+c.Deferred+c.Unanswered == 0 {
		return nil
	}
	if last := t.lastStored.Load(); last > 0 && !t.started.IsZero() {
		c.LastStoredSeconds = time.Unix(0, last).Sub(t.started).Seconds()
	}
	return c
}

// confirmed counts one live window the server said it stored.
func (t *catchUpTally) confirmed() {
	if t != nil {
		t.liveConfirmed.Add(1)
	}
}

// resent counts one request to send history again.
func (t *catchUpTally) resent() {
	if t != nil {
		t.resends.Add(1)
	}
}

// caughtUp counts one machine whose every planned batch was stored.
func (t *catchUpTally) caughtUp() {
	if t != nil {
		t.finished.Add(1)
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
}

// batchAnswer is what became of one catch-up batch.
type batchAnswer int

const (
	batchUnanswered batchAnswer = iota
	batchStored
	batchDeferred
)
