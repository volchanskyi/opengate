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

func TestAgentConn_HandleRegister_NormalizesOS(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	group := testutil.SeedGroup(t, ctx, store, testutil.SeedUser(t, ctx, store).ID)

	deviceID := uuid.New()
	codec := &protocol.Codec{}

	msg := &protocol.ControlMessage{
		Type:     protocol.MsgAgentRegister,
		Hostname: "test-host",
		OS:       "Ubuntu 22.04 LTS",
		Arch:     "x86_64",
		Version:  "0.2.0",
	}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)

	var frameBuf bytes.Buffer
	err = codec.WriteFrame(&frameBuf, protocol.FrameControl, payload)
	require.NoError(t, err)

	ac := &AgentConn{
		DeviceID: deviceID,
		GroupID:  group.ID,
		stream:   &frameBuf,
		codec:    codec,
		devices:  testutil.NewTestDevices(t, store),
		hardware: testutil.NewTestHardware(t, store),
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	d, err := testutil.NewTestDevices(t, store).Get(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "linux", d.OS, "OS should be normalized")
	assert.Equal(t, "Ubuntu 22.04 LTS", d.OsDisplay, "OsDisplay should preserve original")
	assert.Equal(t, "amd64", ac.Arch, "Arch should be normalized from x86_64")
}
