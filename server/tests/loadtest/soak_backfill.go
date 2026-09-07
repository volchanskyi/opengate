package main

import (
	"context"
	"fmt"
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

// buildBackfillSamples builds n pre-rolled historical samples at 10 s spacing
// starting at startTS, cycling through the default host dims. Timestamps are
// strictly increasing so the batch replays recent-first in true time order.
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

// buildBackfillBatch builds one tiered backfill batch preserving each sample's
// original timestamp, with the tier + cursor the ack echoes so the agent
// advances the right durable per-tier watermark.
func buildBackfillBatch(tier protocol.BackfillTier, samples []protocol.BackfillSample, cursor int64) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            tier,
		BackfillSamples: samples,
		Cursor:          cursor,
	}
}

// maxDeferrals bounds how many times a persistent machine will be told to wait
// before it stops asking on this connection. The scheduler shortens a deferred
// machine's wait the longer it has waited, so a machine still being deferred
// after this many rounds is a finding about the scheduler rather than a thing
// to keep a connection asking about for the life of the run.
const maxDeferrals = 8

// deferralWaitCap bounds one wait. The retry time comes from the server, and a
// machine that took an implausible one at face value would sit out the window
// the run is measuring.
const deferralWaitCap = 60 * time.Second

// drainBackfill drives the agent side of a reconnect storm: it requests a drain
// slot, and on GrantBackfill sends up to opts.backfillBatches batches, waiting
// for a MetricBackfillAck between each (one acked batch at a time, matching the
// agent replay engine). A read timeout (no scheduler wired) ends the drain
// cleanly with zero batches sent. It returns the number of batches acked.
//
// What a DeferBackfill means depends on what the run is measuring. A load run
// sheds the load: the deferral path is the thing under measurement and a
// machine that queued through it would hide the shedding. A machine standing in
// for a customer's workstation waits out the retry time the server handed it
// and asks again, which is what a shipped agent does — and without it, a site
// of twenty catches up four machines deep and then stops, because the server
// admits four per customer and the other sixteen would never ask a second time.
func drainBackfill(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) (int, error) {
	if opts.backfillBatches <= 0 {
		return 0, nil
	}
	pendingCount := opts.backfillBatches * opts.backfillSamplesPerBatch
	if pendingCount < 0 {
		pendingCount = 0
	}
	startTS := time.Now().Add(-time.Duration(pendingCount) * time.Second).Unix()

	granted, err := awaitSlot(ctx, codec, stream, opts, safeUint64(pendingCount), startTS)
	if err != nil || !granted {
		return 0, err
	}

	sent := 0
	for i := 0; i < opts.backfillBatches; i++ {
		acked, err := sendBackfillBatch(codec, stream, opts.backfillSamplesPerBatch, startTS+int64(i))
		if err != nil {
			return sent, err
		}
		if !acked {
			return sent, nil // grant expired or ack lost — stop draining
		}
		sent++
	}
	return sent, nil
}

// awaitSlot asks the scheduler for a drain slot and answers whether this
// machine may drain. A machine that is not persistent asks once; one that is
// waits out the retry time it was handed and asks again, up to maxDeferrals.
//
// A reply that is neither a grant nor a deferral, and a read that timed out
// because nothing is scheduling, both end the asking without a drain and
// without an error: neither says the machine failed.
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

		decision, err := readControlFrame(codec, stream)
		if err != nil {
			if isTimeout(err) {
				return false, nil // no scheduler answered within the deadline
			}
			return false, fmt.Errorf("read backfill decision: %w", err)
		}

		switch {
		case decision.Type == protocol.MsgGrantBackfill:
			return true, nil
		case decision.Type != protocol.MsgDeferBackfill:
			return false, nil
		case !opts.retryDeferred || asked >= maxDeferrals:
			return false, nil
		}

		if !waitToAskAgain(ctx, decision.RetryAfter) {
			return false, nil
		}
	}
}

// waitToAskAgain sits out the server's retry time, and reports whether the run
// is still going afterwards. A run that wound down while a machine was waiting
// does not get one more question out of it.
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

// sendBackfillBatch writes one tiered backfill batch and waits for its ack,
// returning whether the batch was acked (a read timeout or a non-ack reply ends
// the drain without an error, mirroring an expired grant).
func sendBackfillBatch(codec *protocol.Codec, stream soakStream, samplesPerBatch int, startTS int64) (bool, error) {
	samples := buildBackfillSamples(samplesPerBatch, startTS)
	batch := buildBackfillBatch(protocol.BackfillTierRecent60s, samples, samples[len(samples)-1].TS)
	payload, err := codec.EncodeControl(batch)
	if err != nil {
		return false, fmt.Errorf("encode backfill batch: %w", err)
	}
	if err := codec.WriteFrame(stream, protocol.FrameControl, payload); err != nil {
		return false, fmt.Errorf("write backfill batch: %w", err)
	}
	ack, err := readControlFrame(codec, stream)
	if err != nil {
		if isTimeout(err) {
			return false, nil
		}
		return false, fmt.Errorf("read backfill ack: %w", err)
	}
	return ack.Type == protocol.MsgMetricBackfillAck, nil
}
