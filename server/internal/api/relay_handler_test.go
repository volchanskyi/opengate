package api

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
)

const (
	testPathWSRelay  = "/ws/relay/"
	testSideBrowser  = "?side=browser"
	testSideAgent    = "?side=agent"
	testBearerPrefix = "Bearer "
)

func newRelayTestServer(t *testing.T) (*httptest.Server, *Server, *auth.JWTConfig) {
	t.Helper()
	return newRelayTestServerWith(t, relay.NewRelay(slog.Default()))
}

// newRelayTestServerWith is newRelayTestServer with a caller-supplied relay, so
// tests can inject a relay backed by a degraded registry (readiness probe).
func newRelayTestServerWith(t *testing.T, r *relay.Relay) (*httptest.Server, *Server, *auth.JWTConfig) {
	t.Helper()
	return newRelayTestServerWithPeerTimeout(t, r, 0)
}

func newRelayTestServerWithPeerTimeout(t *testing.T, r *relay.Relay, peerTimeout time.Duration) (*httptest.Server, *Server, *auth.JWTConfig) {
	t.Helper()
	ts, srv, cfg, _ := newWatchedRelayTestServer(t, r, peerTimeout)
	return ts, srv, cfg
}

// relayHandlerWatch reports every relay handler that has returned, because net/http stops
// tracking a hijacked connection.
type relayHandlerWatch struct {
	inner    http.Handler
	returned chan string
}

func (w *relayHandlerWatch) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.inner.ServeHTTP(rw, r)
	if strings.HasPrefix(r.URL.Path, testPathWSRelay) {
		select {
		case w.returned <- r.URL.Query().Get("side"):
		default:
		}
	}
}

// newWatchedRelayTestServer is newRelayTestServerWithPeerTimeout plus a channel
// carrying the ?side= of each relay handler that has returned.
func newWatchedRelayTestServer(t *testing.T, r *relay.Relay, peerTimeout time.Duration) (*httptest.Server, *Server, *auth.JWTConfig, <-chan string) {
	t.Helper()
	return newWatchedRelayTestServerWithPing(t, r, peerTimeout, 0)
}

// newWatchedRelayTestServerWithPing is newWatchedRelayTestServer with an
// explicit liveness budget, so the ping is provable in milliseconds.
func newWatchedRelayTestServerWithPing(t *testing.T, r *relay.Relay, peerTimeout, pingInterval time.Duration) (*httptest.Server, *Server, *auth.JWTConfig, <-chan string) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	serverCfg := ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Sites:          testutil.NewTestSites(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Relay:          r,
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	}
	serverCfg.RelayPeerTimeout = peerTimeout
	serverCfg.RelayPingInterval = pingInterval
	srv := NewServer(serverCfg)

	watch := &relayHandlerWatch{inner: srv, returned: make(chan string, 256)}
	ts := httptest.NewServer(watch)
	t.Cleanup(ts.Close)
	return ts, srv, cfg, watch.returned
}

func relayTestCtx(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

func dialWS(t *testing.T, ctx context.Context, serverURL, path string, headers http.Header) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + path
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	require.NoError(t, err)
	return conn
}

func bearerHeader(jwtToken string) http.Header {
	return http.Header{"Authorization": {testBearerPrefix + jwtToken}}
}

func dialRelayPair(t *testing.T, ctx context.Context, serverURL, token, jwtToken string) (agent, browser *websocket.Conn) {
	t.Helper()
	agent = dialWS(t, ctx, serverURL, testPathWSRelay+token+testSideAgent, nil)
	browser = dialWS(t, ctx, serverURL, testPathWSRelay+token+testSideBrowser, bearerHeader(jwtToken))
	return agent, browser
}

func awaitBothHandlers(t *testing.T, returned <-chan string, failure string) map[string]bool {
	t.Helper()
	sides := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for len(sides) < 2 {
		select {
		case side := <-returned:
			sides[side] = true
		case <-deadline:
			t.Fatalf("%s; returned %v", failure, sides)
		}
	}
	return sides
}

// waitForRelayWired blocks until both agent and browser sides have registered
// with the relay and piping has started. Replaces fixed `time.Sleep` waits.
func waitForRelayWired(t *testing.T, ctx context.Context, srv *Server, token protocol.SessionToken) {
	t.Helper()
	require.Eventually(t, func() bool {
		waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		return srv.relay.WaitForPeer(waitCtx, token) == nil
	}, 3*time.Second, 25*time.Millisecond, "relay should wire both sides of session %s", token)
}

// seedRelaySession seeds a user, site, device and agent session and returns the session token
// plus a browser JWT for that user.
func seedRelaySession(t *testing.T, ctx context.Context, srv *Server, cfg *auth.JWTConfig) (token, jwtToken string) {
	t.Helper()
	ctx = dbtx.WithDefaultTenant(ctx, true)
	user := testutil.SeedUser(t, ctx, srv.store)
	site := testutil.SeedSite(t, ctx, srv.store)
	device := testutil.SeedDevice(t, ctx, srv.store, site.ID)
	sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)
	jwt, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.TenantID)
	require.NoError(t, err)
	return sess.Token, jwt
}

// forgedJWT returns a structurally valid token signed with a secret the server
// does not know, so accepting it would prove the signature is never verified.
func forgedJWT(t *testing.T) string {
	t.Helper()
	attacker := &auth.JWTConfig{
		Secret:   "an-attacker-controlled-secret-key-32b",
		Issuer:   "opengate",
		Duration: time.Hour,
	}
	token, err := attacker.GenerateToken(uuid.New(), "attacker@example.com", true, uuid.New())
	require.NoError(t, err)
	return token
}

// assertBrowserRejected dials the browser side of a seeded relay session and asserts the
// server closed it with a policy violation, since an idle accepted connection also fails to read.
func assertBrowserRejected(t *testing.T, query string, bearer ...string) {
	t.Helper()
	ts, srv, cfg := newRelayTestServer(t)
	ctx := relayTestCtx(t, 5*time.Second)

	token, _ := seedRelaySession(t, ctx, srv, cfg)

	var headers http.Header
	if len(bearer) > 0 {
		headers = http.Header{"Authorization": bearer}
	}
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + testPathWSRelay + token + query
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return // rejected at handshake
	}
	defer conn.Close(websocket.StatusPolicyViolation, "")

	_, _, err = conn.Read(ctx)
	assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err),
		"browser side without a valid JWT must be closed with a policy violation, got %v", err)
}

func TestRelayWebSocket(t *testing.T) {
	t.Parallel()
	t.Run("token_not_in_db", func(t *testing.T) {
		ts, _, _ := newRelayTestServer(t)
		ctx := relayTestCtx(t, 5*time.Second)

		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + testPathWSRelay + "nonexistent" + testSideAgent
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			// Connection might fail during close frame
			return
		}
		// If connected, expect a close frame
		_, _, err = conn.Read(ctx)
		assert.Error(t, err)
	})

	t.Run("invalid_side_param", func(t *testing.T) {
		ts, srv, cfg := newRelayTestServer(t)
		ctx := relayTestCtx(t, 5*time.Second)

		token, _ := seedRelaySession(t, ctx, srv, cfg)

		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + testPathWSRelay + token + "?side=invalid"
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			return
		}
		_, _, err = conn.Read(ctx)
		assert.Error(t, err)
	})

	t.Run("both_sides_connect_data_flows", func(t *testing.T) {
		ts, srv, cfg := newRelayTestServer(t)
		ctx := relayTestCtx(t, 10*time.Second)

		token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

		agentConn, browserConn := dialRelayPair(t, ctx, ts.URL, token, jwtToken)
		defer agentConn.Close(websocket.StatusNormalClosure, "")
		defer browserConn.Close(websocket.StatusNormalClosure, "")

		// Wait for relay pipe to start (both sides registered).
		waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))

		// Agent sends "hello" → browser receives it
		err := agentConn.Write(ctx, websocket.MessageBinary, []byte("hello"))
		require.NoError(t, err)

		_, data, err := browserConn.Read(ctx)
		require.NoError(t, err)
		assert.Equal(t, []byte("hello"), data)

		// Browser sends "world" → agent receives it
		err = browserConn.Write(ctx, websocket.MessageBinary, []byte("world"))
		require.NoError(t, err)

		_, data, err = agentConn.Read(ctx)
		require.NoError(t, err)
		assert.Equal(t, []byte("world"), data)
	})

	t.Run("disconnect_closes_peer", func(t *testing.T) {
		ts, srv, cfg := newRelayTestServer(t)
		ctx := relayTestCtx(t, 10*time.Second)

		token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

		agentConn, browserConn := dialRelayPair(t, ctx, ts.URL, token, jwtToken)

		waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))

		// Disconnect agent
		agentConn.Close(websocket.StatusNormalClosure, "bye")

		// Browser should get an error on read within reasonable time
		readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
		defer readCancel()
		_, _, err := browserConn.Read(readCtx)
		assert.Error(t, err)
	})

	// A relay token alone cannot attach as the operator side: the browser must present a valid JWT.
	t.Run("browser_rejects_unverifiable_credentials", func(t *testing.T) {
		forged := forgedJWT(t)
		cases := map[string]string{
			"no credential":  "",
			"garbage":        "not-a-jwt",
			"forged jwt":     forged,
			"expired-format": "a.b.c",
		}
		for name, credential := range cases {
			t.Run(name, func(t *testing.T) {
				assertBrowserRejected(t, "?side=browser&auth="+credential)
			})
		}
		// The header transport must be judged identically to the query param.
		t.Run("forged bearer header", func(t *testing.T) {
			assertBrowserRejected(t, testSideBrowser, testBearerPrefix+forged)
		})
	})

	t.Run("browser_auth_via_query_param", func(t *testing.T) {
		ts, srv, cfg := newRelayTestServer(t)
		ctx := relayTestCtx(t, 10*time.Second)

		token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

		// Agent connects
		agentConn := dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideAgent, nil)
		defer agentConn.Close(websocket.StatusNormalClosure, "")

		// Browser connects with JWT via query param (no Authorization header)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+token+"?side=browser&auth="+jwtToken, nil)
		defer browserConn.Close(websocket.StatusNormalClosure, "")

		waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))

		// Verify data flows
		err := agentConn.Write(ctx, websocket.MessageBinary, []byte("from-agent"))
		require.NoError(t, err)

		_, data, err := browserConn.Read(ctx)
		require.NoError(t, err)
		assert.Equal(t, []byte("from-agent"), data)
	})

	t.Run("browser_connects_waits", func(t *testing.T) {
		ts, srv, cfg := newRelayTestServer(t)
		tenantCtx := dbtx.WithDefaultTenant(context.Background(), true)

		user := testutil.SeedUser(t, tenantCtx, srv.store)
		site := testutil.SeedSite(t, tenantCtx, srv.store)
		device := testutil.SeedDevice(t, tenantCtx, srv.store, site.ID)

		token := protocol.GenerateSessionToken()
		require.NoError(t, srv.sessions.Create(tenantCtx, &session.Session{
			Token:    string(token),
			DeviceID: device.ID,
			UserID:   user.ID,
		}))

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.TenantID)
		require.NoError(t, err)

		// Browser connects — should block waiting for peer
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+string(token)+testSideBrowser, bearerHeader(jwtToken))
		defer browserConn.Close(websocket.StatusNormalClosure, "")

		// Read should timeout since no agent is connecting
		readCtx, readCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer readCancel()
		_, _, err = browserConn.Read(readCtx)
		assert.Error(t, err) // context deadline exceeded
	})

	t.Run("browser_only_session_times_out_and_is_released", func(t *testing.T) {
		agentRelay := relay.NewRelay(slog.Default())
		ts, srv, cfg := newRelayTestServerWithPeerTimeout(t, agentRelay, 200*time.Millisecond)
		require.Equal(t, 200*time.Millisecond, srv.peerWaitTimeout)
		tenantCtx := dbtx.WithDefaultTenant(context.Background(), true)
		token, jwtToken := seedRelaySession(t, tenantCtx, srv, cfg)
		cleanup := make(chan error, 1)
		agentRelay.OnSessionEnd = func(ended protocol.SessionToken) {
			cleanup <- srv.sessions.DeleteRelaySession(context.Background(), string(ended))
		}

		browserConn := dialWS(t, context.Background(), ts.URL, testPathWSRelay+token+testSideBrowser, bearerHeader(jwtToken))
		defer browserConn.CloseNow()

		require.Eventually(t, func() bool {
			return agentRelay.ActiveSessionCount() == 1
		}, time.Second, 10*time.Millisecond)

		select {
		case err := <-cleanup:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("browser-only relay was not released after the peer timeout")
		}

		assert.Equal(t, 0, agentRelay.ActiveSessionCount())
		assert.Empty(t, agentRelay.ActiveTokens())
		_, err := srv.sessions.Get(tenantCtx, token)
		assert.ErrorIs(t, err, session.ErrSessionNotFound)
	})
}

// seedRelayTokenFor issues one more relay session row against an already-seeded user and device.
func seedRelayTokenFor(t *testing.T, ctx context.Context, srv *Server, deviceID, userID uuid.UUID) string {
	t.Helper()
	return testutil.SeedAgentSession(t, dbtx.WithDefaultTenant(ctx, true), srv.store, deviceID, userID).Token
}

// runRelaySession opens both sides of one relay session, proves data flows, and hangs up.
func runRelaySession(t *testing.T, ctx context.Context, ts *httptest.Server, srv *Server, token, jwtToken string) {
	t.Helper()
	agentConn, browserConn := dialRelayPair(t, ctx, ts.URL, token, jwtToken)

	waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))

	require.NoError(t, agentConn.Write(ctx, websocket.MessageBinary, []byte("payload")))
	_, data, err := browserConn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), data)

	agentConn.Close(websocket.StatusNormalClosure, "done")
	browserConn.Close(websocket.StatusNormalClosure, "done")
}

func TestRelayWebSocket_HandlerReturnsWhenSessionEnds(t *testing.T) {
	agentRelay := relay.NewRelay(slog.Default())
	ts, srv, cfg, returned := newWatchedRelayTestServer(t, agentRelay, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, jwtToken := seedRelaySession(t, ctx, srv, cfg)
	runRelaySession(t, ctx, ts, srv, token, jwtToken)

	sides := awaitBothHandlers(t, returned, "relay handlers did not return after the session ended")
	assert.True(t, sides["agent"], "the machine side's handler must return")
	assert.True(t, sides["browser"], "the operator side's handler must return")
}

// relayLeakPoints are the cumulative completed-session counts the slope is fitted through.
var relayLeakPoints = []int{4, 8, 16}

// relayLeakSlopeTolerance is the goroutines per completed session the fit may carry; the
// defect retained 2.05.
const relayLeakSlopeTolerance = 0.5

func TestRelayWebSocket_CompletedSessionsRetainNoGoroutines(t *testing.T) {
	agentRelay := relay.NewRelay(slog.Default())
	ts, srv, cfg := newRelayTestServerWith(t, agentRelay)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tenantCtx := dbtx.WithDefaultTenant(ctx, true)
	user := testutil.SeedUser(t, tenantCtx, srv.store)
	site := testutil.SeedSite(t, tenantCtx, srv.store)
	dev := testutil.SeedDevice(t, tenantCtx, srv.store, site.ID)
	jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.TenantID)
	require.NoError(t, err)

	// Warm the paths every session shares — the first dial compiles queries and
	// grows the pool, and charging that to session one would tilt the fit.
	runRelaySession(t, ctx, ts, srv, seedRelayTokenFor(t, ctx, srv, dev.ID, user.ID), jwtToken)
	settleGoroutines(t, agentRelay)

	var xs, ys []float64
	completed := 0
	for _, point := range relayLeakPoints {
		for ; completed < point; completed++ {
			runRelaySession(t, ctx, ts, srv, seedRelayTokenFor(t, ctx, srv, dev.ID, user.ID), jwtToken)
		}
		xs = append(xs, float64(point))
		ys = append(ys, float64(settleGoroutines(t, agentRelay)))
	}

	slope := leastSquaresSlope(xs, ys)
	t.Logf("completed sessions %v → retained goroutines %v (slope %.3f/session)", xs, ys, slope)
	assert.LessOrEqualf(t, math.Abs(slope), relayLeakSlopeTolerance,
		"a completed relay session must give back its goroutines: %.3f retained per session across %v", slope, xs)
}

// settleGoroutines waits for the relay to book every session out and the goroutine count to
// stop falling, because teardown is asynchronous, then returns the count.
func settleGoroutines(t *testing.T, r *relay.Relay) int {
	t.Helper()
	require.Eventually(t, func() bool { return r.ActiveSessionCount() == 0 },
		10*time.Second, 20*time.Millisecond, "the relay must book out every finished session")

	last := runtime.NumGoroutine()
	stable := 0
	for i := 0; i < 200; i++ {
		time.Sleep(25 * time.Millisecond)
		runtime.GC()
		now := runtime.NumGoroutine()
		if now >= last {
			stable++
			if stable == 8 {
				return now
			}
		} else {
			stable = 0
		}
		last = now
	}
	return last
}

// leastSquaresSlope fits y = a + bx and returns b, or zero for fewer than two distinct x values.
func leastSquaresSlope(xs, ys []float64) float64 {
	n := float64(len(xs))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, x := range xs {
		sumX += x
		sumY += ys[i]
		sumXY += x * ys[i]
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func TestRelayWebSocket_StalledPeerEndsTheSession(t *testing.T) {
	agentRelay := relay.NewRelay(slog.Default())
	ts, srv, cfg, returned := newWatchedRelayTestServerWithPing(t, agentRelay, 0, 200*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

	agentConn, browserConn := dialRelayPair(t, ctx, ts.URL, token, jwtToken)
	defer agentConn.CloseNow()
	defer browserConn.CloseNow()
	waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))

	// The machine side stays healthy: its read loop answers the control frame,
	// which is what a real agent and a real browser both do.
	go func() {
		for {
			if _, _, err := agentConn.Read(ctx); err != nil {
				return
			}
		}
	}()

	// The operator side never reads, so it never answers. Nothing is written to
	// it, so this is a peer that looks perfectly healthy at the socket.
	require.Eventually(t, func() bool { return agentRelay.ActiveSessionCount() == 0 },
		10*time.Second, 20*time.Millisecond,
		"a peer that stops answering must end the session within the ping budget")

	awaitBothHandlers(t, returned, "both handlers must return when the session ends")
}
