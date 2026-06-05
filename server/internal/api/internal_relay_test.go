package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"nhooyr.io/websocket"
)

const testInternalServerID = "server-A"

func internalTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// testCtx returns a context bounded by d, cancelled on test cleanup.
func testCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// newInternalTestServer starts an httptest server fronting the internal relay
// handler, backed by r. secret is the shared proxy secret ("" disables it).
func newInternalTestServer(t *testing.T, r *relay.Relay, secret string) *httptest.Server {
	t.Helper()
	h := NewInternalRelayServer(r, testInternalServerID, secret, internalTestLogger())
	ts := httptest.NewServer(h.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// proxyHeaders builds the loop-guard caller header plus the optional secret.
func proxyHeaders(caller, secret string) http.Header {
	h := http.Header{}
	h.Set(proxyCallerHeader, caller)
	if secret != "" {
		h.Set(proxyAuthHeader, secret)
	}
	return h
}

// dialInternal dials the internal relay endpoint for token/side with headers.
func dialInternal(ctx context.Context, serverURL, token, side string, headers http.Header) (*websocket.Conn, error) {
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/internal/relay/" + token + "?side=" + side
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	return conn, err
}

// awaitPaired blocks until the relay has wired both sides of token.
func awaitPaired(t *testing.T, ctx context.Context, r *relay.Relay, token protocol.SessionToken) {
	t.Helper()
	require.Eventually(t, func() bool {
		wc, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		return r.WaitForPeer(wc, token) == nil
	}, 3*time.Second, 25*time.Millisecond)
}

func TestInternalRelay_RejectsMissingCallerHeader(t *testing.T) {
	r := relay.NewRelay(internalTestLogger())
	ts := newInternalTestServer(t, r, "")

	// No X-OpenGate-Proxy header → handshake rejected before upgrade.
	_, err := dialInternal(testCtx(t, 5*time.Second), ts.URL, string(protocol.GenerateSessionToken()), "agent", http.Header{})
	require.Error(t, err)
}

func TestInternalRelay_RejectsInvalidSide(t *testing.T) {
	r := relay.NewRelay(internalTestLogger())
	ts := newInternalTestServer(t, r, "")

	_, err := dialInternal(testCtx(t, 5*time.Second), ts.URL, string(protocol.GenerateSessionToken()), "sideways",
		proxyHeaders(testInternalServerID, ""))
	require.Error(t, err)
}

func TestInternalRelay_SecretRequiredWhenSet(t *testing.T) {
	const secret = "proxy-shared-secret"
	r := relay.NewRelay(internalTestLogger())
	ts := newInternalTestServer(t, r, secret)
	ctx := testCtx(t, 5*time.Second)
	token := string(protocol.GenerateSessionToken())

	// Missing secret → rejected.
	_, err := dialInternal(ctx, ts.URL, token, "agent", proxyHeaders(testInternalServerID, ""))
	require.Error(t, err, "missing secret must be rejected")

	// Wrong secret → rejected.
	_, err = dialInternal(ctx, ts.URL, token, "agent", proxyHeaders(testInternalServerID, "wrong"))
	require.Error(t, err, "wrong secret must be rejected")
}

// TestInternalRelay_PairsViaRegisterLocal proves the endpoint registers proxied
// sides through RegisterLocal so two proxied conns pair and data flows — and the
// PeerDialer is never consulted (loop guard).
func TestInternalRelay_PairsViaRegisterLocal(t *testing.T) {
	r := relay.NewRelay(internalTestLogger())
	ts := newInternalTestServer(t, r, "")
	ctx := testCtx(t, 10*time.Second)
	token := string(protocol.GenerateSessionToken())

	agentConn, err := dialInternal(ctx, ts.URL, token, "agent", proxyHeaders("server-B", ""))
	require.NoError(t, err)
	defer agentConn.Close(websocket.StatusNormalClosure, "")

	browserConn, err := dialInternal(ctx, ts.URL, token, "browser", proxyHeaders("server-B", ""))
	require.NoError(t, err)
	defer browserConn.Close(websocket.StatusNormalClosure, "")

	awaitPaired(t, ctx, r, protocol.SessionToken(token))

	require.NoError(t, agentConn.Write(ctx, websocket.MessageBinary, []byte("proxied hello")))
	_, data, err := browserConn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, []byte("proxied hello"), data)
}

// TestInternalRelay_HalfOpenClosesConn asserts a proxied side with no local peer
// is closed once affinityTTL elapses (stale-affinity teardown).
func TestInternalRelay_HalfOpenClosesConn(t *testing.T) {
	r := relay.NewRelay(internalTestLogger(), relay.WithAffinityTTL(100*time.Millisecond))
	ts := newInternalTestServer(t, r, "")
	ctx := testCtx(t, 5*time.Second)

	conn, err := dialInternal(ctx, ts.URL, string(protocol.GenerateSessionToken()), "agent",
		proxyHeaders("server-B", ""))
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	_, _, err = conn.Read(readCtx)
	assert.Error(t, err, "half-open proxied conn should be closed after affinityTTL")
}

// hostPort splits an httptest URL into its host and port.
func hostPort(t *testing.T, serverURL string) (host, port string) {
	t.Helper()
	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	return u.Hostname(), u.Port()
}

// TestHTTPPeerDialer_DialsOwner proves the production dialer connects to the
// internal endpoint with the loop-guard header and yields a working relay.Conn
// that pairs with a peer side.
func TestHTTPPeerDialer_DialsOwner(t *testing.T) {
	r := relay.NewRelay(internalTestLogger())
	const secret = "dial-secret"
	ts := newInternalTestServer(t, r, secret)
	ctx := testCtx(t, 10*time.Second)
	token := protocol.GenerateSessionToken()

	host, port := hostPort(t, ts.URL)
	dialer := NewHTTPPeerDialer("server-B", port, secret, internalTestLogger())

	// Dial the agent side through the production dialer.
	agentConn, err := dialer.Dial(ctx, host, token, relay.SideAgent)
	require.NoError(t, err)
	defer agentConn.Close()

	// Browser side dials directly; the two pair via RegisterLocal on the owner.
	browserConn, err := dialInternal(ctx, ts.URL, string(token), "browser", proxyHeaders("server-B", secret))
	require.NoError(t, err)
	defer browserConn.Close(websocket.StatusNormalClosure, "")

	awaitPaired(t, ctx, r, token)

	require.NoError(t, agentConn.WriteMessage([]byte("via dialer")))
	_, data, err := browserConn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, []byte("via dialer"), data)
}

// TestHTTPPeerDialer_ErrorOnUnreachable asserts a dial to a dead address fails
// fast rather than blocking.
func TestHTTPPeerDialer_ErrorOnUnreachable(t *testing.T) {
	dialer := NewHTTPPeerDialer("server-B", "1", "", internalTestLogger())

	_, err := dialer.Dial(testCtx(t, 2*time.Second), "127.0.0.1", protocol.GenerateSessionToken(), relay.SideAgent)
	require.Error(t, err)
}
