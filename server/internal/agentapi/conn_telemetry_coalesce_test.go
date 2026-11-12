package agentapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestEachLiveMessageReachesTheWriterAsItArrivesWithoutAHeartbeat(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 4)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	now := time.Now().Unix()

	for i := range 2 {
		writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
			Type: protocol.MsgAgentMetricWindow,
			TS:   now + int64(i)*minTelemetryIntervalSeconds,
			Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: float64(i)}},
		})
	}
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary, TS: now, NodeAnomalyRate: 0.42, SamplerVersion: "s1",
	})
	for range 3 {
		require.NoError(t, ac.handleControl(ctx))
	}

	assert.Equal(t, int64(3), writer.count.Load(), "every message is handed over on arrival")
	for range 3 {
		call := receiveTelemetryCall(t, writer.calls)
		assert.Equal(t, ac.DeviceID, call.deviceID)
	}
	assert.Zero(t, ac.DroppedTelemetryCount())
}

func TestAHeartbeatHandsNothingToTheWriter(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentHeartbeat, Timestamp: time.Now().Unix(),
	})

	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))
	assert.Zero(t, writer.count.Load())
}

// windowConn is a connection with the given capabilities whose writes the test reads back.
func windowConn(t *testing.T, writer *recordingTelemetryWriter, caps ...protocol.AgentCapability) (*AgentConn, *sentStream) {
	t.Helper()
	stream := &sentStream{}
	ac := &AgentConn{DeviceID: uuid.New(), stream: stream, codec: &protocol.Codec{}, telemetry: writer, logger: testLogger()}
	ac.setMeta("linux", "amd64", "0.1.0", caps)
	return ac, stream
}

func liveWindow(ts int64) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow, TS: ts,
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 31}},
	}
}

func TestAStoredLiveWindowIsConfirmedWithItsOwnStamp(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, stream := windowConn(t, writer, protocol.CapLiveConfirmations)
	ts := time.Now().Unix() - 30

	require.NoError(t, ac.handleAgentMetricWindow(dbtx.WithDefaultTenant(context.Background(), false), liveWindow(ts), 256))

	msgs, err := stream.controls()
	require.NoError(t, err)
	require.Len(t, msgs, 1, "one confirmation per stored window")
	assert.Equal(t, protocol.MsgLiveWindowStored, msgs[0].Type)
	assert.Equal(t, ts, msgs[0].TS, "the confirmation names the window as the machine stamped it")
}

func TestAWindowTheStoreDidNotTakeIsNotConfirmed(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1), fail: errors.New("store down")}
	ac, stream := windowConn(t, writer, protocol.CapLiveConfirmations)

	require.NoError(t, ac.handleAgentMetricWindow(dbtx.WithDefaultTenant(context.Background(), false),
		liveWindow(time.Now().Unix()), 256))

	assert.Zero(t, stream.sent(), "an unconfirmed minute is sent again by catch-up")
	assert.Equal(t, uint64(1), ac.DroppedTelemetryCount())
}

func TestAMachineThatDoesNotReadConfirmationsIsSentNone(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, stream := windowConn(t, writer)

	require.NoError(t, ac.handleAgentMetricWindow(dbtx.WithDefaultTenant(context.Background(), false),
		liveWindow(time.Now().Unix()), 256))

	receiveTelemetryCall(t, writer.calls)
	assert.Zero(t, stream.sent(), "an older agent is served exactly as before")
}
