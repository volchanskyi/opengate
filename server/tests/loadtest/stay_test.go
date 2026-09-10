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

// The stay is what comes after the hold, and the run owns it.
//
// A machine's declared hold is a bound on its traffic, not on how long the run
// wants it in the fleet: a profile holds its machines to the end of the walk, so
// the hold running out is nowhere near the end of the stay. What a machine does
// in between is here — it keeps proving its connection, it answers what the
// server asks of it, and it leaves when the run winds it down.

// A machine with no hold asked of it leaves when its traffic is done.
//
// It used to wait for its context instead, whatever the hold said. A phase's
// round trip is a machine with no hold and a thirty-second budget, so every one
// of them sat for thirty seconds after it had already registered: twenty round
// trips across a two-phase profile turned three and a half declared minutes
// into fifteen and forty-two, and a flat run with no hold spent thirty seconds
// per machine holding a connection nobody had asked it to hold.
func TestAMachineWithNoHoldLeavesWhenItsTrafficIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = proveUntilWoundDown(ctx, &protocol.Codec{}, stream, loadOptions{holdFor: 0})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a machine asked to hold for nothing must not wait for the run to end")
	}
	assert.Zero(t, stream.out.Len(), "a machine that left wrote nothing after its traffic")
}

// A machine the run is holding stays until the run winds it down. Leaving early
// would drop the fleet's level between the end of its traffic and the end of
// the phase, and the level is what the phase is measuring.
func TestAHeldMachineStaysUntilTheRunWindsItDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = proveUntilWoundDown(ctx, &protocol.Codec{}, stream, loadOptions{holdFor: time.Minute})
	}()

	select {
	case <-done:
		t.Fatal("a held machine must not leave before the run says so")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("a held machine leaves when the run winds it down")
	}
}

// The defect D29 names: a machine went quiet the moment its declared hold
// elapsed, and the run holds it far longer than that.
//
// It kept its connection — the server keep-alives every thirty seconds against
// a ninety-second idle timeout, and a machine's online status follows the
// connection rather than the heartbeat — so nothing looked wrong. What it lost
// was the only detector of a severed fleet: ErrHeldPeerGone is raised by the
// write, and a machine that has stopped writing cannot raise it. In a profiled
// run the blind window is every minute past -hold, which for the nightly sweep
// is most of the walk.
func TestAMachineHeldPastItsHoldKeepsProvingItsConnection(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*holdReadSlice)
	defer cancel()

	require.NoError(t, proveUntilWoundDown(ctx, codec, stream, loadOptions{holdFor: time.Minute}))

	require.NotZero(t, stream.out.Len(),
		"a machine still in the run and no longer writing cannot tell a quiet server from a severed one")
	beat := readControl(t, codec, stream.out)
	assert.Equal(t, protocol.MsgAgentHeartbeat, beat.Type)
}

// The other half of the arm above: past the hold, a severance is still named
// rather than reported as a machine that behaved.
func TestASeveranceAfterTheHoldIsStillReported(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}, writeErr: errors.New("connection reset")}

	err := proveUntilWoundDown(context.Background(), &protocol.Codec{}, stream,
		loadOptions{holdFor: time.Minute})

	require.Error(t, err, "a machine whose connection is gone must not report success because its hold had ended")
	assert.ErrorIs(t, err, ErrHeldPeerGone)
}

// A machine still in the run answers what the server asks of it, past its hold
// as much as during it. One that only heartbeats is a machine no technician
// can pull a log from.
func TestAMachineHeldPastItsHoldStillAnswersTheServer(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}
	writeControl(t, codec, stream.in, &protocol.ControlMessage{
		Type: protocol.MsgRequestDeviceLogs, LogLimit: 3,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*holdReadSlice)
	defer cancel()

	require.NoError(t, proveUntilWoundDown(ctx, codec, stream, loadOptions{holdFor: time.Minute}))

	reply := readControlOfType(t, codec, stream.out, protocol.MsgDeviceLogsResponse)
	assert.Len(t, reply.LogEntries, 3)
}
