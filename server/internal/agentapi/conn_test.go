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

func TestAgentConn_SendRestartAgent(t *testing.T) {
	codec := &protocol.Codec{}
	var buf bytes.Buffer

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &buf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	err := ac.SendRestartAgent(context.Background(), "restart requested from web UI")
	require.NoError(t, err)

	frameType, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgRestartAgent, decoded.Type)
	assert.Equal(t, "restart requested from web UI", decoded.Reason)
}

func TestAgentConn_SendRequestHardwareReport(t *testing.T) {
	codec := &protocol.Codec{}
	var buf bytes.Buffer

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &buf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	err := ac.SendRequestHardwareReport(context.Background())
	require.NoError(t, err)

	frameType, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgRequestHardwareReport, decoded.Type)
}

func TestAgentConn_SendRequestDeviceLogs(t *testing.T) {
	codec := &protocol.Codec{}
	var buf bytes.Buffer

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &buf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	filter := db.LogFilter{
		Level:  "ERROR",
		From:   "2026-01-01T00:00:00Z",
		To:     "2026-01-02T00:00:00Z",
		Search: "panic",
		Offset: 10,
		Limit:  50,
	}

	err := ac.SendRequestDeviceLogs(context.Background(), filter)
	require.NoError(t, err)

	frameType, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgRequestDeviceLogs, decoded.Type)
	assert.Equal(t, "ERROR", decoded.LogLevel)
	assert.Equal(t, "2026-01-01T00:00:00Z", decoded.TimeFrom)
	assert.Equal(t, "2026-01-02T00:00:00Z", decoded.TimeTo)
	assert.Equal(t, "panic", decoded.Search)
	assert.Equal(t, uint32(10), decoded.LogOffset)
	assert.Equal(t, uint32(50), decoded.LogLimit)
}

func TestAgentConn_HandleDeviceLogsResponse(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

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
		DeviceID: device.ID,
		GroupID:  group.ID,
		stream:   &frameBuf,
		codec:    codec,
		store:    store,
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	// Verify logs were stored
	entries, total, err := store.QueryDeviceLogs(ctx, device.ID, db.LogFilter{Limit: 100})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, entries, 2)
}

func TestAgentConn_HandleDeviceLogsError(t *testing.T) {
	codec := &protocol.Codec{}

	msg := &protocol.ControlMessage{
		Type:     protocol.MsgDeviceLogsError,
		AckError: "permission denied",
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
	require.NoError(t, err)
}

func TestAgentConn_HandleHardwareReportError(t *testing.T) {
	codec := &protocol.Codec{}

	msg := &protocol.ControlMessage{
		Type:     protocol.MsgHardwareReportError,
		AckError: "not supported",
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
	require.NoError(t, err)
}

func TestAgentConn_HandleHardwareReport(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

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
		DeviceID: device.ID,
		GroupID:  group.ID,
		stream:   &frameBuf,
		codec:    codec,
		store:    store,
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	// Verify hardware was stored
	hw, err := store.GetDeviceHardware(ctx, device.ID)
	require.NoError(t, err)
	assert.Equal(t, "Intel i7-12700", hw.CPUModel)
	assert.Equal(t, 12, hw.CPUCores)
	assert.Equal(t, int64(32768), hw.RAMTotalMB)
	assert.Len(t, hw.NetworkInterfaces, 1)
	assert.Equal(t, "eth0", hw.NetworkInterfaces[0].Name)
}

func TestAgentConn_HandleRegister_NormalizesOS(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

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
		store:    store,
		logger:   testLogger(),
	}

	err = ac.handleControl(ctx)
	require.NoError(t, err)

	device, err := store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "linux", device.OS, "OS should be normalized")
	assert.Equal(t, "Ubuntu 22.04 LTS", device.OsDisplay, "OsDisplay should preserve original")
	assert.Equal(t, "amd64", ac.Arch, "Arch should be normalized from x86_64")
}

func TestAgentConn_SendAgentUpdate(t *testing.T) {
	codec := &protocol.Codec{}
	var buf bytes.Buffer

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &buf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	err := ac.SendAgentUpdate(context.Background(), "0.3.0", "https://example.com/agent", "sha256hash", "sig123")
	require.NoError(t, err)

	frameType, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentUpdate, decoded.Type)
	assert.Equal(t, "0.3.0", decoded.Version)
	assert.Equal(t, "https://example.com/agent", decoded.URL)
	assert.Equal(t, "sha256hash", decoded.SHA256)
	assert.Equal(t, "sig123", decoded.Signature)
}

func TestAgentConn_SendAgentDeregistered(t *testing.T) {
	codec := &protocol.Codec{}
	var buf bytes.Buffer

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &buf,
		codec:    codec,
		store:    nil,
		logger:   testLogger(),
	}

	err := ac.SendAgentDeregistered(context.Background(), "device deleted")
	require.NoError(t, err)

	frameType, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentDeregistered, decoded.Type)
	assert.Equal(t, "device deleted", decoded.Reason)
}

func TestAgentConn_Close(t *testing.T) {
	t.Run("stream without closer", func(t *testing.T) {
		var buf bytes.Buffer
		ac := &AgentConn{stream: &buf, logger: testLogger()}
		assert.NoError(t, ac.Close())
	})
}

func TestAgentConn_HandleAgentUpdateAck(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

	// Create a pending update record
	require.NoError(t, store.CreateDeviceUpdate(ctx, &db.DeviceUpdate{
		DeviceID: device.ID,
		Version:  "0.5.0",
		Status:   db.UpdateStatusPending,
	}))

	codec := &protocol.Codec{}

	t.Run("success ack", func(t *testing.T) {
		success := true
		msg := &protocol.ControlMessage{
			Type:    protocol.MsgAgentUpdateAck,
			Version: "0.5.0",
			Success: &success,
		}
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)

		var frameBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FrameControl, payload))

		ac := &AgentConn{
			DeviceID: device.ID,
			GroupID:  group.ID,
			stream:   &frameBuf,
			codec:    codec,
			store:    store,
			logger:   testLogger(),
		}
		require.NoError(t, ac.handleControl(ctx))
	})

	t.Run("failure ack", func(t *testing.T) {
		success := false
		msg := &protocol.ControlMessage{
			Type:     protocol.MsgAgentUpdateAck,
			Version:  "0.5.0",
			Success:  &success,
			AckError: "checksum mismatch",
		}
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)

		var frameBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FrameControl, payload))

		ac := &AgentConn{
			DeviceID: device.ID,
			GroupID:  group.ID,
			stream:   &frameBuf,
			codec:    codec,
			store:    store,
			logger:   testLogger(),
		}
		require.NoError(t, ac.handleControl(ctx))
	})
}

func TestAgentConn_HandleSessionAcceptReject(t *testing.T) {
	codec := &protocol.Codec{}

	t.Run("session accept", func(t *testing.T) {
		msg := &protocol.ControlMessage{
			Type:  protocol.MsgSessionAccept,
			Token: protocol.GenerateSessionToken(),
		}
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)

		var frameBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FrameControl, payload))

		ac := &AgentConn{
			DeviceID: uuid.New(),
			stream:   &frameBuf,
			codec:    codec,
			logger:   testLogger(),
		}
		require.NoError(t, ac.handleControl(context.Background()))
	})

	t.Run("session reject", func(t *testing.T) {
		msg := &protocol.ControlMessage{
			Type:   protocol.MsgSessionReject,
			Token:  protocol.GenerateSessionToken(),
			Reason: "not supported",
		}
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)

		var frameBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FrameControl, payload))

		ac := &AgentConn{
			DeviceID: uuid.New(),
			stream:   &frameBuf,
			codec:    codec,
			logger:   testLogger(),
		}
		require.NoError(t, ac.handleControl(context.Background()))
	})
}

func TestAgentConn_HandlePingFrame(t *testing.T) {
	codec := &protocol.Codec{}

	var frameBuf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&frameBuf, protocol.FramePing, nil))

	ac := &AgentConn{
		DeviceID: uuid.New(),
		stream:   &frameBuf,
		codec:    codec,
		logger:   testLogger(),
	}

	err := ac.handleControl(context.Background())
	require.NoError(t, err)
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
