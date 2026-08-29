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
// It bounds two different things at once, which is what makes the figure worth
// stating: the relay test must outlive it, so a smaller number is a faster
// test — but the session the relay is built on is created through a timed API
// route, so the same number is also the budget that setup has to finish inside.
//
// A budget sized only for the first of those is the one that fails. At 150ms
// the POST that creates the session answered 503 under mutation-testing load,
// where the whole test suite runs once per mutant and a Postgres commit waits
// behind dozens of its own copies — a green relay assertion was never reached,
// because setup never got that far. Two seconds is an order of magnitude clear
// of that, and still well inside the 5s this file already allows one socket
// operation.
const apiTimeoutUnderTest = 2 * time.Second

// relayIdleMargin is how far past the API timeout the relay is held open. One
// crossing is the whole claim — a relay inside the middleware site is closed at
// the first one — so the margin is small and the test does not pay for a second.
const relayIdleMargin = 250 * time.Millisecond

// TestInjectedRequestTimeoutReachesAPIRoutes is the control for
// TestRelayRouteBypassesRequestTimeout: it proves ServerConfig.RequestTimeout
// actually reaches the middleware site. Without it, a mis-wired field would
// leave the relay test asserting survival past a timeout that was never armed.
// A 1ns budget cannot be met by any route that touches Postgres, so the site
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
		"a 1ns RequestTimeout must expire on an API route — the injected value is not reaching the middleware site")
}

// relayExchangeTimeout bounds the whole heartbeat exchange below, once, rather
// than each beat separately. What the test asserts is that the relay survives
// the API timeout; how much spare capacity the runner had while it survived is
// not part of the claim, so a beat that is slow because the machine is loaded
// must not read as a relay that died. A single budget for the exchange fails a
// genuinely broken relay in seconds and tolerates a starved one.
const relayExchangeTimeout = 30 * time.Second

// TestRelayRouteBypassesRequestTimeout verifies that the WebSocket relay route
// lives outside the RequestTimeout middleware site: a relay connection must
// stay bidirectionally functional well past the API timeout. The timeout is
// injected so the window is a multiple of a 150ms budget rather than the
// production 30s. RequestTimeout's own expiry behavior is pinned separately by
// TestRequestTimeout in internal/api; what this test owns is the route grouping.
func TestRelayRouteBypassesRequestTimeout(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnvWithAPITimeout(t, apiTimeoutUnderTest)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, relayExchangeTimeout)
	defer wsCancel()

	// The connection is held open, and idle, across the expiry point: a relay
	// inside the middleware site is already closed by the time the wait returns.
	start := time.Now()
	idleTimer := time.NewTimer(apiTimeoutUnderTest + relayIdleMargin)
	defer idleTimer.Stop()
	select {
	case <-idleTimer.C:
	case <-wsCtx.Done():
		t.Fatal("the exchange budget expired before the relay was even idle past the API timeout")
	}

	// Ten beats back to back, so the exchange spans the expiry point rather than
	// racing a wall-clock ticker the runner may not be able to keep.
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

	// Final assertion: connection still alive after the full window.
	payload := []byte("still-alive-after-timeout")
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, payload, data, "relay connection should survive past API timeout")
}
