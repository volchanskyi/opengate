package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// deferralWaitCap bounds one wait on the server-supplied retry time.
const deferralWaitCap = 60 * time.Second

// turnAnswerWait is how long a turn request waits for the scheduler's answer before it is asked
// again, as the agent waits; gRPC, Vector and Netdata each give a request a minute.
const turnAnswerWait = 60 * time.Second

// turnAnswerJitter is the most a turn request adds to its wait, so a fleet asks again apart.
const turnAnswerJitter = 5 * time.Second

// buildBackfillSlotRequest asks the server-coordinated scheduler for a drain slot.
func buildBackfillSlotRequest() *protocol.ControlMessage {
	return &protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot}
}

// turnAnswer is what one wait for a drain slot came to.
type turnAnswer struct {
	granted bool
	// ending is why the machine stopped asking, where it was not granted.
	ending catchUpEnding
	// waits is how many times the scheduler told it to wait.
	waits int
}

// awaitSlot asks the scheduler for a drain slot and reports whether this machine may drain.
// A persistent machine waits out each retry time and asks again, up to maxDeferrals.
func awaitSlot(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) (turnAnswer, error) {
	for asked := 0; ; asked++ {
		if err := askForTurn(codec, stream); err != nil {
			return turnAnswer{ending: endedNeverGranted, waits: asked}, err
		}

		decision, err := readTurnAnswer(ctx, codec, stream, opts)
		if err != nil {
			if ctx.Err() != nil {
				return turnAnswer{ending: endedRun, waits: asked}, nil
			}
			return turnAnswer{ending: endedNeverGranted, waits: asked}, fmt.Errorf("read backfill decision: %w", err)
		}

		switch {
		case decision.Type == protocol.MsgGrantBackfill:
			return turnAnswer{granted: true, waits: asked}, nil
		case !opts.retryDeferred || asked >= maxDeferrals:
			return turnAnswer{ending: endedToldToWait, waits: asked + 1}, nil
		}

		if !waitToAskAgain(ctx, decision.RetryAfter) {
			return turnAnswer{ending: endedRun, waits: asked + 1}, nil
		}
	}
}

// readTurnAnswer reads the scheduler's answer to a turn request, asking again each time the wait
// for it runs out while the run goes on; the answer leaves the stream with no read deadline.
func readTurnAnswer(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) (*protocol.ControlMessage, error) {
	for {
		if err := stream.SetReadDeadline(time.Now().Add(turnAnswerWait + jitterUpTo(turnAnswerJitter))); err != nil {
			return nil, fmt.Errorf("set turn deadline: %w", err)
		}
		decision, err := readSlotAnswer(codec, stream)
		if err == nil {
			if err := stream.SetReadDeadline(time.Time{}); err != nil {
				return nil, fmt.Errorf("clear turn deadline: %w", err)
			}
			return decision, nil
		}
		// Only the turn's own wait running out asks again; a connection gone quiet is gone.
		if !errors.Is(err, os.ErrDeadlineExceeded) || ctx.Err() != nil {
			return nil, err
		}
		opts.catchUp.askedAgain()
		if err := askForTurn(codec, stream); err != nil {
			return nil, err
		}
	}
}

// askForTurn writes one request for a drain slot.
func askForTurn(codec *protocol.Codec, stream soakStream) error {
	payload, err := codec.EncodeControl(buildBackfillSlotRequest())
	if err != nil {
		return fmt.Errorf("encode slot request: %w", err)
	}
	if err := codec.WriteFrame(stream, protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write slot request: %w", err)
	}
	return nil
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
