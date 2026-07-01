package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
