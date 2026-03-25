package integration

import (
	"context"
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
// A relay connection must survive longer than the API timeout.
func TestRelayRouteBypassesRequestTimeout(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)

	// Wait longer than the API RequestTimeout (30s) — we use 32s to confirm
	// the relay is not subject to it. This is the critical assertion.
	t.Log("waiting 32s to confirm relay survives past API timeout...")
	time.Sleep(32 * time.Second)

	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	// Connection should still be alive
	payload := []byte("still-alive-after-timeout")
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload, data, "relay connection should survive past API timeout")
}
