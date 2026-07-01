package agentapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"testing"
)

func TestAgentConn_SendAgentUpdate(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendAgentUpdate(context.Background(), "0.3.0", "https://example.com/agent", "sha256hash", "sig123")
	require.NoError(t, err)

	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentUpdate, decoded.Type)
	assert.Equal(t, "0.3.0", decoded.Version)
	assert.Equal(t, "https://example.com/agent", decoded.URL)
	assert.Equal(t, "sha256hash", decoded.SHA256)
	assert.Equal(t, "sig123", decoded.Signature)
}

func TestAgentConn_SendAgentDeregistered(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendAgentDeregistered(context.Background(), "device deleted")
	require.NoError(t, err)

	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentDeregistered, decoded.Type)
	assert.Equal(t, "device deleted", decoded.Reason)
}

func TestAgentConn_Close(t *testing.T) {
	t.Run("stream without closer", func(t *testing.T) {
		ac, _ := newTestAgentConn(t, uuid.New(), nil)
		assert.NoError(t, ac.Close())
	})
}
