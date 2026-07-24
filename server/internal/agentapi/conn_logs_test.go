package agentapi

import (
	"bytes"
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

// TestRequestLogsSync_DeliversResponse pins the transient broker: a synchronous
// request blocks until the agent's DeviceLogsResponse is delivered to the
// in-flight waiter, and the bounded lines flow straight through — nothing is
// persisted centrally (the AgentConn holds no log repository).
func TestRequestLogsSync_DeliversResponse(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapDeviceLogs}

	type result struct {
		entries []device.LogEntry
		total   int
		err     error
	}
	resCh := make(chan result, 1)
	go func() {
		entries, total, err := ac.RequestLogsSync(context.Background(), device.LogFilter{Limit: 50})
		resCh <- result{entries, total, err}
	}()

	// The read-loop side delivers once the waiter is registered.
	require.Eventually(t, func() bool {
		return ac.deliverLogs(logsResult{
			entries: []device.LogEntry{
				{Timestamp: "2026-01-01T00:00:01Z", Level: "INFO", Target: "agent", Message: "started"},
				{Timestamp: "2026-01-01T00:00:02Z", Level: "ERROR", Target: "net", Message: "connection lost"},
			},
			total: 2,
		})
	}, time.Second, 2*time.Millisecond)

	got := <-resCh
	require.NoError(t, got.err)
	assert.Equal(t, 2, got.total)
	require.Len(t, got.entries, 2)
	assert.Equal(t, "connection lost", got.entries[1].Message)
}

// TestRequestLogsSync_RequiresCapability keeps old agents safe: without the
// DeviceLogs capability the broker refuses before writing anything.
func TestRequestLogsSync_RequiresCapability(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	_, _, err := ac.RequestLogsSync(context.Background(), device.LogFilter{Limit: 10})
	assert.ErrorIs(t, err, ErrCapabilityNotAdvertised)
	assert.Zero(t, buf.Len())
}

// TestRequestLogsSync_SingleFlight rejects a second concurrent pull for the
// same connection: one raw request is in flight at a time (no wire correlation).
func TestRequestLogsSync_SingleFlight(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapDeviceLogs}
	ac.logWaiter = make(chan logsResult, 1) // simulate an in-flight request

	_, _, err := ac.RequestLogsSync(context.Background(), device.LogFilter{Limit: 10})
	assert.ErrorIs(t, err, ErrLogsBusy)
}

// TestRequestLogsSync_Timeout returns the context error when the agent never
// responds, bounding how long a raw pull can block.
func TestRequestLogsSync_Timeout(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapDeviceLogs}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := ac.RequestLogsSync(ctx, device.LogFilter{Limit: 10})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	// Waiter is cleared so a subsequent pull is not reported busy.
	assert.Nil(t, ac.logWaiter)
}

// TestHandleDeviceLogsError_DeliversError routes an agent-side error to the
// waiting broker instead of silently dropping the request.
func TestHandleDeviceLogsError_DeliversError(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapDeviceLogs}

	// Simulate an in-flight pull, then drive the read loop over a frame that
	// carries a DeviceLogsError. Single-goroutine so there is no data race on
	// the shared stream field.
	ch := make(chan logsResult, 1)
	ac.logWaiter = ch

	codec := &protocol.Codec{}
	var frame bytes.Buffer
	writeControlMsg(t, codec, &frame, &protocol.ControlMessage{
		Type:     protocol.MsgDeviceLogsError,
		AckError: "reader disabled",
	})
	ac.stream = &frame

	require.NoError(t, ac.handleControl(context.Background()))

	select {
	case res := <-ch:
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "reader disabled")
	default:
		t.Fatal("expected the agent-side error to reach the waiter")
	}
}

// TestDeliverLogs_NoWaiterDrops keeps a late or unsolicited response from
// blocking the read loop when no pull is in flight.
func TestDeliverLogs_NoWaiterDrops(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	assert.False(t, ac.deliverLogs(logsResult{total: 1}))
}

// TestHostMetricDimsIngestScopedByConnectionOrg pins that live host-metric
// windows ride the AgentMetricWindow path and land in the telemetry writer as
// `opengate_edge_metric_avg{dim=...}` scoped to the connection's authoritative
// org — never the agent-supplied one. Cross-tenant reads are then denied by the
// VM scoped reader (see telemetry.ScopeSelector tests).
func TestHostMetricDimsIngestScopedByConnectionOrg(t *testing.T) {
	deviceID := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, deviceID, nil)
	ac.telemetry = writer

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:  protocol.MsgAgentMetricWindow,
		TS:    time.Now().Unix(),
		OrgID: uuid.New().String(), // agent-supplied org must be ignored
		Dims: []protocol.MetricDim{
			{Name: "cpu.total", Avg: 42.5},
			{Name: "net.rx_bytes", Avg: 123456},
		},
	})

	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	call := receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, dbtx.DefaultOrgID, call.orgID, "org must be the connection's, not agent-supplied")
	assert.Equal(t, deviceID, call.deviceID)
	require.Len(t, call.samples, 2)
	for _, s := range call.samples {
		assert.Equal(t, "opengate_edge_metric_avg", s.Name)
	}
	assert.Equal(t, "cpu.total", call.samples[0].Labels["dim"])
	assert.Equal(t, "net.rx_bytes", call.samples[1].Labels["dim"])
}
