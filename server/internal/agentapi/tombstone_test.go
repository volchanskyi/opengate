package agentapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestIsWritePathMessage(t *testing.T) {
	t.Parallel()
	write := []protocol.ControlMessageType{
		protocol.MsgAgentRegister, protocol.MsgAgentHeartbeat, protocol.MsgAgentHealthSummary,
		protocol.MsgAgentMetricWindow, protocol.MsgProcessReport, protocol.MsgDiscoveryReport,
		protocol.MsgHealthWindowResponse, protocol.MsgRequestBackfillSlot, protocol.MsgMetricBackfillBatch,
	}
	for _, m := range write {
		assert.Truef(t, isWritePathMessage(m), "%s must be a write path", m)
	}
	read := []protocol.ControlMessageType{
		protocol.MsgSessionAccept, protocol.MsgDeviceLogsResponse, protocol.MsgLocalHistoryResponse,
		protocol.MsgAgentUpdateAck,
	}
	for _, m := range read {
		assert.Falsef(t, isWritePathMessage(m), "%s must not be a write path", m)
	}
}

func TestRejectTombstonedWrite(t *testing.T) {
	t.Parallel()
	tombstoned := &AgentConn{DeviceID: uuid.New(), logger: testLogger(), isTombstoned: func() bool { return true }}
	live := &AgentConn{DeviceID: uuid.New(), logger: testLogger(), isTombstoned: func() bool { return false }}
	nilChecker := &AgentConn{DeviceID: uuid.New(), logger: testLogger()}

	writeMsg := &protocol.ControlMessage{Type: protocol.MsgProcessReport}
	readMsg := &protocol.ControlMessage{Type: protocol.MsgDeviceLogsResponse}

	assert.True(t, tombstoned.rejectTombstonedWrite(writeMsg), "tombstoned write is rejected")
	assert.Equal(t, uint64(1), tombstoned.DroppedTelemetryCount(), "rejected write is counted as a drop")
	assert.False(t, tombstoned.rejectTombstonedWrite(readMsg), "read-path message is never rejected")
	assert.False(t, live.rejectTombstonedWrite(writeMsg), "live device write is accepted")
	assert.False(t, nilChecker.rejectTombstonedWrite(writeMsg), "nil checker treats device as live")
}

// fakeTombstoneLoader returns a fixed deny-list.
type fakeTombstoneLoader struct {
	tombstones []lifecycle.Tombstone
}

func (f *fakeTombstoneLoader) ListAll(context.Context) ([]lifecycle.Tombstone, error) {
	return f.tombstones, nil
}

func TestWarmTombstonesLoadsDeviceDenyList(t *testing.T) {
	t.Parallel()
	device := uuid.New()
	org := uuid.New()
	loader := &fakeTombstoneLoader{tombstones: []lifecycle.Tombstone{
		{OrgID: org, DeviceID: &device, Scope: lifecycle.ScopeDevice},
		{OrgID: org, Scope: lifecycle.ScopeOrg}, // org-scoped: carries no device id
	}}
	s := NewAgentServer(AgentServerConfig{Logger: testLogger(), Tombstones: loader})

	require.NoError(t, s.WarmTombstones(context.Background()))

	_, denied := s.tombstones.Load(device)
	assert.True(t, denied, "a persisted device tombstone must warm the in-memory cache")
}

func TestWarmTombstonesNoStoreIsNoop(t *testing.T) {
	t.Parallel()
	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	require.NoError(t, s.WarmTombstones(context.Background()))
}

func TestDeregisterAgentTombstonesDevice(t *testing.T) {
	t.Parallel()
	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	device := uuid.New()
	s.DeregisterAgent(context.Background(), device)
	_, denied := s.tombstones.Load(device)
	assert.True(t, denied, "deregistering a device adds it to the in-memory deny-list")
}
