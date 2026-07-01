package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
