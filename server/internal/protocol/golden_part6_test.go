package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
