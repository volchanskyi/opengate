package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// holdOpen ends when the run winds the machine down or its hold elapses, whichever is first.
func holdOpen(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if opts.holdFor <= 0 {
		return nil
	}
	deadline := time.Now().Add(opts.holdFor)
	return proveConnection(codec, stream, opts, func() time.Duration {
		if ctx.Err() != nil {
			return 0
		}
		return time.Until(deadline)
	})
}

// proveUntilWoundDown keeps heartbeating until the run winds the machine down, because the
// heartbeat write is what raises ErrHeldPeerGone.
func proveUntilWoundDown(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if opts.holdFor <= 0 {
		return nil
	}
	return proveConnection(codec, stream, opts, func() time.Duration {
		if ctx.Err() != nil {
			return 0
		}
		return holdReadSlice
	})
}

// proveConnection answers the server and heartbeats while left reports time remaining; a read
// timeout is the ordinary quiet-server case.
func proveConnection(codec *protocol.Codec, stream soakStream, opts loadOptions,
	left func() time.Duration,
) error {
	// One heartbeat before the loop lets a stay shorter than one interval detect a severance.
	if err := sendHeartbeat(codec, stream); err != nil {
		return err
	}

	nextBeat := time.Now().Add(holdHeartbeatInterval)
	for {
		remaining := left()
		if remaining <= 0 {
			return nil
		}
		if time.Now().After(nextBeat) {
			if err := sendHeartbeat(codec, stream); err != nil {
				return err
			}
			nextBeat = time.Now().Add(holdHeartbeatInterval)
		}

		wait := min(holdReadSlice, remaining)
		if err := stream.SetReadDeadline(time.Now().Add(wait)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}
		msg, err := readControlFrame(codec, stream)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return fmt.Errorf("hold open: %w", err)
		}
		if err := answerHeldFrame(codec, stream, msg, opts); err != nil {
			return err
		}
	}
}

// ErrHeldPeerGone marks a heartbeat write that failed because the machine's connection is gone.
var ErrHeldPeerGone = errors.New("hold open: heartbeat write failed, so this machine's connection is gone")

func sendHeartbeat(codec *protocol.Codec, w io.Writer) error {
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}
	if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("%w: %w", ErrHeldPeerGone, err)
	}
	return nil
}

const holdHeartbeatInterval = 15 * time.Second

// holdReadSlice bounds one read so the hold ends close to its deadline.
const holdReadSlice = 2 * time.Second

// answerHeldFrame answers device-log and session requests; other frames are discarded.
func answerHeldFrame(codec *protocol.Codec, w io.Writer, msg *protocol.ControlMessage, opts loadOptions) error {
	switch msg.Type {
	case protocol.MsgRequestDeviceLogs:
		payload, err := codec.EncodeControl(buildDeviceLogsResponse(int(msg.LogLimit)))
		if err != nil {
			return fmt.Errorf("encode device logs response: %w", err)
		}
		if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
			return fmt.Errorf("write device logs response: %w", err)
		}
	case protocol.MsgSessionRequest:
		if !opts.relaySessions {
			return nil
		}
		return joinRequestedSession(msg, opts.sessionsJoined)
	}
	return nil
}

// joinRequestedSession echoes on its own goroutine so the control stream stays readable while
// the relay session is open.
func joinRequestedSession(msg *protocol.ControlMessage, counter *atomic.Int64) error {
	req, err := RelayRequestFrom(msg)
	if err != nil {
		return fmt.Errorf("session request: %w", err)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), relaySessionLifetime)
		defer cancel()

		joined, err := JoinRelay(ctx, req)
		if err != nil {
			return
		}
		if counter != nil {
			counter.Add(1)
		}
		defer func() { _ = joined.Close() }()
		_ = joined.Echo(ctx)
	}()
	return nil
}

// relaySessionLifetime bounds one simulated session so no goroutine echoes into an unread pipe.
const relaySessionLifetime = 5 * time.Minute
