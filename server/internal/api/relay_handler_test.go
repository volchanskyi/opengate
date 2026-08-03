package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
		Groups:         testutil.NewTestGroups(t, store),
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
	srv := NewServer(serverCfg)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, srv, cfg
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

// seedRelaySession seeds a user → group → device → agent session and returns the
// session token plus a browser JWT for that user — the common fixture for the
// relay WebSocket subtests.
func seedRelaySession(t *testing.T, ctx context.Context, srv *Server, cfg *auth.JWTConfig) (token, jwtToken string) {
	t.Helper()
	ctx = dbtx.WithDefaultTenant(ctx, true)
	user := testutil.SeedUser(t, ctx, srv.store)
	group := testutil.SeedGroup(t, ctx, srv.store)
	device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
	sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)
	jwt, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.OrgID)
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

// assertBrowserRejected dials the browser side of a freshly seeded relay
// session with the given query and optional bearer header, and asserts the
// server closed it with an explicit policy violation.
//
// The close status is the assertion, not "Read returned an error": a connection
// that was accepted and merely idled out waiting for a peer also fails to read,
// so only the status tells the two apart.
func assertBrowserRejected(t *testing.T, query string, bearer ...string) {
	t.Helper()
	ts, srv, cfg := newRelayTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

		// Agent connects
		agentHeaders := http.Header{}
		agentConn := dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideAgent, agentHeaders)
		defer agentConn.Close(websocket.StatusNormalClosure, "")

		// Browser connects with JWT
		browserHeaders := http.Header{}
		browserHeaders.Set("Authorization", testBearerPrefix+jwtToken)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideBrowser, browserHeaders)
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

		agentConn := dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideAgent, nil)

		browserHeaders := http.Header{}
		browserHeaders.Set("Authorization", testBearerPrefix+jwtToken)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideBrowser, browserHeaders)

		// Wait for relay pipe to start (both sides registered).
		waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))

		// Disconnect agent
		agentConn.Close(websocket.StatusNormalClosure, "bye")

		// Browser should get an error on read within reasonable time
		readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
		defer readCancel()
		_, _, err := browserConn.Read(readCtx)
		assert.Error(t, err)
	})

	// The browser side must present a *valid* JWT, not merely some credential
	// shaped value. A relay token can leak through browser history, a referrer,
	// or a shared link; on its own it must not be enough to attach as the
	// operator side of somebody's remote session.
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

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
		group := testutil.SeedGroup(t, tenantCtx, srv.store)
		device := testutil.SeedDevice(t, tenantCtx, srv.store, group.ID)

		token := protocol.GenerateSessionToken()
		require.NoError(t, srv.sessions.Create(tenantCtx, &session.Session{
			Token:    string(token),
			DeviceID: device.ID,
			UserID:   user.ID,
		}))

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.OrgID)
		require.NoError(t, err)

		// Browser connects — should block waiting for peer
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		browserHeaders := http.Header{}
		browserHeaders.Set("Authorization", testBearerPrefix+jwtToken)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+string(token)+testSideBrowser, browserHeaders)
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

		browserHeaders := http.Header{}
		browserHeaders.Set("Authorization", testBearerPrefix+jwtToken)
		browserConn := dialWS(t, context.Background(), ts.URL, testPathWSRelay+token+testSideBrowser, browserHeaders)
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
