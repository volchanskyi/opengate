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

func TestWebSocketUpgradeThroughFullMiddlewareStack(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	req, err := http.NewRequest(http.MethodGet, env.httpSrv.URL+"/api/v1/health", nil)
	require.NoError(t, err)
	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", resp.Header.Get("Referrer-Policy"))

	agentConn, browserConn := env.setupRelayPair(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

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

// apiTimeoutUnderTest is the injected RequestTimeout, which also bounds session-create setup.
const apiTimeoutUnderTest = 2 * time.Second

// relayIdleMargin is how far past the API timeout the relay is held open; one crossing suffices.
const relayIdleMargin = 250 * time.Millisecond

func TestInjectedRequestTimeoutReachesAPIRoutes(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnvWithAPITimeout(t, time.Nanosecond)

	req, err := http.NewRequest(http.MethodGet, env.httpSrv.URL+"/api/v1/health", nil)
	require.NoError(t, err)
	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"a 1ns RequestTimeout must expire on an API route — the injected value is not reaching the middleware site")
}

// relayExchangeTimeout bounds the whole heartbeat exchange once, so a loaded runner is tolerated
// and a broken relay still fails in seconds.
const relayExchangeTimeout = 30 * time.Second

func TestRelayRouteBypassesRequestTimeout(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnvWithAPITimeout(t, apiTimeoutUnderTest)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, relayExchangeTimeout)
	defer wsCancel()

	// A relay inside the middleware site would already be closed once this idle wait returns.
	start := time.Now()
	idleTimer := time.NewTimer(apiTimeoutUnderTest + relayIdleMargin)
	defer idleTimer.Stop()
	select {
	case <-idleTimer.C:
	case <-wsCtx.Done():
		t.Fatal("the exchange budget expired before the relay was even idle past the API timeout")
	}

	const heartbeats = 10
	t.Logf("relay idle for %s past the %s API timeout; exchanging %d heartbeats",
		time.Since(start), apiTimeoutUnderTest, heartbeats)

	for i := range heartbeats {
		payload := fmt.Appendf(nil, "heartbeat-%d", i)
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload), "heartbeat %d agent→browser write failed", i)
		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err, "heartbeat %d browser read failed", i)
		require.Equal(t, payload, data, "heartbeat %d payload mismatch", i)
	}

	require.Greater(t, time.Since(start), apiTimeoutUnderTest,
		"the exchange must outlast the API timeout for this test to mean anything")

	payload := []byte("still-alive-after-timeout")
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload, data, "relay connection should survive past API timeout")
}
