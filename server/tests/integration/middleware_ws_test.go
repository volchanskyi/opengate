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

// apiTimeoutUnderTest is the injected RequestTimeout for the two tests below.
// It is short enough that "outlives the API timeout" costs ~1.5s of wall clock
// instead of ~32s, and long enough that a healthy REST round-trip completes
// inside it on a loaded CI runner.
const apiTimeoutUnderTest = 150 * time.Millisecond

// TestInjectedRequestTimeoutReachesAPIRoutes is the control for
// TestRelayRouteBypassesRequestTimeout: it proves ServerConfig.RequestTimeout
// actually reaches the middleware group. Without it, a mis-wired field would
// leave the relay test asserting survival past a timeout that was never armed.
// A 1ns budget cannot be met by any route that touches Postgres, so the group
// must answer 503 from http.TimeoutHandler.
func TestInjectedRequestTimeoutReachesAPIRoutes(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnvWithAPITimeout(t, time.Nanosecond)

	req, err := http.NewRequest(http.MethodGet, env.httpSrv.URL+"/api/v1/health", nil)
	require.NoError(t, err)
	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"a 1ns RequestTimeout must expire on an API route — the injected value is not reaching the middleware group")
}

// TestRelayRouteBypassesRequestTimeout verifies that the WebSocket relay route
// lives outside the RequestTimeout middleware group: a relay connection must
// stay bidirectionally functional well past the API timeout. The timeout is
// injected so the window is a multiple of a 150ms budget rather than the
// production 30s. RequestTimeout's own expiry behavior is pinned separately by
// TestRequestTimeout in internal/api; what this test owns is the route grouping.
func TestRelayRouteBypassesRequestTimeout(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnvWithAPITimeout(t, apiTimeoutUnderTest)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)

	// Ten heartbeats one timeout-budget apart ⇒ the exchange spans 10× the API
	// timeout, and every beat after the first lands past the expiry point.
	const heartbeats = 10
	totalDuration := heartbeats * apiTimeoutUnderTest
	t.Logf("exchanging heartbeats every %s for %s to confirm relay survives past the %s API timeout",
		apiTimeoutUnderTest, totalDuration, apiTimeoutUnderTest)

	ticker := time.NewTicker(apiTimeoutUnderTest)
	defer ticker.Stop()
	start := time.Now()

	for i := range heartbeats {
		<-ticker.C
		wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
		payload := fmt.Appendf(nil, "heartbeat-%d", i)
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload), "heartbeat %d agent→browser write failed", i)
		_, data, err := browserConn.Read(wsCtx)
		wsCancel()
		require.NoError(t, err, "heartbeat %d browser read failed", i)
		require.Equal(t, payload, data, "heartbeat %d payload mismatch", i)
	}

	require.Greater(t, time.Since(start), apiTimeoutUnderTest,
		"the exchange must outlast the API timeout for this test to mean anything")

	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	// Final assertion: connection still alive after the full window.
	payload := []byte("still-alive-after-timeout")
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload, data, "relay connection should survive past API timeout")
}
