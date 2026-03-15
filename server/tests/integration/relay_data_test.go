package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
)

// setupRelayPair creates a session and connects both agent and browser WebSockets.
// The returned connections are cleaned up when the test ends.
func (e *sessionTestEnv) setupRelayPair(t *testing.T, ctx context.Context) (agentConn, browserConn *websocket.Conn) {
	t.Helper()

	user := testutil.SeedUser(t, ctx, e.store)
	group := testutil.SeedGroup(t, ctx, e.store, user.ID)

	jwtToken, err := e.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := e.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := e.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := e.createSession(t, jwtToken, deviceID, map[string]bool{"desktop": true})

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

	agentConn = e.dialRelayWS(t, ctx, result.Token, "agent", "")
	t.Cleanup(func() { agentConn.Close(websocket.StatusNormalClosure, "") })

	browserConn = e.dialRelayWS(t, ctx, result.Token, "browser", jwtToken)
	t.Cleanup(func() { browserConn.Close(websocket.StatusNormalClosure, "") })

	// Wait for relay to wire both sides
	time.Sleep(200 * time.Millisecond)

	return agentConn, browserConn
}

func TestRelayBinaryPayloadIntegrity(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	// Send multiple distinct messages and verify each arrives intact.
	// The relay streams data so messages may be split/merged; we verify
	// by sending individually and reading each message back.
	payloads := [][]byte{
		[]byte("hello-from-agent"),
		make([]byte, 1024),   // 1 KB zeros
		make([]byte, 16*1024), // 16 KB zeros
	}
	// Fill with recognizable patterns
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

func TestRelayLargeFrameSequence(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wsCancel()

	// Send 20 sequential small messages, verify ordering is preserved
	const msgCount = 20

	for i := 0; i < msgCount; i++ {
		payload := []byte{byte(i), byte(i + 100)}
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))

		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		assert.Equal(t, payload, data, "message %d corrupted or reordered", i)
	}
}

func TestRelayBidirectionalConcurrent(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wsCancel()

	const msgCount = 20

	var wg sync.WaitGroup

	// Agent → Browser
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			payload := []byte{byte(i), 'A'}
			require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
		}
	}()

	// Browser → Agent
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			payload := []byte{byte(i), 'B'}
			require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, payload))
		}
	}()

	// Receive at browser (from agent)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			_, data, err := browserConn.Read(wsCtx)
			require.NoError(t, err)
			assert.Equal(t, byte('A'), data[1], "expected agent message at browser")
		}
	}()

	// Receive at agent (from browser)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			_, data, err := agentConn.Read(wsCtx)
			require.NoError(t, err)
			assert.Equal(t, byte('B'), data[1], "expected browser message at agent")
		}
	}()

	wg.Wait()
}
