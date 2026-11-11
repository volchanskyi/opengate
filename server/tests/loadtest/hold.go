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
	return proveConnection(ctx, codec, stream, opts, func() time.Duration {
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
	return proveConnection(ctx, codec, stream, opts, func() time.Duration {
		if ctx.Err() != nil {
			return 0
		}
		return holdReadSlice
	})
}

// proveConnection answers the server and heartbeats while left reports time remaining; a read
// timeout is the ordinary quiet-server case.
func proveConnection(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions,
	left func() time.Duration,
) error {
	// One heartbeat before the loop lets a stay shorter than one interval detect a severance.
	if err := sendHeartbeat(codec, stream); err != nil {
		return err
	}

	nextBeat := time.Now().Add(holdHeartbeatInterval)
	// Each machine sends at its own second of the period, so a fleet that came back together
	// arrives evenly through it.
	nextWindow := time.Now()
	if opts.liveEvery > 0 {
		nextWindow = nextWindow.Add(liveSendDelay(nextWindow, opts.liveSecond) % opts.liveEvery)
	}
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
		if opts.liveEvery > 0 {
			if !time.Now().Before(nextWindow) {
				if err := sendLiveWindow(codec, stream, opts); err != nil {
					return err
				}
				nextWindow = nextWindow.Add(opts.liveEvery)
			}
			wait = min(wait, time.Until(nextWindow))
		}

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
		if msg.Type == protocol.MsgDeferBackfill && msg.SinceTS != 0 {
			if err := answerResendRequest(ctx, codec, stream, opts, msg); err != nil {
				return err
			}
			continue
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
		return joinRequestedSession(msg, opts.sessionsJoined, opts.helpers)
	case protocol.MsgLiveWindowStored:
		opts.catchUp.confirmed()
	}
	return nil
}

// liveSendSecond is the second of each minute a machine named name sends its live window at, as
// the agent takes it from its own id.
func liveSendSecond(name string) int {
	hash := uint64(0xcbf29ce484222325)
	for _, b := range []byte(name) {
		hash = (hash ^ uint64(b)) * 0x100000001b3
	}
	return int(hash % 60)
}

// liveSendDelay is how long from now until the clock next reads second past a minute; never zero.
func liveSendDelay(now time.Time, second int) time.Duration {
	into := time.Duration(now.UnixMilli()%60_000) * time.Millisecond
	wait := (time.Duration(second)*time.Second + time.Minute - into) % time.Minute
	if wait == 0 {
		return time.Minute
	}
	return wait
}

// joinRequestedSession echoes on its own goroutine so the control stream stays readable while
// the relay session is open. Beside the join it asks the session's helpers what an attempt asks.
func joinRequestedSession(msg *protocol.ControlMessage, counter *atomic.Int64, helpers *helperTally) error {
	req, err := RelayRequestFrom(msg)
	if err != nil {
		return fmt.Errorf("session request: %w", err)
	}
	helperAddrs, err := helperAddresses(msg.IceServers)
	if err != nil {
		return fmt.Errorf("session request: %w", err)
	}

	// The requests outlive a short session, as an attempt's gathering does; the schedule ends them.
	if helpers != nil && len(helperAddrs) > 0 {
		helpers.start(context.Background(), helperAddrs, rfc8489Schedule)
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

// sendLiveWindow writes a window stamped at its send second, past the arrival window's ten-second
// floor.
func sendLiveWindow(codec *protocol.Codec, w io.Writer, _ loadOptions) error {
	ts := time.Now().Unix()
	payload, err := codec.EncodeControl(buildDefaultMetricWindow(ts))
	if err != nil {
		return fmt.Errorf("encode live window: %w", err)
	}
	if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write live window: %w", err)
	}
	return nil
}
