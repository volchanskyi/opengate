package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenControlDeviceLogsResponse(t *testing.T) {
	data := readGolden(t, "control_device_logs_response.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgDeviceLogsResponse, msg.Type)
	require.Len(t, msg.LogEntries, 2)
	assert.Equal(t, "2026-04-01T12:00:00.000000Z", msg.LogEntries[0].Timestamp)
	assert.Equal(t, "WARN", msg.LogEntries[0].Level)
	assert.Equal(t, "mesh_agent::connection", msg.LogEntries[0].Target)
	assert.Equal(t, "slow heartbeat detected", msg.LogEntries[0].Message)
	assert.Equal(t, "2026-04-01T12:00:01.000000Z", msg.LogEntries[1].Timestamp)
	assert.Equal(t, "ERROR", msg.LogEntries[1].Level)
	assert.Equal(t, "connection lost", msg.LogEntries[1].Message)
	assert.Equal(t, uint32(42), msg.TotalCount)
	require.NotNil(t, msg.HasMore)
	assert.True(t, *msg.HasMore)
}

func TestGoldenControlDeviceLogsError(t *testing.T) {
	data := readGolden(t, "control_device_logs_error.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgDeviceLogsError, msg.Type)
	assert.Equal(t, "log directory not found", msg.AckError)
}
