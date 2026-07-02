package agentapi

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"testing"
)

func readOutboundControl(t *testing.T, ac *AgentConn, buf *bytes.Buffer) *protocol.ControlMessage {
	t.Helper()
	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	return decoded
}

func TestAgentConn_SendRequestHardwareReport(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapHardwareInventory}

	err := ac.SendRequestHardwareReport(context.Background())
	require.NoError(t, err)

	decoded := readOutboundControl(t, ac, buf)
	assert.Equal(t, protocol.MsgRequestHardwareReport, decoded.Type)
}

func TestAgentConn_SendRequestHardwareReportRequiresCapability(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendRequestHardwareReport(context.Background())
	assert.ErrorIs(t, err, ErrCapabilityNotAdvertised)
	assert.Zero(t, buf.Len(), "old agents must not receive unsupported server-to-agent variants")
}

func TestAgentConn_SendRequestHealthWindow(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapHealthWindow}

	err := ac.SendRequestHealthWindow(context.Background(), 1700000000, 12)
	require.NoError(t, err)

	decoded := readOutboundControl(t, ac, buf)
	assert.Equal(t, protocol.MsgRequestHealthWindow, decoded.Type)
	assert.Equal(t, int64(1700000000), decoded.SinceTS)
	assert.Equal(t, uint32(12), decoded.Limit)
}

func TestAgentConn_SendRequestHealthWindowRequiresCapability(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendRequestHealthWindow(context.Background(), 1700000000, 12)
	assert.ErrorIs(t, err, ErrCapabilityNotAdvertised)
	assert.Zero(t, buf.Len(), "old agents must not receive unsupported server-to-agent variants")
}
