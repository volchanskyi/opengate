package main

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// safeUint64 narrows a non-negative int to uint64, clamping negatives to 0 so
// the conversion cannot wrap (gosec G115).
func safeUint64(v int) uint64 {
	if v <= 0 {
		return 0
	}
	return uint64(v)
}

// buildBackfillSamples builds n historical samples at 10 s spacing from startTS, cycling the
// default host dims with strictly increasing timestamps.
func buildBackfillSamples(n int, startTS int64) []protocol.BackfillSample {
	samples := make([]protocol.BackfillSample, n)
	for i := 0; i < n; i++ {
		samples[i] = protocol.BackfillSample{
			Name:  defaultMetricDimNames[i%len(defaultMetricDimNames)],
			TS:    startTS + int64(i)*10,
			Value: float64(i % 100),
		}
	}
	return samples
}

// buildBackfillSlotRequest asks the server-coordinated scheduler for a drain
// slot, carrying the backlog hints the scheduler biases on.
func buildBackfillSlotRequest(pending uint64, oldest int64) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:           protocol.MsgRequestBackfillSlot,
		PendingSamples: pending,
		OldestTS:       oldest,
	}
}

// buildBackfillBatch builds one tiered backfill batch that keeps each sample's timestamp and
// carries the tier and cursor the ack echoes.
func buildBackfillBatch(tier protocol.BackfillTier, samples []protocol.BackfillSample, cursor int64) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            tier,
		BackfillSamples: samples,
		Cursor:          cursor,
	}
}

// maxDeferrals bounds how many times a persistent machine is told to wait before it stops
// asking on this connection.
const maxDeferrals = 8

// deferralWaitCap bounds one wait on the server-supplied retry time.
const deferralWaitCap = 60 * time.Second

// CatchUpCount is how the server answered the run's catch-up batches, how many machines caught up
// whole, and when, counted from the run's start, the last batch was stored.
type CatchUpCount struct {
	Stored            int64   `json:"stored"`
	Deferred          int64   `json:"deferred"`
	Unanswered        int64   `json:"unanswered"`
	Complete          int64   `json:"complete,omitempty"`
	LastStoredSeconds float64 `json:"last_stored_seconds,omitempty"`
}

// catchUpTally is the run-wide count every machine's catch-up adds to.
type catchUpTally struct {
	stored, deferred, unanswered, finished atomic.Int64
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
		Complete: t.finished.Load(),
	}
	if c.Stored+c.Deferred+c.Unanswered == 0 {
		return nil
	}
	if last := t.lastStored.Load(); last > 0 && !t.started.IsZero() {
		c.LastStoredSeconds = time.Unix(0, last).Sub(t.started).Seconds()
	}
	return c
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

// drainBackfill sends the machine's planned batches under a grant, one answered at a time, and
// returns how many were stored; a persistent machine resends a deferred batch after a regrant.
func drainBackfill(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) (int, error) {
	if opts.backfillBatches <= 0 && opts.backfillGap <= 0 {
		return 0, nil
	}
	batches := plannedBatches(opts, time.Now())
	if len(batches) == 0 {
		return 0, nil
	}
	pending := 0
	for _, batch := range batches {
		pending += len(batch)
	}
	oldest := batches[0][0].TS

	granted, err := awaitSlot(ctx, codec, stream, opts, safeUint64(pending), oldest)
	if err != nil || !granted {
		return 0, err
	}

	stored, deferrals := 0, 0
	for stored < len(batches) {
		answer, retryAfter, err := sendBackfillBatch(codec, stream, batches[stored])
		opts.catchUp.record(answer)
		if err != nil {
			return stored, err
		}
		switch answer {
		case batchStored:
			stored++
			continue
		case batchUnanswered:
			return stored, nil
		}
		deferrals++
		if !opts.retryDeferred || deferrals > maxDeferrals || !waitToAskAgain(ctx, retryAfter) {
			return stored, nil
		}
		if granted, err = awaitSlot(ctx, codec, stream, opts, safeUint64(pending), oldest); err != nil || !granted {
			return stored, err
		}
	}
	opts.catchUp.caughtUp()
	return stored, nil
}

// awaitSlot asks the scheduler for a drain slot and reports whether this machine may drain.
// A persistent machine waits out each retry time and asks again, up to maxDeferrals.
func awaitSlot(
	ctx context.Context, codec *protocol.Codec, stream soakStream,
	opts loadOptions, pending uint64, oldest int64,
) (bool, error) {
	for asked := 0; ; asked++ {
		reqPayload, err := codec.EncodeControl(buildBackfillSlotRequest(pending, oldest))
		if err != nil {
			return false, fmt.Errorf("encode slot request: %w", err)
		}
		if err := codec.WriteFrame(stream, protocol.FrameControl, reqPayload); err != nil {
			return false, fmt.Errorf("write slot request: %w", err)
		}

		decision, err := readSlotAnswer(codec, stream)
		if err != nil {
			if isTimeout(err) {
				return false, nil // no scheduler answered within the deadline
			}
			return false, fmt.Errorf("read backfill decision: %w", err)
		}

		switch {
		case decision.Type == protocol.MsgGrantBackfill:
			return true, nil
		case !opts.retryDeferred || asked >= maxDeferrals:
			return false, nil
		}

		if !waitToAskAgain(ctx, decision.RetryAfter) {
			return false, nil
		}
	}
}

// readSlotAnswer reads frames until the answer to a request for permission, passing over any other
// control message the server pushes meanwhile.
func readSlotAnswer(codec *protocol.Codec, stream soakStream) (*protocol.ControlMessage, error) {
	for {
		reply, err := readControlFrame(codec, stream)
		if err != nil {
			return nil, err
		}
		if reply.Type == protocol.MsgGrantBackfill || reply.Type == protocol.MsgDeferBackfill {
			return reply, nil
		}
	}
}

// waitToAskAgain sits out the server's retry time and reports whether the run is still going.
func waitToAskAgain(ctx context.Context, retryAfter uint32) bool {
	wait := time.Duration(retryAfter) * time.Second
	if wait > deferralWaitCap {
		wait = deferralWaitCap
	}
	if wait <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// sendBackfillBatch writes one tiered backfill batch and reads frames until its answer, passing
// over any other control message; a read timeout leaves the batch unanswered without an error.
func sendBackfillBatch(codec *protocol.Codec, stream soakStream, samples []protocol.BackfillSample) (batchAnswer, uint32, error) {
	batch := buildBackfillBatch(protocol.BackfillTierRecent60s, samples, samples[len(samples)-1].TS)
	payload, err := codec.EncodeControl(batch)
	if err != nil {
		return batchUnanswered, 0, fmt.Errorf("encode backfill batch: %w", err)
	}
	if err := codec.WriteFrame(stream, protocol.FrameControl, payload); err != nil {
		return batchUnanswered, 0, fmt.Errorf("write backfill batch: %w", err)
	}
	for {
		reply, err := readControlFrame(codec, stream)
		if err != nil {
			if isTimeout(err) {
				return batchUnanswered, 0, nil
			}
			return batchUnanswered, 0, fmt.Errorf("read backfill answer: %w", err)
		}
		switch reply.Type {
		case protocol.MsgMetricBackfillAck:
			return batchStored, 0, nil
		case protocol.MsgDeferBackfill:
			return batchDeferred, reply.RetryAfter, nil
		}
	}
}
