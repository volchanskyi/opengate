package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
