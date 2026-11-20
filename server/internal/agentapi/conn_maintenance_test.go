package agentapi

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestAgentConn_SendSetMaintenanceMode(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		ac := &AgentConn{codec: &protocol.Codec{}, logger: testLogger()}
		var buf bytes.Buffer
		ac.stream = &buf

		require.NoError(t, ac.SendSetMaintenanceMode(context.Background(), enabled))
		msg := readReply(t, ac, &buf)
		assert.Equal(t, protocol.MsgSetMaintenanceMode, msg.Type)
		// The pointer survives for false, so a dropped field cannot read as no change.
		require.NotNil(t, msg.Enabled)
		assert.Equal(t, enabled, *msg.Enabled)
	}
}

func TestAgentConn_PushMaintenanceState_Reconciles(t *testing.T) {
	t.Run("in maintenance pushes true", func(t *testing.T) {
		ac := &AgentConn{DeviceID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger()}
		var buf bytes.Buffer
		ac.stream = &buf

		ac.pushMaintenanceState(context.Background(), true)
		msg := readReply(t, ac, &buf)
		assert.Equal(t, protocol.MsgSetMaintenanceMode, msg.Type)
		require.NotNil(t, msg.Enabled)
		assert.True(t, *msg.Enabled)
	})

	t.Run("active pushes nothing", func(t *testing.T) {
		ac := &AgentConn{DeviceID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger()}
		var buf bytes.Buffer
		ac.stream = &buf

		ac.pushMaintenanceState(context.Background(), false)
		assert.Zero(t, buf.Len(), "an Active device needs no reconcile push — the agent defaults to Active")
	})
}
