package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"nhooyr.io/websocket"
	"testing"
	"time"
)

func TestSessionLifecycle_CreateAndRelay(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// 1. Connect QUIC agent
	stream, deviceID := env.connectAgent(t, group.ID)

	// Wait for agent to register as online
	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// 2. Create session via REST API
	result := env.createSession(t, jwtToken, deviceID, map[string]bool{"desktop": true})
	assert.Len(t, result.Token, 64)
	assert.Contains(t, result.RelayURL, "/ws/relay/"+result.Token)

	// 3. Read SessionRequest from QUIC control stream
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	assert.Equal(t, protocol.FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgSessionRequest, msg.Type)
	assert.Equal(t, protocol.SessionToken(result.Token), msg.Token)
	require.NotNil(t, msg.Permissions)
	assert.True(t, msg.Permissions.Desktop)

	// 4. Agent sends SessionAccept
	acceptMsg := &protocol.ControlMessage{
		Type:  protocol.MsgSessionAccept,
		Token: protocol.SessionToken(result.Token),
	}
	acceptPayload, err := codec.EncodeControl(acceptMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, acceptPayload))

	// 5. Browser connects to relay
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	browserConn := env.dialRelayWS(t, wsCtx, result.Token, "browser", jwtToken)
	defer browserConn.Close(websocket.StatusNormalClosure, "")

	// 6. Agent connects to relay
	agentConn := env.dialRelayWS(t, wsCtx, result.Token, "agent", "")
	defer agentConn.Close(websocket.StatusNormalClosure, "")

	// Wait for relay pipe to start (both sides registered).
	waitForRelayWired(t, ctx, env.relay, protocol.SessionToken(result.Token))

	// 7. Agent sends test payload → browser receives it
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, []byte("agent-payload")))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, []byte("agent-payload"), data)

	// 8. Browser sends test payload → agent receives it
	require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, []byte("browser-payload")))
	_, data, err = agentConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, []byte("browser-payload"), data)

	// 9. Delete session
	status := env.deleteSession(t, jwtToken, result.Token)
	assert.Equal(t, http.StatusNoContent, status)
}
