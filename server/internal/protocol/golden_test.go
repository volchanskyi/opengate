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
