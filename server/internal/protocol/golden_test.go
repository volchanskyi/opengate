package protocol

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func goldenDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "testdata", "golden")
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(goldenDir(), name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "Golden file %s not found. Run Rust golden generator first.", name)
	return data
}

func TestGoldenPingPong(t *testing.T) {
	ping := readGolden(t, "ping.bin")
	assert.Equal(t, []byte{FramePing}, ping)

	pong := readGolden(t, "pong.bin")
	assert.Equal(t, []byte{FramePong}, pong)
}

func TestGoldenHandshakeServerHello(t *testing.T) {
	data := readGolden(t, "handshake_server_hello.bin")
	assert.Len(t, data, 81)
	assert.Equal(t, byte(MsgServerHello), data[0])

	nonce, certHash, err := DecodeServerHello(data)
	require.NoError(t, err)

	var expectedNonce [32]byte
	var expectedHash [48]byte
	for i := range expectedNonce {
		expectedNonce[i] = 0xAA
	}
	for i := range expectedHash {
		expectedHash[i] = 0xBB
	}
	assert.Equal(t, expectedNonce, nonce)
	assert.Equal(t, expectedHash, certHash)
}

func TestGoldenFrameWireFormat(t *testing.T) {
	// All framed golden files share the wire format: [type][4-byte-BE-length][msgpack-payload]
	tests := []struct {
		name          string
		file          string
		expectedType  byte
	}{
		{"AgentRegister", "control_agent_register.bin", FrameControl},
		{"Heartbeat", "control_heartbeat.bin", FrameControl},
		{"RelayReady", "control_relay_ready.bin", FrameControl},
		{"DesktopFrame", "desktop_frame.bin", FrameDesktop},
		{"SwitchToWebRTC", "control_switch_to_webrtc.bin", FrameControl},
		{"IceCandidate", "control_ice_candidate.bin", FrameControl},
		{"SwitchAck", "control_switch_ack.bin", FrameControl},
		{"AgentUpdateAck", "control_agent_update_ack.bin", FrameControl},
		{"AgentDeregistered", "control_agent_deregistered.bin", FrameControl},
		{"RestartAgent", "control_restart_agent.bin", FrameControl},
		{"RequestHardwareReport", "control_request_hardware_report.bin", FrameControl},
		{"HardwareReport", "control_hardware_report.bin", FrameControl},
		{"HardwareReportError", "control_hardware_report_error.bin", FrameControl},
		{"RequestDeviceLogs", "control_request_device_logs.bin", FrameControl},
		{"DeviceLogsResponse", "control_device_logs_response.bin", FrameControl},
		{"DeviceLogsError", "control_device_logs_error.bin", FrameControl},
	}

	codec := &Codec{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := readGolden(t, tt.file)

			// Verify frame type byte
			assert.Equal(t, tt.expectedType, data[0], "frame type mismatch")

			// Verify we can read the frame
			reader := bytes.NewReader(data)
			frameType, payload, err := codec.ReadFrame(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, frameType)
			assert.NotEmpty(t, payload, "payload should not be empty")

			// For control frames, verify we can decode the msgpack payload
			if frameType == FrameControl {
				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err, "failed to decode control message from golden file")
				assert.NotEmpty(t, msg.Type)
			}
		})
	}
}

func TestGoldenControlAgentRegister(t *testing.T) {
	data := readGolden(t, "control_agent_register.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)

	// The Rust golden file encodes this specific message
	assert.Equal(t, MsgAgentRegister, msg.Type)
	assert.Equal(t, "golden-test-host", msg.Hostname)
	assert.Equal(t, "linux", msg.OS)
	assert.Equal(t, "amd64", msg.Arch)
	assert.Equal(t, "0.1.0", msg.Version)
	assert.Len(t, msg.Capabilities, 2)
}

func TestGoldenControlSwitchToWebRTC(t *testing.T) {
	data := readGolden(t, "control_switch_to_webrtc.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgSwitchToWebRTC, msg.Type)
	assert.Equal(t, "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n", msg.SDPOffer)
}

func TestGoldenControlIceCandidate(t *testing.T) {
	data := readGolden(t, "control_ice_candidate.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgIceCandidate, msg.Type)
	assert.Equal(t, "candidate:1 1 UDP 2130706431 192.168.1.1 50000 typ host", msg.Candidate)
	assert.Equal(t, "0", msg.Mid)
}

func TestGoldenControlSwitchAck(t *testing.T) {
	data := readGolden(t, "control_switch_ack.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgSwitchAck, msg.Type)
}

func TestGoldenControlAgentUpdateAck(t *testing.T) {
	data := readGolden(t, "control_agent_update_ack.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgAgentUpdateAck, msg.Type)
	assert.Equal(t, "1.2.3", msg.Version)
	require.NotNil(t, msg.Success)
	assert.True(t, *msg.Success)
	assert.Empty(t, msg.AckError)
}

func TestGoldenControlAgentDeregistered(t *testing.T) {
	data := readGolden(t, "control_agent_deregistered.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgAgentDeregistered, msg.Type)
	assert.Equal(t, "device deleted by administrator", msg.Reason)
}

func TestGoldenControlRestartAgent(t *testing.T) {
	data := readGolden(t, "control_restart_agent.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgRestartAgent, msg.Type)
	assert.Equal(t, "restart requested from web UI", msg.Reason)
}

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

func TestGoldenControlHardwareReportError(t *testing.T) {
	data := readGolden(t, "control_hardware_report_error.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgHardwareReportError, msg.Type)
	assert.Equal(t, "failed to read system info", msg.AckError)
}

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
}

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

func TestGoldenDesktopFrame(t *testing.T) {
	data := readGolden(t, "desktop_frame.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameDesktop, frameType)

	frame, err := codec.DecodeDesktopFrame(payload)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), frame.Sequence)
	assert.Equal(t, uint16(10), frame.X)
	assert.Equal(t, uint16(20), frame.Y)
	assert.Equal(t, uint16(1920), frame.Width)
	assert.Equal(t, uint16(1080), frame.Height)
	assert.Equal(t, EncodingZstd, frame.Encoding)
	assert.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, frame.Data)
}

func TestGoldenDesktopFrameJpeg(t *testing.T) {
	data := readGolden(t, "desktop_frame_jpeg.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameDesktop, frameType)

	frame, err := codec.DecodeDesktopFrame(payload)
	require.NoError(t, err)
	assert.Equal(t, uint64(99), frame.Sequence)
	assert.Equal(t, uint16(0), frame.X)
	assert.Equal(t, uint16(0), frame.Y)
	assert.Equal(t, uint16(1920), frame.Width)
	assert.Equal(t, uint16(1080), frame.Height)
	assert.Equal(t, EncodingJpeg, frame.Encoding)
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF, 0xE0}, frame.Data)
}
