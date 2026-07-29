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

	// The AMT link fields: the SMBIOS system UUID that resolves the device's
	// CIRA connection, plus what the host's Management Engine reports.
	assert.Equal(t, "4c4c4544-0037-5a10-8054-b4c04f335432", msg.SystemUUID)
	require.NotNil(t, msg.AMTAvailable, "a reporting agent always states AMT presence")
	assert.True(t, *msg.AMTAvailable)
	assert.Equal(t, "16.1.30.2260", msg.AMTVersion)
}

// TestGoldenControlHardwareReportNoAMT covers the other half of the presence
// flag: a host with no Management Engine reports false, and false must arrive as
// a stated false rather than as an absent field the server cannot distinguish
// from an agent too old to report at all.
func TestGoldenControlHardwareReportNoAMT(t *testing.T) {
	msg := decodeControlFrame(t, "control_hardware_report_no_amt.bin")

	assert.Equal(t, MsgHardwareReport, msg.Type)
	assert.Empty(t, msg.SystemUUID)
	require.NotNil(t, msg.AMTAvailable)
	assert.False(t, *msg.AMTAvailable)
	assert.Empty(t, msg.AMTVersion)
}
