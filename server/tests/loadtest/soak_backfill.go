package main

import (
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

// drainBackfill drives the agent side of a reconnect storm: it requests a drain
// slot, and on GrantBackfill sends up to opts.backfillBatches batches, waiting
// for a MetricBackfillAck between each (one acked batch at a time, matching the
// agent replay engine). A DeferBackfill — the scheduler shedding load — ends the
// drain cleanly with zero batches sent; a read timeout (no scheduler wired) does
// the same. It returns the number of batches acked.
func drainBackfill(codec *protocol.Codec, stream soakStream, opts loadOptions) (int, error) {
	if opts.backfillBatches <= 0 {
		return 0, nil
	}
	pendingCount := opts.backfillBatches * opts.backfillSamplesPerBatch
	if pendingCount < 0 {
		pendingCount = 0
	}
	startTS := time.Now().Add(-time.Duration(pendingCount) * time.Second).Unix()
	reqPayload, err := codec.EncodeControl(buildBackfillSlotRequest(safeUint64(pendingCount), startTS))
	if err != nil {
		return 0, fmt.Errorf("encode slot request: %w", err)
	}
	if err := codec.WriteFrame(stream, protocol.FrameControl, reqPayload); err != nil {
		return 0, fmt.Errorf("write slot request: %w", err)
	}

	decision, err := readControlFrame(codec, stream)
	if err != nil {
		if isTimeout(err) {
			return 0, nil // no scheduler answered within the deadline
		}
		return 0, fmt.Errorf("read backfill decision: %w", err)
	}
	if decision.Type != protocol.MsgGrantBackfill {
		return 0, nil // deferred (or unexpected) — shed load, drain nothing
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

// sendBackfillBatch writes one tiered backfill batch and waits for its ack,
// returning whether the batch was acked (a read timeout or a non-ack reply ends
// the drain without an error, mirroring an expired grant).
func sendBackfillBatch(codec *protocol.Codec, stream soakStream, samplesPerBatch int, startTS int64) (bool, error) {
	samples := buildBackfillSamples(samplesPerBatch, startTS)
	batch := buildBackfillBatch(protocol.BackfillTierRaw10s, samples, samples[len(samples)-1].TS)
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
