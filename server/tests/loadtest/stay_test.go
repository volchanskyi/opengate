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

func TestASeveranceAfterTheHoldIsStillReported(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}, writeErr: errors.New("connection reset")}

	err := proveUntilWoundDown(context.Background(), &protocol.Codec{}, stream,
		loadOptions{holdFor: time.Minute})

	require.Error(t, err, "a machine whose connection is gone must not report success because its hold had ended")
	assert.ErrorIs(t, err, ErrHeldPeerGone)
}

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
