package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// deadlineBuffer stands in for a QUIC stream; a read past its data reports a timeout.
type deadlineBuffer struct {
	in  *bytes.Buffer
	out *bytes.Buffer
	// drained turns true once a read finds no data left.
	drained bool

	// writeErr is returned by Write, as a QUIC stream does once its peer has died.
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
	require.NoError(t, holdOpen(context.Background(), &protocol.Codec{}, stream, loadOptions{}))

	assert.Less(t, time.Since(start), 200*time.Millisecond)
}

func TestAHeldMachineStaysForItsWholeHold(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	start := time.Now()
	require.NoError(t, holdOpen(context.Background(), &protocol.Codec{}, stream, loadOptions{holdFor: 150 * time.Millisecond}))

	assert.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)
	assert.True(t, stream.drained, "a held machine keeps reading rather than closing")
}

func TestAHeldMachineLeavesWhenTheRunWindsItDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = holdOpen(ctx, &protocol.Codec{}, stream, loadOptions{holdFor: time.Hour})
	}()

	select {
	case <-done:
		t.Fatal("a machine inside its hold must not leave before the run says so")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("a machine the run wound down must leave, not serve out a hold as long as the run")
	}
}

func TestAQuietServerDoesNotEndTheHold(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	assert.NoError(t, holdOpen(context.Background(), &protocol.Codec{}, stream, loadOptions{holdFor: 100 * time.Millisecond}))
}

func TestAHoldFailsWhenItsPeerDiesPartWayThrough(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}, writeErr: errors.New("connection reset")}

	err := holdOpen(context.Background(), &protocol.Codec{}, stream, loadOptions{holdFor: time.Second})

	require.Error(t, err, "a hold against a peer that is gone must not report success")
	assert.ErrorIs(t, err, ErrHeldPeerGone,
		"the severance is named, so the bundle can count the machines it took")
}

func TestAHeldMachineProvesItsPeerIsAlive(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	require.NoError(t, holdOpen(context.Background(), codec, stream, loadOptions{holdFor: 100 * time.Millisecond}))

	require.NotZero(t, stream.out.Len(), "a hold that wrote nothing cannot tell a quiet peer from an absent one")
	beat := readControl(t, codec, stream.out)
	assert.Equal(t, protocol.MsgAgentHeartbeat, beat.Type)
	assert.NotZero(t, beat.Timestamp, "a heartbeat carries when the machine sent it")
	assert.Less(t, holdHeartbeatInterval, 8*time.Minute,
		"the interval has to be well inside a hold, or a severance is still invisible for most of one")
}

func TestAShortHoldStillAsksOnce(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}, writeErr: errors.New("connection reset")}

	err := holdOpen(context.Background(), &protocol.Codec{}, stream, loadOptions{holdFor: 50 * time.Millisecond})

	require.Error(t, err, "a hold too short for a full interval must still prove its peer")
}

func TestAHeldMachineAnswersARawLogPull(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}
	writeControl(t, codec, stream.in, &protocol.ControlMessage{
		Type: protocol.MsgRequestDeviceLogs, LogLimit: 5,
	})

	require.NoError(t, holdOpen(context.Background(), codec, stream, loadOptions{holdFor: 120 * time.Millisecond}))

	require.NotZero(t, stream.out.Len(), "a held machine must answer the pull")
	reply := readControlOfType(t, codec, stream.out, protocol.MsgDeviceLogsResponse)
	assert.Len(t, reply.LogEntries, 5)
}

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

func TestAnUnhandledFrameIsDroppedRatherThanFailingTheRun(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	err := answerHeldFrame(codec, stream.out, &protocol.ControlMessage{
		Type: protocol.MsgRequestHardwareReport,
	}, loadOptions{})

	require.NoError(t, err)
	assert.Zero(t, stream.out.Len())
}

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

// readControlOfType skips the heartbeats a held machine writes down the same stream.
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
