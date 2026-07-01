package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenControlRequestHardwareReport(t *testing.T) {
	data := readGolden(t, "control_request_hardware_report.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgRequestHardwareReport, msg.Type)
}

func TestGoldenControlHardwareReport(t *testing.T) {
	data := readGolden(t, "control_hardware_report.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgHardwareReport, msg.Type)
	assert.Equal(t, "Intel Core i7-12700K", msg.CPUModel)
	assert.Equal(t, uint32(12), msg.CPUCores)
	assert.Equal(t, uint64(32768), msg.RAMTotalMB)
	assert.Equal(t, uint64(512000), msg.DiskTotalMB)
	assert.Equal(t, uint64(256000), msg.DiskFreeMB)
	require.Len(t, msg.NetworkInterfaces, 1)
	assert.Equal(t, "eth0", msg.NetworkInterfaces[0].Name)
	assert.Equal(t, "00:11:22:33:44:55", msg.NetworkInterfaces[0].MAC)
	assert.Equal(t, []string{"192.168.1.100"}, msg.NetworkInterfaces[0].IPv4)
	assert.Equal(t, []string{"fe80::1"}, msg.NetworkInterfaces[0].IPv6)
}
