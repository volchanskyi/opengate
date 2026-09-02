package main

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// A fleet's steady-state cost is connections that stay up. A harness that
// connects, writes and leaves measures the accept path and nothing after it —
// and it leaves a generator on the other side with no machine to open a session
// against.

// deadlineBuffer is a read-write pair with a deadline, standing in for a QUIC
// stream. A read past the deadline reports a timeout the way the network does,
// which is the ordinary case a quiet server produces.
type deadlineBuffer struct {
	in  *bytes.Buffer
	out *bytes.Buffer
	// expired makes every read after the first report a timeout, so a hold
	// spins on the quiet path rather than on real frames.
	drained bool

	// writeErr is what the far side does when the connection is gone. A QUIC
	// stream whose peer has died refuses the write; a live peer accepts it.
	writeErr error
}

func (b *deadlineBuffer) Read(p []byte) (int, error) {
	if b.in.Len() == 0 {
		b.drained = true
		return 0, timeoutError{}
	}
	return b.in.Read(p)
}

func (b *deadlineBuffer) Write(p []byte) (int, error) {
	if b.writeErr != nil {
		return 0, b.writeErr
	}
	return b.out.Write(p)
}

func (b *deadlineBuffer) SetReadDeadline(time.Time) error { return nil }

// timeoutError reports itself as a network timeout.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestAMachineWithNoHoldLeavesImmediately(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	start := time.Now()
	require.NoError(t, holdOpen(&protocol.Codec{}, stream, loadOptions{}))

	assert.Less(t, time.Since(start), 200*time.Millisecond)
}

func TestAHeldMachineStaysForItsWholeHold(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	start := time.Now()
	require.NoError(t, holdOpen(&protocol.Codec{}, stream, loadOptions{holdFor: 150 * time.Millisecond}))

	assert.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)
	assert.True(t, stream.drained, "a held machine keeps reading rather than closing")
}

// A quiet server is the ordinary case, so a read that times out must not end
// the hold or be reported as a fault.
func TestAQuietServerDoesNotEndTheHold(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	assert.NoError(t, holdOpen(&protocol.Codec{}, stream, loadOptions{holdFor: 100 * time.Millisecond}))
}

// The defect this closes: a hold that only ever reads cannot tell a quiet
// server from a dead one.
//
// The heartbeat runs agent to server, so a held machine receives nothing from a
// healthy server either — reading harder can never separate the two. Writing
// can: a write to a peer that is gone fails, which turns a severance nothing
// could see into a named error. The run that found this reported
// "Agents: 100/100 succeeded, Failures: 0" for an 8m30s hold whose connections
// were severed at the four-minute mark.
func TestAHoldFailsWhenItsPeerDiesPartWayThrough(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}, writeErr: errors.New("connection reset")}

	err := holdOpen(&protocol.Codec{}, stream, loadOptions{holdFor: time.Second})

	require.Error(t, err, "a hold against a peer that is gone must not report success")
	assert.ErrorIs(t, err, ErrHeldPeerGone,
		"the severance is named, so the bundle can count the machines it took")
}

// A hold does not just fail on a dead peer; it proves the peer alive on a live
// one, or the arm above proves nothing about the ordinary case.
func TestAHeldMachineProvesItsPeerIsAlive(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	require.NoError(t, holdOpen(codec, stream, loadOptions{holdFor: 100 * time.Millisecond}))

	require.NotZero(t, stream.out.Len(), "a hold that wrote nothing cannot tell a quiet peer from an absent one")
	beat := readControl(t, codec, stream.out)
	assert.Equal(t, protocol.MsgAgentHeartbeat, beat.Type)
	assert.NotZero(t, beat.Timestamp, "a heartbeat carries when the machine sent it")
	assert.Less(t, holdHeartbeatInterval, 8*time.Minute,
		"the interval has to be well inside a hold, or a severance is still invisible for most of one")
}

// A hold shorter than one interval still writes once. A run whose hold is
// seconds long would otherwise never ask, and the same severance would come
// back reported as a success on exactly the runs that are cheapest to make.
func TestAShortHoldStillAsksOnce(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}, writeErr: errors.New("connection reset")}

	err := holdOpen(&protocol.Codec{}, stream, loadOptions{holdFor: 50 * time.Millisecond})

	require.Error(t, err, "a hold too short for a full interval must still prove its peer")
}

// A held machine still answers what the server asks of it. A machine that goes
// silent once connected is not a machine anybody has.
func TestAHeldMachineAnswersARawLogPull(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}
	writeControl(t, codec, stream.in, &protocol.ControlMessage{
		Type: protocol.MsgRequestDeviceLogs, LogLimit: 5,
	})

	require.NoError(t, holdOpen(codec, stream, loadOptions{holdFor: 120 * time.Millisecond}))

	require.NotZero(t, stream.out.Len(), "a held machine must answer the pull")
	reply := readControlOfType(t, codec, stream.out, protocol.MsgDeviceLogsResponse)
	assert.Len(t, reply.LogEntries, 5)
}

// A session request is only acted on when the run asked for relay coverage.
// Joining one otherwise would open connections a profile never declared.
func TestASessionRequestIsIgnoredUnlessRelayCoverageWasAskedFor(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	err := answerHeldFrame(codec, stream.out, &protocol.ControlMessage{
		Type:     protocol.MsgSessionRequest,
		Token:    "tok",
		RelayURL: "ws://localhost:8080/ws/relay/tok",
	}, loadOptions{})

	require.NoError(t, err)
	assert.Zero(t, stream.out.Len(), "an unasked-for session must produce no traffic at all")
}

// A frame the machine has nothing to say about is read and dropped, which is
// what an agent without that capability does — not an error that ends the run.
func TestAnUnhandledFrameIsDroppedRatherThanFailingTheRun(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	err := answerHeldFrame(codec, stream.out, &protocol.ControlMessage{
		Type: protocol.MsgRequestHardwareReport,
	}, loadOptions{})

	require.NoError(t, err)
	assert.Zero(t, stream.out.Len())
}

// A session request naming a target the run may not reach is refused, so a
// field on the wire cannot send the generator somewhere it is forbidden to go.
func TestAHeldMachineRefusesASessionOnADisallowedTarget(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	err := answerHeldFrame(codec, stream.out, &protocol.ControlMessage{
		Type:     protocol.MsgSessionRequest,
		Token:    "tok",
		RelayURL: "ws://opengate-server.opengate.svc.cluster.local:8080/ws/relay/tok",
	}, loadOptions{relaySessions: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

// readControlOfType reads frames until it finds one of the wanted type. A held
// machine writes its own heartbeats down the same stream, so a test about what
// it answers has to look past what it asks.
func readControlOfType(t *testing.T, codec *protocol.Codec, buf *bytes.Buffer,
	want protocol.ControlMessageType,
) *protocol.ControlMessage {
	t.Helper()
	for buf.Len() > 0 {
		msg := readControl(t, codec, buf)
		if msg.Type == want {
			return msg
		}
	}
	require.FailNowf(t, "frame not found", "the machine never wrote a %s", want)
	return nil
}
