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

// stubStatusDevices satisfies the heartbeat handler's only repository call
// (SetStatus) so the coalescing flush trigger can be driven without a database.
type stubStatusDevices struct {
	device.Repository
}

func (stubStatusDevices) SetStatus(context.Context, device.DeviceID, device.DeviceStatus) error {
	return nil
}

// A heartbeat-shaped burst — the host-metric window firehose followed by the
// tail-ordered anomaly summary — coalesces into a single WriteSamples, and the
// tail summary is persisted rather than lost to the persist-slot race.
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
	// Nothing is written until the burst is flushed — the firehose can no longer
	// saturate the persist slots because it is buffered, not written per-message.
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

// The heartbeat that opens each cycle flushes the previous cycle's buffered
// telemetry (the agent sends the heartbeat first, then drains its burst).
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

// A burst with no following heartbeat (a disconnect mid-cycle) must still be
// persisted by the teardown flush — buffered samples are never silently lost.
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
