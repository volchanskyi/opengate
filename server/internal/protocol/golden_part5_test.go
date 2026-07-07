package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenControlRequestDeviceLogs(t *testing.T) {
	data := readGolden(t, "control_request_device_logs.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgRequestDeviceLogs, msg.Type)
	assert.Equal(t, "WARN", msg.LogLevel)
	assert.Equal(t, "2026-04-01T00:00:00Z", msg.TimeFrom)
	assert.Equal(t, "2026-04-01T23:59:59Z", msg.TimeTo)
	assert.Equal(t, "connection", msg.Search)
	assert.Equal(t, uint32(0), msg.LogOffset)
	assert.Equal(t, uint32(100), msg.LogLimit)
	// Host-source selector + structured emitting-unit filter.
	assert.Equal(t, "journald", msg.Source)
	assert.Equal(t, "nginx.service", msg.Unit)
}
