package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

type stubStatusDevices struct {
	device.Repository
}

func (stubStatusDevices) SetStatus(context.Context, device.DeviceID, device.DeviceStatus) error {
	return nil
}

func TestAgentConn_CoalescesHeartbeatBurstIntoOneWrite(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 4)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	now := time.Now().Unix()

	const windows = 6
	for i := 0; i < windows; i++ {
		writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
			Type: protocol.MsgAgentMetricWindow,
			// Spread by the interval floor so every window is accepted.
			TS:   now + int64(i)*minTelemetryIntervalSeconds,
			Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: float64(i)}},
		})
	}
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              now + windows*minTelemetryIntervalSeconds,
		NodeAnomalyRate: 0.42,
		SamplerVersion:  "s1",
	})
	for i := 0; i < windows+1; i++ {
		require.NoError(t, ac.handleControl(ctx))
	}
	require.Empty(t, writer.calls)

	ac.flushTelemetry(ctx)
	call := receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, int64(1), writer.count.Load(), "the whole burst is exactly one write")

	var gotAnomaly bool
	for _, s := range call.samples {
		if s.Name == "opengate_edge_node_anomaly_rate" {
			gotAnomaly = true
			assert.InDelta(t, 0.42, s.Value, 0.0001)
		}
	}
	assert.True(t, gotAnomaly, "the tail-ordered health summary is persisted, not dropped")
	assert.Zero(t, ac.DroppedTelemetryCount(), "coalescing drops nothing")
}

func TestAgentConn_HeartbeatFlushesBufferedTelemetry(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	ac.devices = stubStatusDevices{}
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		NodeAnomalyRate: 0.5,
		SamplerVersion:  "s1",
	})
	require.NoError(t, ac.handleControl(ctx))
	require.Empty(t, writer.calls, "summary is buffered, not written")

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	})
	require.NoError(t, ac.handleControl(ctx))

	call := receiveTelemetryCall(t, writer.calls)
	require.NotEmpty(t, call.samples)
}

func TestAgentConn_TeardownFlushPersistsBufferedTelemetry(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		NodeAnomalyRate: 0.7,
		SamplerVersion:  "s1",
	})
	require.NoError(t, ac.handleControl(ctx))
	require.Empty(t, writer.calls)

	// runControlLoop defers this exact call on teardown.
	ac.flushTelemetry(context.WithoutCancel(ctx))
	require.NotEmpty(t, receiveTelemetryCall(t, writer.calls).samples)
}
