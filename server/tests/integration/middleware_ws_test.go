package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

// TestWebSocketUpgradeThroughFullMiddlewareStack verifies that the full
// middleware chain (Recoverer → RequestID → SecurityHeaders → MaxBodySize →
// RequestLogger) does not break WebSocket upgrades. The http.Hijacker
// interface must be preserved through all response writer wrappers.
func TestWebSocketUpgradeThroughFullMiddlewareStack(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	// 1. Verify security headers on a REST endpoint
	req, err := http.NewRequest(http.MethodGet, env.httpSrv.URL+"/api/v1/health", nil)
	require.NoError(t, err)
	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", resp.Header.Get("Referrer-Policy"))

	// 2. Verify WebSocket upgrade succeeds through the same middleware stack
	agentConn, browserConn := env.setupRelayPair(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	// 3. Verify bidirectional data flows through the relay
	payload := []byte("middleware-stack-test-payload")
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload, data)

	payload2 := []byte("browser-to-agent-through-middleware")
	require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, payload2))
	_, data2, err := agentConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload2, data2)
}

// TestRelayRouteBypassesRequestTimeout verifies that the WebSocket relay
// route lives outside the 30-second RequestTimeout middleware group.
// A relay connection must survive longer than the API timeout. Instead of
// sleeping silently for 32s the test exchanges heartbeats every 2s for the
// duration, asserting the relay stays bidirectionally functional past the
// 30s timeout point.
func TestRelayRouteBypassesRequestTimeout(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)

	const (
		heartbeatInterval = 2 * time.Second
		totalDuration     = 32 * time.Second
	)
	t.Logf("exchanging heartbeats every %s for %s to confirm relay survives past API timeout", heartbeatInterval, totalDuration)

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(totalDuration)

	for i := 0; time.Now().Before(deadline); i++ {
		<-ticker.C
		wsCtx, wsCancel := context.WithTimeout(ctx, 2*time.Second)
		payload := []byte(fmt.Sprintf("heartbeat-%d", i))
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload), "heartbeat %d agent→browser write failed", i)
		_, data, err := browserConn.Read(wsCtx)
		wsCancel()
		require.NoError(t, err, "heartbeat %d browser read failed", i)
		require.Equal(t, payload, data, "heartbeat %d payload mismatch", i)
	}

	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	// Final assertion: connection still alive after the full window.
	payload := []byte("still-alive-after-timeout")
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload, data, "relay connection should survive past API timeout")
}
