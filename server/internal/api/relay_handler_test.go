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
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
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
	store := testutil.NewTestStore(t)
	r := relay.NewRelay(slog.Default())
	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:    store,
		JWT:      cfg,
		Agents:   &stubAgentGetter{},
		AMT:      &stubAMTOperator{},
		Relay:    r,
		Notifier: &notifications.NoopNotifier{},
		Logger:   logger,
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

func TestRelayWebSocket(t *testing.T) {
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
		ts, srv, _ := newRelayTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Seed a valid session
		user := testutil.SeedUser(t, ctx, srv.store)
		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + testPathWSRelay + sess.Token + "?side=invalid"
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

		user := testutil.SeedUser(t, ctx, srv.store)
		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		// Agent connects
		agentHeaders := http.Header{}
		agentConn := dialWS(t, ctx, ts.URL, testPathWSRelay+sess.Token+testSideAgent, agentHeaders)
		defer agentConn.Close(websocket.StatusNormalClosure, "")

		// Browser connects with JWT
		browserHeaders := http.Header{}
		browserHeaders.Set("Authorization", testBearerPrefix+jwtToken)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+sess.Token+testSideBrowser, browserHeaders)
		defer browserConn.Close(websocket.StatusNormalClosure, "")

		// Wait for relay to start piping
		time.Sleep(100 * time.Millisecond)

		// Agent sends "hello" → browser receives it
		err = agentConn.Write(ctx, websocket.MessageBinary, []byte("hello"))
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

		user := testutil.SeedUser(t, ctx, srv.store)
		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		agentConn := dialWS(t, ctx, ts.URL, testPathWSRelay+sess.Token+testSideAgent, nil)

		browserHeaders := http.Header{}
		browserHeaders.Set("Authorization", testBearerPrefix+jwtToken)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+sess.Token+testSideBrowser, browserHeaders)

		// Wait for relay to connect
		time.Sleep(100 * time.Millisecond)

		// Disconnect agent
		agentConn.Close(websocket.StatusNormalClosure, "bye")

		// Browser should get an error on read within reasonable time
		readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
		defer readCancel()
		_, _, err = browserConn.Read(readCtx)
		assert.Error(t, err)
	})

	t.Run("browser_auth_via_query_param", func(t *testing.T) {
		ts, srv, cfg := newRelayTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		user := testutil.SeedUser(t, ctx, srv.store)
		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		// Agent connects
		agentConn := dialWS(t, ctx, ts.URL, testPathWSRelay+sess.Token+testSideAgent, nil)
		defer agentConn.Close(websocket.StatusNormalClosure, "")

		// Browser connects with JWT via query param (no Authorization header)
		browserConn := dialWS(t, ctx, ts.URL, testPathWSRelay+sess.Token+"?side=browser&auth="+jwtToken, nil)
		defer browserConn.Close(websocket.StatusNormalClosure, "")

		time.Sleep(100 * time.Millisecond)

		// Verify data flows
		err = agentConn.Write(ctx, websocket.MessageBinary, []byte("from-agent"))
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
		require.NoError(t, srv.store.CreateAgentSession(context.Background(), &db.AgentSession{
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
