package agentapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"testing"
)

func TestAgentConn_SendRequestHardwareReport(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapHardwareInventory}

	err := ac.SendRequestHardwareReport(context.Background())
	require.NoError(t, err)

	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgRequestHardwareReport, decoded.Type)
}

func TestAgentConn_SendRequestHardwareReportRequiresCapability(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendRequestHardwareReport(context.Background())
	assert.ErrorIs(t, err, ErrCapabilityNotAdvertised)
	assert.Zero(t, buf.Len(), "old agents must not receive unsupported server-to-agent variants")
}
