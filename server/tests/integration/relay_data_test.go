package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
	"testing"
	"time"
)

func waitForRelayWired(t *testing.T, ctx context.Context, r *relay.Relay, token protocol.SessionToken) {
	t.Helper()
	require.Eventually(t, func() bool {
		waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		return r.WaitForPeer(waitCtx, token) == nil
	}, 3*time.Second, 25*time.Millisecond, "relay should wire both sides of session %s", token)
}

func (e *sessionTestEnv) setupRelayPair(t *testing.T, ctx context.Context) (agentConn, browserConn *websocket.Conn) {
	t.Helper()

	user := testutil.SeedUser(t, ctx, e.store)
	site := testutil.SeedSite(t, ctx, e.store)

	jwtToken, err := e.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := e.connectAgent(t, site.ID)

	require.Eventually(t, func() bool {
		d, err := device.NewPostgresDevices(e.store.DB()).Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := e.createSession(t, jwtToken, deviceID, map[string]bool{"desktop": true})

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

	agentConn = e.dialRelayWS(t, ctx, result.Token, "agent", "")
	t.Cleanup(func() { agentConn.Close(websocket.StatusNormalClosure, "") })

	browserConn = e.dialRelayWS(t, ctx, result.Token, "browser", jwtToken)
	t.Cleanup(func() { browserConn.Close(websocket.StatusNormalClosure, "") })

	waitForRelayWired(t, ctx, e.relay, protocol.SessionToken(result.Token))

	return agentConn, browserConn
}

func TestRelayBinaryPayloadIntegrity(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	payloads := [][]byte{
		[]byte("hello-from-agent"),
		make([]byte, 1024),
		make([]byte, 16*1024),
	}
	for i := range payloads[1] {
		payloads[1][i] = byte(i % 256)
	}
	for i := range payloads[2] {
		payloads[2][i] = byte(i % 256)
	}

	for _, p := range payloads {
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, p))
		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		assert.Equal(t, p, data, "payload of size %d corrupted", len(p))
	}
}
