package agentapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestAgentConn_HandleRegister(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

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
		DeviceID: deviceID,
		GroupID:  group.ID,
		stream:   &frameBuf,
		codec:    codec,
		store:    store,
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	// Verify device was upserted
	device, err := store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "test-host", device.Hostname)
	assert.Equal(t, "linux", device.OS)
	assert.Equal(t, db.StatusOnline, device.Status)
}

func TestAgentConn_HandleHeartbeat(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

	codec := &protocol.Codec{}

	ts := time.Now().Unix()
	msg := &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: ts,
	}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)

	var frameBuf bytes.Buffer
	err = codec.WriteFrame(&frameBuf, protocol.FrameControl, payload)
	require.NoError(t, err)

	ac := &AgentConn{
		DeviceID: device.ID,
		GroupID:  group.ID,
		stream:   &frameBuf,
		codec:    codec,
		store:    store,
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	// Verify device status is online
	updated, err := store.GetDevice(ctx, device.ID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusOnline, updated.Status)
}

func TestAgentConn_SendSessionRequest(t *testing.T) {
	codec := &protocol.Codec{}
	var buf bytes.Buffer

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &buf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	token := protocol.GenerateSessionToken()
	perms := protocol.Permissions{
		Desktop:  true,
		Terminal: true,
	}

	err := ac.SendSessionRequest(context.Background(), token, "wss://relay/test", perms)
	require.NoError(t, err)

	// Decode what was written
	frameType, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgSessionRequest, decoded.Type)
	assert.Equal(t, token, decoded.Token)
	assert.Equal(t, "wss://relay/test", decoded.RelayURL)
	require.NotNil(t, decoded.Permissions)
	assert.True(t, decoded.Permissions.Desktop)
	assert.True(t, decoded.Permissions.Terminal)
}

func TestAgentConn_HandleUnknownMessage(t *testing.T) {
	codec := &protocol.Codec{}

	msg := &protocol.ControlMessage{
		Type: protocol.MsgAgentUpdate,
	}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)

	var frameBuf bytes.Buffer
	err = codec.WriteFrame(&frameBuf, protocol.FrameControl, payload)
	require.NoError(t, err)

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &frameBuf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	err = ac.handleControl(context.Background())
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnexpectedMessage))
}
