package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"nhooyr.io/websocket"
)

const (
	// proxyCallerHeader carries the dialing server's ID. It is required on every
	// cross-server proxy request: it identifies the caller and, because only a
	// peer relay sets it, marks the connection as already-proxied so the owner
	// registers it through RegisterLocal (the loop guard, ADR-033).
	proxyCallerHeader = "X-OpenGate-Proxy"
	// proxyAuthHeader carries the optional shared secret. When the owner is
	// configured with a non-empty secret, this header must match it
	// (constant-time) — defense-in-depth on top of the network boundary.
	proxyAuthHeader = "X-OpenGate-Proxy-Secret"
)

// InternalRelayServer handles cross-server proxy connections from peer servers.
// It is served on a private listener that is never exposed through the public
// ingress (ADR-033); its only authentication is the network boundary plus the
// loop-guard caller header and an optional shared secret.
type InternalRelayServer struct {
	relay    *relay.Relay
	serverID string
	secret   string // when non-empty, proxyAuthHeader must match
	logger   *slog.Logger
}

// NewInternalRelayServer builds the internal relay handler. secret may be empty
// to rely on network isolation alone.
func NewInternalRelayServer(r *relay.Relay, serverID, secret string, logger *slog.Logger) *InternalRelayServer {
	return &InternalRelayServer{relay: r, serverID: serverID, secret: secret, logger: logger}
}

// Handler returns the router for the internal listener.
func (s *InternalRelayServer) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/internal/relay/{token}", s.handleProxyConn)
	return r
}

// secretOK reports whether the request carries the configured shared secret.
// With no secret configured every request passes this check (network boundary
// only). The comparison is constant-time to avoid leaking the secret by timing.
func (s *InternalRelayServer) secretOK(r *http.Request) bool {
	if s.secret == "" {
		return true
	}
	got := r.Header.Get(proxyAuthHeader)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) == 1
}

// proxySide parses the ?side= param into a relay.Side and its label. Unlike the
// public handler it performs no browser-auth: the browser already authenticated
// on its entry server, and this listener is unreachable from outside the cluster.
func proxySide(r *http.Request) (relay.Side, string, bool) {
	switch r.URL.Query().Get("side") {
	case "agent":
		return relay.SideAgent, "agent", true
	case "browser":
		return relay.SideBrowser, "browser", true
	default:
		return 0, "", false
	}
}

// handleProxyConn validates the loop-guard header, secret, and side BEFORE
// upgrading, so a rejected peer sees a plain HTTP error rather than a WebSocket.
// On success it accepts the WS, wraps it, and registers it through RegisterLocal
// — which never re-proxies — then holds the connection open while the relay pipes.
func (s *InternalRelayServer) handleProxyConn(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	caller := r.Header.Get(proxyCallerHeader)
	if caller == "" {
		s.logger.Warn("internal relay missing caller header", "token_prefix", protocol.RedactToken(token))
		http.Error(w, "missing proxy caller", http.StatusBadRequest)
		return
	}
	if !s.secretOK(r) {
		s.logger.Warn("internal relay secret mismatch", "token_prefix", protocol.RedactToken(token), "caller", caller)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	side, label, ok := proxySide(r)
	if !ok {
		s.logger.Warn("internal relay invalid side", "token_prefix", protocol.RedactToken(token), "side_param", r.URL.Query().Get("side"))
		http.Error(w, "invalid side", http.StatusBadRequest)
		return
	}

	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.logger.Error("internal relay upgrade failed", "token_prefix", protocol.RedactToken(token), "error", err)
		return
	}

	s.logger.Info("internal relay proxied side connected", "token_prefix", protocol.RedactToken(token), "side", label, "caller", caller)
	ctx := r.Context()
	if err := s.relay.RegisterLocal(ctx, protocol.SessionToken(token), NewWSConn(wsConn, "peer:"+label), side); err != nil {
		s.logger.Error("internal relay register-local failed", "token_prefix", protocol.RedactToken(token), "side", label, "error", err)
		_ = wsConn.Close(websocket.StatusInternalError, "relay error")
		return
	}
	<-ctx.Done()
	s.logger.Info("internal relay proxied side disconnected", "token_prefix", protocol.RedactToken(token), "side", label)
}

// HTTPPeerDialer is the production relay.PeerDialer. It dials a session's
// affinity owner directly at the owner's address over the flat cluster overlay
// (ADR-033): pod IPs are routable container-to-container, so no DNS or headless
// Service is needed. All pods share the same internal port (homogeneous
// deployment), so the dialer reuses its own port to reach any peer.
type HTTPPeerDialer struct {
	serverID string // this server's ID, sent as the loop-guard caller header
	port     string // the internal listener port shared cluster-wide
	secret   string // optional shared secret
	logger   *slog.Logger
}

// NewHTTPPeerDialer builds the production dialer. port is the shared internal
// relay port; secret may be empty.
func NewHTTPPeerDialer(serverID, port, secret string, logger *slog.Logger) *HTTPPeerDialer {
	return &HTTPPeerDialer{serverID: serverID, port: port, secret: secret, logger: logger}
}

// Dial opens the cross-server tunnel to owner for token's session, forwarding
// the given side. The full token rides the URL path (private overlay only); only
// the redacted prefix is ever logged (ADR-027).
func (d *HTTPPeerDialer) Dial(ctx context.Context, owner string, token protocol.SessionToken, side relay.Side) (relay.Conn, error) {
	label := "agent"
	if side == relay.SideBrowser {
		label = "browser"
	}
	wsURL := fmt.Sprintf("ws://%s/internal/relay/%s?side=%s",
		net.JoinHostPort(owner, d.port), url.PathEscape(string(token)), label)

	header := http.Header{}
	header.Set(proxyCallerHeader, d.serverID)
	if d.secret != "" {
		header.Set(proxyAuthHeader, d.secret)
	}

	wsConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, fmt.Errorf("dial internal relay owner: %w", err)
	}
	d.logger.Info("dialed relay owner", "token_prefix", protocol.RedactToken(string(token)), "side", label)
	return NewWSConn(wsConn, "peer:"+label), nil
}
