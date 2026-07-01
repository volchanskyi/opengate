package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
