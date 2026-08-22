package main

import (
	"bytes"
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
}

func (b *deadlineBuffer) Read(p []byte) (int, error) {
	if b.in.Len() == 0 {
		b.drained = true
		return 0, timeoutError{}
	}
	return b.in.Read(p)
}

func (b *deadlineBuffer) Write(p []byte) (int, error)     { return b.out.Write(p) }
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
	reply := readControl(t, codec, stream.out)
	assert.Equal(t, protocol.MsgDeviceLogsResponse, reply.Type)
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
