package agentapi

import (
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
	orgID    uuid.UUID
	deviceID uuid.UUID
	samples  []telemetry.Sample
}

type recordingTelemetryWriter struct {
	calls chan telemetryWriteCall
	block chan struct{}
	count atomic.Int64
}

func (r *recordingTelemetryWriter) WriteSamples(_ context.Context, orgID uuid.UUID, deviceID uuid.UUID, samples []telemetry.Sample) error {
	r.count.Add(1)
	if r.block != nil {
		<-r.block
	}
	r.calls <- telemetryWriteCall{orgID: orgID, deviceID: deviceID, samples: samples}
	return nil
}

type recordingProcessRepo struct {
	calls chan processWriteCall
}

type processWriteCall struct {
	deviceID uuid.UUID
	samples  []telemetry.ProcessSample
}

func (r *recordingProcessRepo) UpsertReport(_ context.Context, deviceID uuid.UUID, _ time.Time, samples []telemetry.ProcessSample) error {
	r.calls <- processWriteCall{deviceID: deviceID, samples: samples}
	return nil
}

func (r *recordingProcessRepo) ListLatest(context.Context, uuid.UUID, int) ([]telemetry.ProcessSample, error) {
	return nil, nil
}

func TestAgentConn_HandleAgentHealthSummaryUsesAuthoritativeTenant(t *testing.T) {
	deviceID := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, deviceID, nil)
	ac.telemetry = writer

	msg := &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		OrgID:           uuid.New().String(),
		NodeAnomalyRate: 0.25,
		PerFamilyRates:  []protocol.FamilyAnomalyRate{{Family: "cpu", Rate: 0.5}},
	}
	writeControlMsg(t, ac.codec, buf, msg)

	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	call := receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, dbtx.DefaultOrgID, call.orgID)
	assert.Equal(t, deviceID, call.deviceID)
	require.Len(t, call.samples, 2)
	assert.Equal(t, "opengate_edge_node_anomaly_rate", call.samples[0].Name)
	assert.Equal(t, map[string]string{"sampler_ver": "", "model_ver": ""}, call.samples[0].Labels)
	assert.Equal(t, "cpu", call.samples[1].Labels["family"])
}

func TestAgentConn_HandleProcessReportStoresRowsAndRankOnlyMetrics(t *testing.T) {
	deviceID := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	processes := &recordingProcessRepo{calls: make(chan processWriteCall, 1)}
	ac, buf := newTestAgentConn(t, deviceID, nil)
	ac.telemetry = writer
	ac.processes = processes

	hash := "deadbeef"
	msg := &protocol.ControlMessage{
		Type:  protocol.MsgProcessReport,
		TS:    time.Now().Unix(),
		OrgID: uuid.New().String(),
		TopN: []protocol.ProcessReportEntry{{
			Rank: 1, Basename: "postgres", CmdlineHash: &hash, PID: 222, CPU: 12.5, Mem: 3.25,
		}},
	}
	writeControlMsg(t, ac.codec, buf, msg)

	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	processCall := receiveProcessCall(t, processes.calls)
	assert.Equal(t, deviceID, processCall.deviceID)
	require.Len(t, processCall.samples, 1)
	assert.Equal(t, "postgres", processCall.samples[0].Basename)
	assert.Equal(t, &hash, processCall.samples[0].CmdlineHash)

	metricCall := receiveTelemetryCall(t, writer.calls)
	require.Len(t, metricCall.samples, 2)
	for _, sample := range metricCall.samples {
		assert.Equal(t, "1", sample.Labels["rank"])
		assert.NotContains(t, sample.Labels, "basename")
		assert.NotContains(t, sample.Labels, "pid")
	}
}

func TestAgentConn_TelemetryIntervalFloorDropsFastSamples(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 2)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	now := time.Now().Unix()

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   now,
		Dims: []protocol.MetricDim{{Name: "cpu", Avg: 1}},
	})
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   now + 1,
		Dims: []protocol.MetricDim{{Name: "cpu", Avg: 2}},
	})

	require.NoError(t, ac.handleControl(ctx))
	require.NoError(t, ac.handleControl(ctx))
	_ = receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, uint64(1), ac.DroppedTelemetryCount())
	assert.Equal(t, int64(1), writer.count.Load())
}

func TestAgentConn_TelemetryPayloadCapDropsOversizedMessage(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer
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
		Dims: []protocol.MetricDim{{Name: "cpu", Avg: 1}},
	})

	start := time.Now()
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))
	assert.Less(t, time.Since(start), 100*time.Millisecond)
	close(writer.block)
	_ = receiveTelemetryCall(t, writer.calls)
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

func receiveProcessCall(t *testing.T, calls <-chan processWriteCall) processWriteCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for process write")
		return processWriteCall{}
	}
}
