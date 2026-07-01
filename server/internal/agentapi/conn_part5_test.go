package agentapi

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
)

func TestAgentConn_HandleDeviceLogsError(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:     protocol.MsgDeviceLogsError,
		AckError: "permission denied",
	})

	require.NoError(t, ac.handleControl(context.Background()))
}

func TestAgentConn_HandleHardwareReportError(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:     protocol.MsgHardwareReportError,
		AckError: "not supported",
	})

	require.NoError(t, ac.handleControl(context.Background()))
}

func TestAgentConn_HandleHardwareReport(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	d := testutil.SeedDevice(t, ctx, store, group.ID)

	codec := &protocol.Codec{}

	msg := &protocol.ControlMessage{
		Type:        protocol.MsgHardwareReport,
		CPUModel:    "Intel i7-12700",
		CPUCores:    12,
		RAMTotalMB:  32768,
		DiskTotalMB: 512000,
		DiskFreeMB:  256000,
		NetworkInterfaces: []protocol.NetworkInterface{
			{Name: "eth0", MAC: "00:11:22:33:44:55", IPv4: []string{"192.168.1.10"}, IPv6: []string{"::1"}},
		},
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

	// Verify hardware was stored
	hw, err := testutil.NewTestHardware(t, store).Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, "Intel i7-12700", hw.CPUModel)
	assert.Equal(t, 12, hw.CPUCores)
	assert.Equal(t, int64(32768), hw.RAMTotalMB)
	assert.Len(t, hw.NetworkInterfaces, 1)
	assert.Equal(t, "eth0", hw.NetworkInterfaces[0].Name)
}
