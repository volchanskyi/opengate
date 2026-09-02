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

// A fleet's steady-state cost is connections that stay up, not connections that
// open, so a machine that has done its traffic stays in the run rather than
// leaving. This is what it does while it is there: it answers what the server
// asks of it, it joins any session it is handed, and it proves on an interval
// that the connection still exists.
//
// That last part is why the hold writes at all. The heartbeat runs agent to
// server, so a held machine receives nothing from a healthy server either — a
// quiet server and a severed one produce identical silence, and no amount of
// reading separates them.

// holdOpen keeps this machine in the run for opts.holdFor, answering what the
// server sends and proving on an interval that the far side is still there.
// A fleet's steady-state cost is connections that stay up, not connections that
// open; a harness that disconnects immediately never applies it.
//
// A read that times out is the ordinary case — a quiet server has nothing to
// say — so the loop simply reads again until the hold is over.
//
// Reading is why the hold had to start writing. The heartbeat runs agent to
// server, so a held machine receives nothing from a healthy server either: a
// quiet server and a dead one produce the same silence, and no amount of
// reading separates them. A write does. A heartbeat to a QUIC peer that is gone
// fails and names it; one to a live peer is a frame the server already expects
// during the traffic phase, and it keeps the device's status online, which the
// relay scenario beside this run depends on.
func holdOpen(codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if opts.holdFor <= 0 {
		return nil
	}

	// Once before the loop, so a hold shorter than one interval still asks. A
	// run held for seconds is the cheapest kind to make, and it would otherwise
	// be the kind that could not see a severance at all.
	if err := sendHeartbeat(codec, stream); err != nil {
		return err
	}

	deadline := time.Now().Add(opts.holdFor)
	nextBeat := time.Now().Add(holdHeartbeatInterval)
	for time.Now().Before(deadline) {
		if time.Now().After(nextBeat) {
			if err := sendHeartbeat(codec, stream); err != nil {
				return err
			}
			nextBeat = time.Now().Add(holdHeartbeatInterval)
		}

		wait := min(holdReadSlice, time.Until(deadline))
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
	return nil
}

// ErrHeldPeerGone is a machine that lost its connection part way through the
// hold. It is a named error rather than a message so the bundle can count them:
// "the fleet held" and "the fleet was severed and nobody could tell" are the
// two outcomes this whole path exists to separate.
var ErrHeldPeerGone = errors.New("hold open: heartbeat write failed, so this machine's connection is gone")

// sendHeartbeat writes one agent heartbeat, which is how a held machine asks
// whether its connection still exists.
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

// holdHeartbeatInterval is how often a held machine proves its connection.
//
// Well inside any hold a run asks for, and well inside the window a severance
// has to be caught in: the run that found this defect held for 8m30s and lost
// its fleet at the four-minute mark, so an interval of a quarter of a minute
// turns a four-and-a-half-minute blind spot into fifteen seconds. It is also
// far slower than the traffic phase's own writes, so it adds nothing a
// throughput figure would notice.
const holdHeartbeatInterval = 15 * time.Second

// holdReadSlice bounds one read while a machine is held open, so the hold ends
// close to when it was asked to rather than at the next frame the server
// happens to send.
const holdReadSlice = 2 * time.Second

// answerHeldFrame replies to what the server sends a held-open machine. Only
// the frames a load run needs to keep moving are answered; anything else is
// read and discarded, which is what an agent that does not support a capability
// does.
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

// joinRequestedSession opens the machine side of the session the server just
// handed this connection and echoes on it until the operator's side goes away.
//
// It runs on its own goroutine because the relay is a separate connection: the
// control stream must stay readable, or the machine stops answering everything
// else for as long as somebody has a session open.
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

// relaySessionLifetime bounds one simulated session, so a run cannot leave a
// goroutine echoing into a pipe nobody is reading.
const relaySessionLifetime = 5 * time.Minute
