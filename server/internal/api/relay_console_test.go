package api

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func newConsoleRelayServer(t *testing.T, recheck time.Duration) (*httptest.Server, *Server, *serverClock) {
	t.Helper()
	clock := &serverClock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	cfg := testServerConfig(t)
	cfg.Clock = clock.Now
	cfg.LiveSessionRecheck = recheck
	cfg.RelayPingInterval = 50 * time.Millisecond
	srv := NewServer(cfg)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, srv, clock
}

// consoleRelaySession seeds a relay session held by a user signed in to the console, and returns
// its token and the console's cookie.
func consoleRelaySession(t *testing.T, ctx context.Context, srv *Server) (string, *http.Cookie) {
	t.Helper()
	ctx = dbtx.WithDefaultTenant(ctx, true)
	user := testutil.SeedUser(t, ctx, srv.store)
	site := testutil.SeedSite(t, ctx, srv.store)
	device := testutil.SeedDevice(t, ctx, srv.store, site.ID)
	held := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)
	cookie, _, err := srv.startConsoleSession(ctx, user, false)
	require.NoError(t, err)
	return held.Token, cookie
}

func consoleHeaders(cookie *http.Cookie, origin string) http.Header {
	headers := http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	return headers
}

// dialRefused reports whether the browser side was turned away, at the handshake or right after.
func dialRefused(t *testing.T, ctx context.Context, url string, headers http.Header) bool {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return true
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(ctx)
	return websocket.CloseStatus(err) == websocket.StatusPolicyViolation
}

func wsURL(ts *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

// pairWithConsole opens the agent side and the browser side with the console's cookie.
func pairWithConsole(t *testing.T, ctx context.Context, ts *httptest.Server, srv *Server, token string,
	cookie *http.Cookie,
) (agent, browser *websocket.Conn) {
	t.Helper()
	agent = dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideAgent, nil)
	browser = dialWS(t, ctx, ts.URL, testPathWSRelay+token+testSideBrowser, consoleHeaders(cookie, ts.URL))
	waitForRelayWired(t, ctx, srv, protocol.SessionToken(token))
	return agent, browser
}

func TestALiveSessionOpensWithTheConsoleCookieAlone(t *testing.T) {
	ts, srv, _ := newConsoleRelayServer(t, 0)
	ctx := relayTestCtx(t, 10*time.Second)
	token, cookie := consoleRelaySession(t, ctx, srv)

	agent, browser := pairWithConsole(t, ctx, ts, srv, token, cookie)
	defer agent.CloseNow()
	defer browser.CloseNow()

	require.NoError(t, agent.Write(ctx, websocket.MessageBinary, []byte("screen")))
	_, data, err := browser.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, []byte("screen"), data)
}

func TestALiveSessionRefusesAForeignOrigin(t *testing.T) {
	ts, srv, _ := newConsoleRelayServer(t, 0)
	ctx := relayTestCtx(t, 10*time.Second)
	token, cookie := consoleRelaySession(t, ctx, srv)

	assert.True(t, dialRefused(t, ctx, wsURL(ts, testPathWSRelay+token+testSideBrowser),
		consoleHeaders(cookie, "https://pages.example.net")),
		"another site cannot take over a signed-in technician's live session")
}

func TestAPassInTheAddressOpensNoLiveSession(t *testing.T) {
	ts, srv, cfg := newRelayTestServer(t)
	ctx := relayTestCtx(t, 10*time.Second)
	token, jwtToken := seedRelaySession(t, ctx, srv, cfg)

	assert.True(t, dialRefused(t, ctx, wsURL(ts, testPathWSRelay+token+"?side=browser&auth="+jwtToken), nil),
		"the address carries no credential, since it lands in access logs")
}

// awaitClosed waits for the browser side to be closed by the server.
func awaitClosed(t *testing.T, ctx context.Context, browser *websocket.Conn) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		if _, _, err := browser.Read(readCtx); err != nil {
			require.NotErrorIs(t, err, context.DeadlineExceeded, "the live session stayed open")
			return
		}
	}
}

func TestASignedOutConsoleLosesItsLiveSession(t *testing.T) {
	ts, srv, _ := newConsoleRelayServer(t, 50*time.Millisecond)
	ctx := relayTestCtx(t, 10*time.Second)
	token, cookie := consoleRelaySession(t, ctx, srv)
	agent, browser := pairWithConsole(t, ctx, ts, srv, token, cookie)
	defer agent.CloseNow()
	defer browser.CloseNow()

	id, ok := srv.openSessionCookie(cookie.Value)
	require.True(t, ok)
	require.NoError(t, srv.consoleSessions.Delete(ctx, sha256.Sum256(id)))

	awaitClosed(t, ctx, browser)
}

func TestALiveSessionEndsAtTheConsoleSessionsTotal(t *testing.T) {
	ts, srv, clock := newConsoleRelayServer(t, 50*time.Millisecond)
	ctx := relayTestCtx(t, 10*time.Second)
	token, cookie := consoleRelaySession(t, ctx, srv)
	agent, browser := pairWithConsole(t, ctx, ts, srv, token, cookie)
	defer agent.CloseNow()
	defer browser.CloseNow()

	clock.advance(8 * time.Hour)
	awaitClosed(t, ctx, browser)
}

func TestAnIdleConsoleKeepsItsLiveSession(t *testing.T) {
	ts, srv, clock := newConsoleRelayServer(t, 50*time.Millisecond)
	ctx := relayTestCtx(t, 10*time.Second)
	token, cookie := consoleRelaySession(t, ctx, srv)
	agent, browser := pairWithConsole(t, ctx, ts, srv, token, cookie)
	defer agent.CloseNow()
	defer browser.CloseNow()

	// Both ends keep reading, as a browser and an agent do, which answers the server's pings.
	agent.CloseRead(ctx)
	received := make(chan []byte, 1)
	go func() {
		for {
			_, data, err := browser.Read(ctx)
			if err != nil {
				close(received)
				return
			}
			received <- data
		}
	}()

	// A long repair: the console sends nothing for 35 minutes while the technician works.
	for range 7 {
		clock.advance(5 * time.Minute)
		time.Sleep(150 * time.Millisecond)
	}

	require.NoError(t, agent.Write(ctx, websocket.MessageBinary, []byte("still here")))
	data, open := <-received
	require.True(t, open, "the live session stayed open")
	assert.Equal(t, []byte("still here"), data)

	console := console{srv: srv, cookie: cookie}
	assert.Equal(t, http.StatusOK, console.do(http.MethodGet, "/api/v1/users/me", nil, false).Code,
		"a technician at work in a live session is not idle, so the console is still signed in")
}
