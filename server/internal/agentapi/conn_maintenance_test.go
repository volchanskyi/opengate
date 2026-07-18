package agentapi

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// stubMaintDevices is a device.Repository whose Get returns a fixed device (or
// error). Only Get is exercised by pushMaintenanceState; the embedded interface
// satisfies the rest.
type stubMaintDevices struct {
	device.Repository
	dev *device.Device
	err error
}

func (s *stubMaintDevices) Get(context.Context, device.DeviceID) (*device.Device, error) {
	return s.dev, s.err
}

func TestAgentConn_SendSetMaintenanceMode(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		ac := &AgentConn{codec: &protocol.Codec{}, logger: testLogger()}
		var buf bytes.Buffer
		ac.stream = &buf

		require.NoError(t, ac.SendSetMaintenanceMode(context.Background(), enabled))
		msg := readReply(t, ac, &buf)
		assert.Equal(t, protocol.MsgSetMaintenanceMode, msg.Type)
		// The pointer must survive even for false, so the agent can be told to
		// resume — a dropped field would read as "no change".
		require.NotNil(t, msg.Enabled)
		assert.Equal(t, enabled, *msg.Enabled)
	}
}

func TestAgentConn_PushMaintenanceState_Reconciles(t *testing.T) {
	t.Run("in maintenance pushes true", func(t *testing.T) {
		ac := &AgentConn{DeviceID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger(),
			devices: &stubMaintDevices{dev: &device.Device{MaintenanceOn: true}}}
		var buf bytes.Buffer
		ac.stream = &buf

		ac.pushMaintenanceState(context.Background())
		msg := readReply(t, ac, &buf)
		assert.Equal(t, protocol.MsgSetMaintenanceMode, msg.Type)
		require.NotNil(t, msg.Enabled)
		assert.True(t, *msg.Enabled)
	})

	t.Run("active pushes nothing", func(t *testing.T) {
		ac := &AgentConn{DeviceID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger(),
			devices: &stubMaintDevices{dev: &device.Device{MaintenanceOn: false}}}
		var buf bytes.Buffer
		ac.stream = &buf

		ac.pushMaintenanceState(context.Background())
		assert.Zero(t, buf.Len(), "an Active device needs no reconcile push — the agent defaults to Active")
	})

	t.Run("read error writes nothing", func(t *testing.T) {
		ac := &AgentConn{DeviceID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger(),
			devices: &stubMaintDevices{err: errors.New("boom")}}
		var buf bytes.Buffer
		ac.stream = &buf

		ac.pushMaintenanceState(context.Background())
		assert.Zero(t, buf.Len(), "a read failure must not push a stale reconcile")
	})
}

func TestAgentConn_HandleMaintenanceApplied(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		ac, buf := newTestAgentConn(t, uuid.New(), nil)
		writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
			Type:    protocol.MsgMaintenanceApplied,
			Enabled: &enabled,
		})
		require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))
		assert.Equal(t, enabled, ac.maintenanceApplied.Load())
	}
}
