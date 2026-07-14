package agentapi

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

type inventoryReplaceCall struct {
	deviceID   uuid.UUID
	orgID      uuid.UUID
	components []inventory.Component
}

type recordingInventoryRepo struct {
	calls chan inventoryReplaceCall
}

func (r *recordingInventoryRepo) Replace(ctx context.Context, deviceID uuid.UUID, _ time.Time, components []inventory.Component) error {
	tenant, _ := dbtx.TenantFromContext(ctx)
	r.calls <- inventoryReplaceCall{deviceID: deviceID, orgID: tenant.OrgID, components: components}
	return nil
}

func (r *recordingInventoryRepo) ListForDevice(context.Context, uuid.UUID, int) ([]inventory.Component, error) {
	return nil, nil
}

// discoveryConn builds an AgentConn wired to an inventory repo over an in-memory
// buffer, scoped to org and advertising the Discovery capability.
func discoveryConn(t *testing.T, org uuid.UUID, inv inventory.Repository) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:     uuid.New(),
		OrgID:        org,
		stream:       &buf,
		codec:        &protocol.Codec{},
		inventory:    inv,
		Capabilities: []protocol.AgentCapability{protocol.CapDiscovery},
		logger:       testLogger(),
	}
	return ac, &buf
}

func receiveInventoryCall(t *testing.T, calls <-chan inventoryReplaceCall) inventoryReplaceCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for inventory replace")
		return inventoryReplaceCall{}
	}
}

// A full discovery report maps every category to an inventory component and is
// persisted under the connection's authoritative org, never the payload org.
func TestAgentConn_HandleDiscoveryReportUpsertsScopedInventory(t *testing.T) {
	org := uuid.New()
	inv := &recordingInventoryRepo{calls: make(chan inventoryReplaceCall, 1)}
	ac, _ := discoveryConn(t, org, inv)

	truncated := true
	msg := &protocol.ControlMessage{
		Type:       protocol.MsgDiscoveryReport,
		TS:         time.Now().Unix(),
		OrgID:      uuid.New().String(), // hostile: agent asserts a foreign org — must be ignored
		Ports:      []protocol.DiscoveredPort{{Proto: "tcp", Port: 5432, Process: "postgres"}},
		Services:   []protocol.DiscoveredService{{Name: "nginx.service", State: "running"}},
		DBEngines:  []protocol.DiscoveredDbEngine{{Engine: "postgres", Version: "16.2", Port: 5432}},
		Containers: []protocol.DiscoveredContainer{{Runtime: "docker", Image: "redis:7", Name: "cache", State: "running"}},
		Packages:   []protocol.DiscoveredPackage{{Name: "openssl", Version: "3.0.13"}},
		Truncated:  &truncated,
	}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), msg, 512))

	call := receiveInventoryCall(t, inv.calls)
	assert.Equal(t, ac.DeviceID, call.deviceID)
	assert.Equal(t, org, call.orgID, "inventory must be scoped to the connection org, not the payload org")
	require.Len(t, call.components, 5)

	byKind := make(map[string]inventory.Component, len(call.components))
	for _, c := range call.components {
		byKind[c.Kind] = c
	}
	assert.Equal(t, inventory.Component{Kind: inventory.KindPort, Name: "postgres", Proto: "tcp", Port: 5432}, byKind[inventory.KindPort])
	assert.Equal(t, inventory.Component{Kind: inventory.KindService, Name: "nginx.service", State: "running"}, byKind[inventory.KindService])
	assert.Equal(t, inventory.Component{Kind: inventory.KindDBEngine, Name: "postgres", Version: "16.2", Port: 5432}, byKind[inventory.KindDBEngine])
	assert.Equal(t, inventory.Component{Kind: inventory.KindContainer, Name: "cache", Runtime: "docker", Image: "redis:7", State: "running"}, byKind[inventory.KindContainer])
	assert.Equal(t, inventory.Component{Kind: inventory.KindPackage, Name: "openssl", Version: "3.0.13"}, byKind[inventory.KindPackage])
}

// The dispatch switch routes a DiscoveryReport frame to the handler.
func TestAgentConn_HandleControlDispatchesDiscovery(t *testing.T) {
	org := uuid.New()
	inv := &recordingInventoryRepo{calls: make(chan inventoryReplaceCall, 1)}
	ac, buf := discoveryConn(t, org, inv)

	msg := &protocol.ControlMessage{
		Type:     protocol.MsgDiscoveryReport,
		TS:       time.Now().Unix(),
		Packages: []protocol.DiscoveredPackage{{Name: "curl", Version: "8.5.0"}},
	}
	writeControlMsg(t, ac.codec, buf, msg)

	require.NoError(t, ac.handleControl(tenantCtx(org)))
	call := receiveInventoryCall(t, inv.calls)
	require.Len(t, call.components, 1)
	assert.Equal(t, "curl", call.components[0].Name)
}

// A nil inventory repo (the default programmatic AgentConn) is a safe no-op.
func TestAgentConn_HandleDiscoveryReportNilRepoIsNoop(t *testing.T) {
	org := uuid.New()
	ac, _ := discoveryConn(t, org, nil)
	ac.inventory = nil

	msg := &protocol.ControlMessage{
		Type:     protocol.MsgDiscoveryReport,
		TS:       time.Now().Unix(),
		Packages: []protocol.DiscoveredPackage{{Name: "openssl", Version: "3.0.13"}},
	}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), msg, 128))
}

// An oversized discovery payload is dropped before it reaches the repo and is
// counted against the drop metric.
func TestAgentConn_HandleDiscoveryReportPayloadCapDrops(t *testing.T) {
	org := uuid.New()
	inv := &recordingInventoryRepo{calls: make(chan inventoryReplaceCall, 1)}
	ac, _ := discoveryConn(t, org, inv)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	msg := &protocol.ControlMessage{
		Type:     protocol.MsgDiscoveryReport,
		TS:       time.Now().Unix(),
		Packages: []protocol.DiscoveredPackage{{Name: "openssl", Version: "3.0.13"}},
	}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), msg, maxDiscoveryPayloadBytes+1))

	assert.Empty(t, inv.calls)
	assert.InDelta(t, 1,
		promtestutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("discovery_payload_too_large")), 0)
}

// An empty discovery report is accepted but never issues a persist (the repo's
// own empty-report guard would no-op anyway, but skipping avoids a wasted slot).
func TestAgentConn_HandleDiscoveryReportEmptyDoesNotPersist(t *testing.T) {
	org := uuid.New()
	inv := &recordingInventoryRepo{calls: make(chan inventoryReplaceCall, 1)}
	ac, _ := discoveryConn(t, org, inv)

	msg := &protocol.ControlMessage{Type: protocol.MsgDiscoveryReport, TS: time.Now().Unix()}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), msg, 32))
	assert.Empty(t, inv.calls)
}

// A second report inside the interval floor is dropped; one past it is accepted
// again, pinning the interval-floor boundary.
func TestAgentConn_HandleDiscoveryReportIntervalFloorDrops(t *testing.T) {
	org := uuid.New()
	inv := &recordingInventoryRepo{calls: make(chan inventoryReplaceCall, 2)}
	ac, _ := discoveryConn(t, org, inv)
	base := time.Now().Unix()

	first := &protocol.ControlMessage{Type: protocol.MsgDiscoveryReport, TS: base, Packages: []protocol.DiscoveredPackage{{Name: "a"}}}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), first, 128))
	receiveInventoryCall(t, inv.calls)

	tooSoon := &protocol.ControlMessage{Type: protocol.MsgDiscoveryReport, TS: base + minDiscoveryIntervalSeconds - 1, Packages: []protocol.DiscoveredPackage{{Name: "b"}}}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), tooSoon, 128))
	assert.Empty(t, inv.calls, "a report inside the interval floor is dropped")

	later := &protocol.ControlMessage{Type: protocol.MsgDiscoveryReport, TS: base + minDiscoveryIntervalSeconds, Packages: []protocol.DiscoveredPackage{{Name: "c"}}}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(org), later, 128))
	call := receiveInventoryCall(t, inv.calls)
	require.Len(t, call.components, 1)
	assert.Equal(t, "c", call.components[0].Name)
}

func TestAgentConn_AcceptDiscoveryPinsPayloadAndIntervalBoundaries(t *testing.T) {
	ac := &AgentConn{}

	assert.Equal(t, 1<<20, maxDiscoveryPayloadBytes)
	assert.True(t, ac.acceptDiscovery(0, maxDiscoveryPayloadBytes))
	assert.Nil(t, ac.telemetryLast, "a missing timestamp must not start interval tracking")

	assert.True(t, ac.acceptDiscovery(100, maxDiscoveryPayloadBytes))
	assert.False(t, ac.acceptDiscovery(129, maxDiscoveryPayloadBytes))
	assert.True(t, ac.acceptDiscovery(130, maxDiscoveryPayloadBytes))
	assert.Equal(t, int64(130), ac.telemetryLast[protocol.MsgDiscoveryReport])
	assert.False(t, ac.acceptDiscovery(160, maxDiscoveryPayloadBytes+1))
	assert.Equal(t, uint64(2), ac.DroppedTelemetryCount())
}
