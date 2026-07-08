package agentapi

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"testing"
)

func TestAgentConn_SendRequestDeviceLogs(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapDeviceLogs}

	filter := device.LogFilter{
		Level:  "ERROR",
		From:   "2026-01-01T00:00:00Z",
		To:     "2026-01-02T00:00:00Z",
		Search: "panic",
		Offset: 10,
		Limit:  50,
	}

	err := ac.SendRequestDeviceLogs(context.Background(), filter)
	require.NoError(t, err)

	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgRequestDeviceLogs, decoded.Type)
	assert.Equal(t, "ERROR", decoded.LogLevel)
	assert.Equal(t, "2026-01-01T00:00:00Z", decoded.TimeFrom)
	assert.Equal(t, "2026-01-02T00:00:00Z", decoded.TimeTo)
	assert.Equal(t, "panic", decoded.Search)
	assert.Equal(t, uint32(10), decoded.LogOffset)
	assert.Equal(t, uint32(50), decoded.LogLimit)
}

func TestAgentConn_SendRequestDeviceLogsRequiresCapability(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendRequestDeviceLogs(context.Background(), device.LogFilter{Limit: 10})
	assert.ErrorIs(t, err, ErrCapabilityNotAdvertised)
	assert.Zero(t, buf.Len(), "old agents must not receive unsupported server-to-agent variants")
}

// TestAgentConn_HandleDeviceLogsResponse_NoWaiterDrops proves the read loop
// never blocks on an unsolicited raw-log response: with no broker pull in
// flight the response is dropped, not persisted anywhere.
func TestAgentConn_HandleDeviceLogsResponse_NoWaiterDrops(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	codec := &protocol.Codec{}

	var frameBuf bytes.Buffer
	writeControlMsg(t, codec, &frameBuf, &protocol.ControlMessage{
		Type: protocol.MsgDeviceLogsResponse,
		LogEntries: []protocol.LogEntry{
			{Timestamp: "2026-01-01T00:00:01Z", Level: "INFO", Target: "agent", Message: "started"},
		},
		TotalCount: 1,
	})
	ac.stream = &frameBuf

	require.NoError(t, ac.handleControl(context.Background()))
}
