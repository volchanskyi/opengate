package main

import (
	"context"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

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

// answerResendRequest catches up from the time a re-send request names, as the agent forgets what
// it was told is stored after it; the whole fleet's request is spread by its retry time.
func answerResendRequest(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions,
	msg *protocol.ControlMessage,
) error {
	opts.catchUp.resent()
	if opts.backfillBatches <= 0 && opts.backfillGap <= 0 {
		return nil
	}
	if !waitToAskAgain(ctx, msg.RetryAfter) {
		return nil
	}
	opts.backfillBatches = 0
	opts.backfillGap = time.Since(time.Unix(msg.SinceTS, 0)).Truncate(time.Minute) + time.Minute
	opts.retryDeferred = true
	opts.resending = true
	_, err := drainBackfill(ctx, codec, stream, opts)
	return err
}

// drainBackfill sends the machine's planned batches under a grant, one answered at a time, returns
// how many were stored, and records why a first catch-up on the connection ended.
func drainBackfill(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) (int, error) {
	if opts.backfillBatches <= 0 && opts.backfillGap <= 0 {
		return 0, nil
	}
	batches := plannedBatches(opts, time.Now())
	if len(batches) == 0 {
		return 0, nil
	}
	stored, end, err := drainPlanned(ctx, codec, stream, opts, batches)
	// A machine sending again caught up once already, so it is not counted a second time.
	if !opts.resending {
		opts.catchUp.ended(opts.machine, end)
	}
	return stored, err
}

// drainPlanned sends batches under a grant and says how the catch-up ended.
func drainPlanned(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions,
	batches [][]protocol.BackfillSample,
) (int, catchUpEnd, error) {
	turn, err := awaitSlot(ctx, codec, stream, opts)
	waits := turn.waits
	if err != nil || !turn.granted {
		return 0, catchUpEnd{why: turn.ending, waits: waits}, err
	}

	stored, deferrals := 0, 0
	for stored < len(batches) {
		answer, retryAfter, err := sendBackfillBatch(codec, stream, batches[stored])
		opts.catchUp.record(answer)
		if err != nil {
			return stored, catchUpEnd{why: endedConnection, waits: waits}, err
		}
		switch answer {
		case batchStored:
			stored++
			continue
		case batchUnanswered:
			return stored, catchUpEnd{why: endedConnection, waits: waits}, nil
		}
		deferrals++
		waits++
		if !opts.retryDeferred || deferrals > maxDeferrals {
			return stored, catchUpEnd{why: endedToldToWait, waits: waits}, nil
		}
		if !waitToAskAgain(ctx, retryAfter) {
			return stored, catchUpEnd{why: endedRun, waits: waits}, nil
		}
		turn, err = awaitSlot(ctx, codec, stream, opts)
		waits += turn.waits
		if err != nil || !turn.granted {
			return stored, catchUpEnd{why: turn.ending, waits: waits}, err
		}
	}
	return stored, catchUpEnd{why: endedCaughtUp, waits: waits}, nil
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
