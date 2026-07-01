package agentapi

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
)

func TestAgentConn_HandleRegister(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	// Create a group so the device can belong to it
	group := testutil.SeedGroup(t, ctx, store, testutil.SeedUser(t, ctx, store).ID)

	deviceID := uuid.New()
	codec := &protocol.Codec{}

	// Encode an AgentRegister message into a buffer
	msg := &protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal, protocol.CapFileManager},
		Hostname:     "test-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)

	var frameBuf bytes.Buffer
	err = codec.WriteFrame(&frameBuf, protocol.FrameControl, payload)
	require.NoError(t, err)

	ac := &AgentConn{
		DeviceID:   deviceID,
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

	// Verify device was upserted
	d, err := testutil.NewTestDevices(t, store).Get(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "test-host", d.Hostname)
	assert.Equal(t, "linux", d.OS)
	assert.Equal(t, db.StatusOnline, d.Status)
}
