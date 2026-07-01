package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
	"testing"
	"time"
)

func TestSessionLifecycle_AgentDisconnectDuringSession(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := env.createSession(t, jwtToken, deviceID, nil)

	// Read SessionRequest and accept
	codec := &protocol.Codec{}
	_, _, err = codec.ReadFrame(stream)
	require.NoError(t, err)

	acceptMsg := &protocol.ControlMessage{
		Type:  protocol.MsgSessionAccept,
		Token: protocol.SessionToken(result.Token),
	}
	acceptPayload, err := codec.EncodeControl(acceptMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, acceptPayload))

	// Connect both sides to relay
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	agentWSConn := env.dialRelayWS(t, wsCtx, result.Token, "agent", "")
	browserConn := env.dialRelayWS(t, wsCtx, result.Token, "browser", jwtToken)
	defer browserConn.Close(websocket.StatusNormalClosure, "")

	waitForRelayWired(t, ctx, env.relay, protocol.SessionToken(result.Token))

	// Verify data flows
	require.NoError(t, agentWSConn.Write(wsCtx, websocket.MessageBinary, []byte("pre-disconnect")))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, []byte("pre-disconnect"), data)

	// Close agent WebSocket
	agentWSConn.Close(websocket.StatusNormalClosure, "")

	// Browser should get an error
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	_, _, err = browserConn.Read(readCtx)
	assert.Error(t, err)

	// Relay active count should drop
	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 5*time.Second, 100*time.Millisecond)
}
