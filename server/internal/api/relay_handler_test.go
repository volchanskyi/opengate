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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
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
	store := testutil.NewTestStore(t)
	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		DeviceLogs:     testutil.NewTestLogs(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		AMTDevices:     testutil.NewTestAMTDevices(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Relay:          r,
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	})

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
	user := testutil.SeedUser(t, ctx, srv.store)
	group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
	device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
	sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)
	jwt, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)
	return sess.Token, jwt
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

		user := testutil.SeedUser(t, context.Background(), srv.store)
		group := testutil.SeedGroup(t, context.Background(), srv.store, user.ID)
		device := testutil.SeedDevice(t, context.Background(), srv.store, group.ID)

		token := protocol.GenerateSessionToken()
		require.NoError(t, srv.sessions.Create(context.Background(), &session.Session{
			Token:    string(token),
			DeviceID: device.ID,
			UserID:   user.ID,
		}))

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
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
}
