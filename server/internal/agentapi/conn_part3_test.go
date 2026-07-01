package agentapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"testing"
)

func TestAgentConn_SendSessionRequest(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	token := protocol.GenerateSessionToken()
	perms := protocol.Permissions{
		Desktop:  true,
		Terminal: true,
	}

	err := ac.SendSessionRequest(context.Background(), token, "wss://relay/test", perms)
	require.NoError(t, err)

	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgSessionRequest, decoded.Type)
	assert.Equal(t, token, decoded.Token)
	assert.Equal(t, "wss://relay/test", decoded.RelayURL)
	require.NotNil(t, decoded.Permissions)
	assert.True(t, decoded.Permissions.Desktop)
	assert.True(t, decoded.Permissions.Terminal)
}

func TestAgentConn_SendRestartAgent(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	err := ac.SendRestartAgent(context.Background(), "restart requested from web UI")
	require.NoError(t, err)

	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.FrameControl), frameType)

	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgRestartAgent, decoded.Type)
	assert.Equal(t, "restart requested from web UI", decoded.Reason)
}
