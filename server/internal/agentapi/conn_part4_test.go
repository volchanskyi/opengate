package agentapi

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
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

func TestAgentConn_HandleDeviceLogsResponse(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	d := testutil.SeedDevice(t, ctx, store, group.ID)

	codec := &protocol.Codec{}

	msg := &protocol.ControlMessage{
		Type: protocol.MsgDeviceLogsResponse,
		LogEntries: []protocol.LogEntry{
			{Timestamp: "2026-01-01T00:00:01Z", Level: "INFO", Target: "agent", Message: "started"},
			{Timestamp: "2026-01-01T00:00:02Z", Level: "ERROR", Target: "network", Message: "connection lost"},
		},
		TotalCount: 2,
	}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)

	var frameBuf bytes.Buffer
	err = codec.WriteFrame(&frameBuf, protocol.FrameControl, payload)
	require.NoError(t, err)

	ac := &AgentConn{
		DeviceID:   d.ID,
		GroupID:    group.ID,
		stream:     &frameBuf,
		codec:      codec,
		devices:    testutil.NewTestDevices(t, store),
		hardware:   testutil.NewTestHardware(t, store),
		deviceLogs: testutil.NewTestLogs(t, store),
		logger:     testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	// Verify logs were stored
	entries, total, err := testutil.NewTestLogs(t, store).Query(ctx, d.ID, device.LogFilter{Limit: 100})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, entries, 2)
}
