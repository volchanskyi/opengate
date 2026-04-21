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
		{"SessionAccept", "control_session_accept.bin", FrameControl},
		{"SessionReject", "control_session_reject.bin", FrameControl},
		{"SessionRequest", "control_session_request.bin", FrameControl},
		{"AgentUpdate", "control_agent_update.bin", FrameControl},
		{"FileListRequest", "control_file_list_request.bin", FrameControl},
		{"FileListResponse", "control_file_list_response.bin", FrameControl},
		{"FileListError", "control_file_list_error.bin", FrameControl},
		{"FileDownloadRequest", "control_file_download_request.bin", FrameControl},
		{"FileUploadRequest", "control_file_upload_request.bin", FrameControl},
		{"ChatMessage", "control_chat_message.bin", FrameControl},
		{"AgentRegisterEmptyCaps", "control_agent_register_empty_capabilities.bin", FrameControl},
		{"AgentRegisterUTF8", "control_agent_register_utf8.bin", FrameControl},
		{"HardwareReportLargeSize", "control_hardware_report_large_size.bin", FrameControl},
		{"ChatMessageForwardCompat", "control_chat_message_forward_compat.bin", FrameControl},
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

// Fixed session token used by cross-boundary Rust goldens (see
// agent/crates/mesh-protocol/tests/golden_test.rs::GOLDEN_SESSION_TOKEN).
const goldenSessionToken = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func decodeControlFrame(t *testing.T, file string) *ControlMessage {
	t.Helper()
	data := readGolden(t, file)
	codec := &Codec{}
	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

func TestGoldenControlSessionAccept(t *testing.T) {
	msg := decodeControlFrame(t, "control_session_accept.bin")
	assert.Equal(t, MsgSessionAccept, msg.Type)
	assert.Equal(t, SessionToken(goldenSessionToken), msg.Token)
	assert.Equal(t, "wss://relay.example.com/relay", msg.RelayURL)
}

func TestGoldenControlSessionReject(t *testing.T) {
	msg := decodeControlFrame(t, "control_session_reject.bin")
	assert.Equal(t, MsgSessionReject, msg.Type)
	assert.Equal(t, SessionToken(goldenSessionToken), msg.Token)
	assert.Equal(t, "agent is busy", msg.Reason)
}

func TestGoldenControlSessionRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_session_request.bin")
	assert.Equal(t, MsgSessionRequest, msg.Type)
	assert.Equal(t, SessionToken(goldenSessionToken), msg.Token)
	assert.Equal(t, "wss://relay.example.com/relay", msg.RelayURL)
	require.NotNil(t, msg.Permissions)
	assert.True(t, msg.Permissions.Desktop)
	assert.True(t, msg.Permissions.Terminal)
	assert.True(t, msg.Permissions.FileRead)
	assert.False(t, msg.Permissions.FileWrite)
	assert.True(t, msg.Permissions.Input)
}

func TestGoldenControlAgentUpdate(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_update.bin")
	assert.Equal(t, MsgAgentUpdate, msg.Type)
	assert.Equal(t, "1.2.3", msg.Version)
	assert.Equal(t, "https://updates.example.com/agent-1.2.3-linux-amd64", msg.URL)
	assert.Equal(t, 64, len(msg.SHA256))
	assert.Equal(t, 128, len(msg.Signature))
}

func TestGoldenControlFileListRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_list_request.bin")
	assert.Equal(t, MsgFileListRequest, msg.Type)
	assert.Equal(t, "/home/ivan", msg.Path)
}

func TestGoldenControlFileListResponse(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_list_response.bin")
	assert.Equal(t, MsgFileListResponse, msg.Type)
	assert.Equal(t, "/home/ivan", msg.Path)
	require.Len(t, msg.Entries, 2)
	assert.Equal(t, "documents", msg.Entries[0].Name)
	assert.True(t, msg.Entries[0].IsDir)
	assert.Equal(t, uint64(0), msg.Entries[0].Size)
	assert.Equal(t, int64(1_700_000_000), msg.Entries[0].Modified)
	assert.Equal(t, "notes.txt", msg.Entries[1].Name)
	assert.False(t, msg.Entries[1].IsDir)
	assert.Equal(t, uint64(2048), msg.Entries[1].Size)
	assert.Equal(t, int64(1_700_000_100), msg.Entries[1].Modified)
}

func TestGoldenControlFileListError(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_list_error.bin")
	assert.Equal(t, MsgFileListError, msg.Type)
	assert.Equal(t, "/root/secret", msg.Path)
	// The Rust FileListError uses `error`; Go's msgpack tag on AckError
	// also binds to `error`, which is how HardwareReportError decodes too.
	assert.Equal(t, "permission denied", msg.AckError)
}

func TestGoldenControlFileDownloadRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_download_request.bin")
	assert.Equal(t, MsgFileDownloadRequest, msg.Type)
	assert.Equal(t, "/home/ivan/notes.txt", msg.Path)
}

func TestGoldenControlFileUploadRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_upload_request.bin")
	assert.Equal(t, MsgFileUploadRequest, msg.Type)
	assert.Equal(t, "/home/ivan/uploads/archive.tar", msg.Path)
	assert.Equal(t, uint64(10_485_760), msg.TotalSize)
}

func TestGoldenControlChatMessage(t *testing.T) {
	msg := decodeControlFrame(t, "control_chat_message.bin")
	assert.Equal(t, MsgChatMessage, msg.Type)
	assert.Equal(t, "hello from the operator", msg.Text)
	assert.Equal(t, "operator@example.com", msg.Sender)
}

// --- Edge-case verifiers ---

func TestGoldenControlAgentRegisterEmptyCapabilities(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_register_empty_capabilities.bin")
	assert.Equal(t, MsgAgentRegister, msg.Type)
	assert.Equal(t, "headless-ci-runner", msg.Hostname)
	assert.Equal(t, "linux", msg.OS)
	assert.Equal(t, "aarch64", msg.Arch)
	assert.Equal(t, "0.1.0", msg.Version)
	assert.Empty(t, msg.Capabilities, "empty Vec must decode to empty/nil slice, not produce an error")
}

func TestGoldenControlAgentRegisterUTF8(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_register_utf8.bin")
	assert.Equal(t, MsgAgentRegister, msg.Type)
	// Emoji + CJK must round-trip bit-for-bit through msgpack.
	assert.Equal(t, "ラップトップ-🖥️-办公室", msg.Hostname)
	assert.Equal(t, "macos", msg.OS)
	assert.Equal(t, "aarch64", msg.Arch)
	assert.Equal(t, "0.1.0-αβγ", msg.Version)
	require.Len(t, msg.Capabilities, 1)
	assert.Equal(t, CapRemoteDesktop, msg.Capabilities[0])
}

func TestGoldenControlHardwareReportLargeSize(t *testing.T) {
	data := readGolden(t, "control_hardware_report_large_size.bin")
	// Frame length high bytes must be non-zero — proves the 4-byte BE header
	// is actually exercised, not just the low byte.
	require.Greater(t, len(data), 65_536,
		"large_size golden must exceed 64 KiB to exercise BE length high bytes")

	msg := decodeControlFrame(t, "control_hardware_report_large_size.bin")
	assert.Equal(t, MsgHardwareReport, msg.Type)
	assert.Equal(t, "AMD EPYC 7763 64-Core Processor", msg.CPUModel)
	assert.Equal(t, uint32(128), msg.CPUCores)
	assert.Equal(t, uint64(524_288), msg.RAMTotalMB)
	assert.Equal(t, uint64(8_388_608), msg.DiskTotalMB)
	assert.Equal(t, uint64(4_194_304), msg.DiskFreeMB)
	require.Len(t, msg.NetworkInterfaces, 2000)
	assert.Equal(t, "veth0000", msg.NetworkInterfaces[0].Name)
	assert.Equal(t, "veth1999", msg.NetworkInterfaces[1999].Name)
}

func TestGoldenControlChatMessageForwardCompat(t *testing.T) {
	// Forward-compatibility: the payload contains two msgpack keys
	// (future_field, future_number) that Go's ControlMessage struct does not
	// declare. They must be silently ignored — the known fields decode unchanged.
	msg := decodeControlFrame(t, "control_chat_message_forward_compat.bin")
	assert.Equal(t, MsgChatMessage, msg.Type)
	assert.Equal(t, "hello from the future", msg.Text)
	assert.Equal(t, "operator@example.com", msg.Sender)
}

func TestGoldenFrameControlLELength(t *testing.T) {
	// Negative test: length field encoded little-endian instead of big-endian.
	// BE interpretation inflates the declared length past MaxFrameSize, so
	// ReadFrame must return an error rather than hang or over-read.
	data := readGolden(t, "frame_control_le_length.bin")
	codec := &Codec{}
	_, _, err := codec.ReadFrame(bytes.NewReader(data))
	require.Error(t, err, "LE-encoded length must be rejected by the BE-parsing decoder")
	assert.ErrorIs(t, err, ErrFrameTooLarge)
}
