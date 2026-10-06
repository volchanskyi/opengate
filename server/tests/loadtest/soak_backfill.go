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

// drainBackfill requests a drain slot and, once granted, sends up to opts.backfillBatches
// batches one acked batch at a time, returning how many were acked.
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

// sendBackfillBatch writes one tiered backfill batch and reports whether it was acked; a read
// timeout or a non-ack reply ends the drain without an error.
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
