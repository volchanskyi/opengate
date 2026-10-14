package agentapi

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

type telemetryWriteCall struct {
	tenantID uuid.UUID
	deviceID uuid.UUID
	samples  []telemetry.Sample
}

type recordingTelemetryWriter struct {
	calls chan telemetryWriteCall
	block chan struct{}
	count atomic.Int64
}

func (r *recordingTelemetryWriter) WriteSamples(_ context.Context, tenantID uuid.UUID, deviceID uuid.UUID, samples []telemetry.Sample) error {
	r.count.Add(1)
	if r.block != nil {
		<-r.block
	}
	r.calls <- telemetryWriteCall{tenantID: tenantID, deviceID: deviceID, samples: samples}
	return nil
}

func TestAgentConn_HandleAgentHealthSummaryUsesAuthoritativeTenant(t *testing.T) {
	deviceID := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, deviceID, nil)
	ac.telemetry = writer

	msg := &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		TenantID:        uuid.New().String(),
		NodeAnomalyRate: 0.25,
		PerFamilyRates:  []protocol.FamilyAnomalyRate{{Family: "cpu", Rate: 0.5}},
	}
	writeControlMsg(t, ac.codec, buf, msg)

	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	require.NoError(t, ac.handleControl(ctx))
	ac.flushTelemetry(ctx)

	call := receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, dbtx.DefaultTenantID, call.tenantID)
	assert.Equal(t, deviceID, call.deviceID)
	require.Len(t, call.samples, 2)
	assert.Equal(t, "opengate_edge_node_anomaly_rate", call.samples[0].Name)
	assert.Equal(t, map[string]string{"sampler_ver": "", "model_ver": ""}, call.samples[0].Labels)
	assert.Equal(t, "cpu", call.samples[1].Labels["family"])
}

func TestAgentConn_TelemetryIntervalFloorDropsFastSamples(t *testing.T) {
	ac, buf, writer := telemetryConn(t, 2)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	now := time.Now().Unix()

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   now,
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 1}},
	})
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   now + 1,
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 2}},
	})

	require.NoError(t, ac.handleControl(ctx))
	require.NoError(t, ac.handleControl(ctx))
	ac.flushTelemetry(ctx)
	_ = receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, uint64(1), ac.DroppedTelemetryCount())
	assert.Equal(t, int64(1), writer.count.Load())
}

func TestAgentConn_TelemetryPayloadCapDropsOversizedMessage(t *testing.T) {
	ac, buf, writer := telemetryConn(t, 1)
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{{Name: strings.Repeat("x", maxTelemetryPayloadBytes), Avg: 1}},
	})

	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	assert.Equal(t, uint64(1), ac.DroppedTelemetryCount())
	assert.Equal(t, int64(0), writer.count.Load())
}

func TestAgentConn_TelemetryWriterDoesNotBlockControlLoop(t *testing.T) {
	writer := &recordingTelemetryWriter{
		calls: make(chan telemetryWriteCall, 1),
		block: make(chan struct{}),
	}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 1}},
	})

	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	require.NoError(t, ac.handleControl(ctx))
	start := time.Now()
	ac.flushTelemetry(ctx)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
	close(writer.block)
	_ = receiveTelemetryCall(t, writer.calls)
}

func TestAgentConn_HandleHealthWindowResponseFansOutSummaries(t *testing.T) {
	ac, buf, writer := telemetryConn(t, 1)

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgHealthWindowResponse,
		Summaries: []protocol.HealthSummary{{
			TS:              time.Now().Unix(),
			NodeAnomalyRate: 0.4,
			SamplerVersion:  "s1",
			ModelVersion:    "m1",
			PerFamilyRates:  []protocol.FamilyAnomalyRate{{Family: "net", Rate: 0.2}},
		}},
	})

	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	require.NoError(t, ac.handleControl(ctx))
	ac.flushTelemetry(ctx)

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 2)
	assert.Equal(t, "health_window", call.samples[0].Labels["source"])
	assert.Equal(t, "net", call.samples[1].Labels["family"])
	assert.Equal(t, "health_window", call.samples[1].Labels["source"])
}

func TestAgentConn_PersistTelemetryDropsWhenTenantMissing(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)

	called := false
	ac.persistTelemetry(context.Background(), 3, func(context.Context, dbtx.Tenant) error {
		called = true
		return nil
	})

	assert.Equal(t, uint64(3), ac.DroppedTelemetryCount())
	assert.False(t, called)
}

func TestAcceptTelemetryPinsPayloadAndTimestampBoundaries(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.logger = nil

	assert.Equal(t, 64*1024, maxTelemetryPayloadBytes)
	assert.Equal(t, 2*time.Second, telemetryPersistTimeout)
	assert.True(t, ac.acceptTelemetry(protocol.MsgAgentMetricWindow, 0, maxTelemetryPayloadBytes))
	assert.Nil(t, ac.telemetryLast, "a missing timestamp must not start interval tracking")

	assert.True(t, ac.acceptTelemetry(protocol.MsgAgentMetricWindow, 100, maxTelemetryPayloadBytes))
	assert.False(t, ac.acceptTelemetry(protocol.MsgAgentMetricWindow, 109, maxTelemetryPayloadBytes))
	assert.True(t, ac.acceptTelemetry(protocol.MsgAgentMetricWindow, 110, maxTelemetryPayloadBytes))
	assert.Equal(t, int64(110), ac.telemetryLast[protocol.MsgAgentMetricWindow])
	assert.False(t, ac.acceptTelemetry(protocol.MsgAgentMetricWindow, 120, maxTelemetryPayloadBytes+1))
	assert.Equal(t, uint64(2), ac.DroppedTelemetryCount())
	assert.NotPanics(t, func() { ac.dropTelemetry("nil_logger") })
}

func TestTelemetryTimestamp(t *testing.T) {
	t.Parallel()
	ac, _ := newTestAgentConn(t, uuid.New(), nil)

	inWindow := time.Now().Add(-time.Hour).Unix()
	assert.Equal(t, time.Unix(inWindow, 0).UTC(), ac.telemetryTimestamp(inWindow))

	got := ac.telemetryTimestamp(0)
	assert.WithinDuration(t, time.Now().UTC(), got, 5*time.Second)
	assert.Equal(t, time.UTC, got.Location())

	assert.WithinDuration(t, time.Now().UTC().Add(-maxTelemetryBacklog),
		ac.telemetryTimestamp(time.Now().Add(-30*24*time.Hour).Unix()), 5*time.Second)
}

func telemetryConn(t *testing.T, callCap int) (*AgentConn, *bytes.Buffer, *recordingTelemetryWriter) {
	t.Helper()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, callCap)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	return ac, buf, writer
}

func receiveTelemetryCall(t *testing.T, calls <-chan telemetryWriteCall) telemetryWriteCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for telemetry write")
		return telemetryWriteCall{}
	}
}
