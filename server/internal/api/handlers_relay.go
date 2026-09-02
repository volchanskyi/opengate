package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
	"nhooyr.io/websocket"
)

// defaultRelayPeerTimeout bounds half-open relay entries. Pairing normally
// completes in milliseconds; after this window a missing peer cannot leave a
// session row and relay token alive indefinitely.
const defaultRelayPeerTimeout = 30 * time.Second

// defaultRelayPingInterval is how often a parked relay handler asks its peer to
// answer a control frame, and the budget the answer must arrive inside.
//
// A paired session is deliberately not time-limited — a technician may hold one
// open for an hour, and a duration cap would make that a defect. So liveness is
// proved rather than assumed. The ping catches both the peer that vanished and
// the peer that is present and no longer consuming: the frame carries its own
// deadline, and the pong has to come back. One unanswered ping ends the session;
// a peer that cannot answer inside a whole interval is not answering.
const defaultRelayPingInterval = 20 * time.Second

// sideLabel names a relay side for logs and for the connection wrapper.
func sideLabel(side relay.Side) string {
	if side == relay.SideBrowser {
		return "browser"
	}
	return "agent"
}

// rejectWebSocket accepts the WebSocket handshake and immediately closes the
// connection with a policy-violation status code carrying the given reason.
func rejectWebSocket(w http.ResponseWriter, r *http.Request, reason string) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	_ = c.Close(websocket.StatusPolicyViolation, reason)
}

// bearerToken extracts the credential from an "Authorization: Bearer <token>"
// header, returning "" when the header is absent or malformed.
func bearerToken(header string) string {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// authenticateBrowser verifies the browser side presents a JWT this server
// signed, and that the session it is joining is visible in that caller's
// tenant. The browser WebSocket API cannot set custom headers, so the token may
// arrive in the ?auth= query parameter as well as in the Authorization header.
//
// The relay token in the URL establishes *which* session is being joined; this
// establishes *who* is joining it. Without it, a relay token that leaked
// through browser history, a referrer, or a shared link would by itself be
// enough to attach an operator console to somebody's remote session.
func (s *Server) authenticateBrowser(r *http.Request, sessionToken string) bool {
	credential := bearerToken(r.Header.Get("Authorization"))
	if credential == "" {
		credential = r.URL.Query().Get("auth")
	}
	if credential == "" {
		return false
	}

	claims, err := s.jwt.ValidateToken(credential)
	if err != nil {
		return false
	}

	// Resolve the session under the caller's own tenant scope so a relay token
	// cannot be used across tenants.
	scoped := dbtx.WithTenant(r.Context(), claims.TenantID, claims.IsAdmin)
	if _, err := s.sessions.Get(scoped, sessionToken); err != nil {
		return false
	}
	return true
}

// parseSide determines the relay side from the ?side= query param.
// Returns the side and true on success, or rejects the WebSocket and returns false.
func (s *Server) parseSide(w http.ResponseWriter, r *http.Request, sessionToken string) (relay.Side, bool) {
	switch r.URL.Query().Get("side") {
	case "browser":
		if !s.authenticateBrowser(r, sessionToken) {
			rejectWebSocket(w, r, "browser side requires authorization")
			return 0, false
		}
		return relay.SideBrowser, true
	case "agent":
		return relay.SideAgent, true
	default:
		rejectWebSocket(w, r, "invalid side")
		return 0, false
	}
}

// upgradeRelayWebSocket upgrades the HTTP connection to WebSocket.
// On failure it writes an HTTP error response and returns nil.
func (s *Server) upgradeRelayWebSocket(w http.ResponseWriter, r *http.Request) *websocket.Conn {
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Error("relay websocket upgrade failed", "error", err)
		return nil
	}
	return wsConn
}

// registerAndWait registers conn with the relay, waits for the peer, and parks
// until the relay says the session is over. It closes wsConn on registration
// failure.
//
// The deferred release covers the side that connects and leaves while its peer
// never arrives: no pipe runs for that session, so without it the relay entry,
// the active-session count and the session row would all outlive the connection.
// It is inert once the pair started piping — that teardown belongs to the pipe.
func (s *Server) registerAndWait(r *http.Request, wsConn *websocket.Conn, conn relay.Conn, token string, side relay.Side) {
	ctx := r.Context()

	sessionDone, err := s.relay.Register(ctx, protocol.SessionToken(token), conn, side)
	if err != nil {
		s.logger.Error("relay register failed", "error", err, "token_prefix", protocol.RedactToken(token))
		_ = wsConn.Close(websocket.StatusInternalError, "relay error")
		return
	}
	// websocket.Accept hijacked this connection, so net/http will not close it
	// when the handler returns — this is the only thing that can. It is
	// registered first so it runs last, after the relay's own teardown has had
	// its graceful close and a peer still listening has seen a normal closure.
	// CloseNow rather than Close: a graceful close waits on an acknowledgement,
	// and by this point either it has already been sent or the peer is gone.
	defer wsConn.CloseNow()
	defer s.relay.Unregister(protocol.SessionToken(token))

	peerCtx, cancelPeerWait := context.WithTimeout(ctx, s.peerWaitTimeout)
	err = s.relay.WaitForPeer(peerCtx, protocol.SessionToken(token))
	cancelPeerWait()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			s.logger.Warn("relay peer wait timed out", "token_prefix", protocol.RedactToken(token))
		} else {
			s.logger.Error("relay wait for peer failed", "error", err, "token_prefix", protocol.RedactToken(token))
		}
		return
	}

	// Park until the relay ends the session, or until the process is shutting
	// down, proving the peer is still there in the meantime. The request context
	// is deliberately not a branch here: the connection was hijacked at Accept,
	// which untracks it, so nothing cancels that context — not a client hangup
	// and not Server.Shutdown. Selecting on it would encode a belief that either
	// is handled.
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sessionDone:
			return
		case <-s.lifetime.Done():
			return
		case <-ticker.C:
			// A background parent, not the lifetime: a shutdown must not be
			// reported as a peer that stopped answering.
			pingCtx, cancelPing := context.WithTimeout(context.Background(), s.pingInterval)
			err := wsConn.Ping(pingCtx)
			cancelPing()
			if err == nil {
				continue
			}
			// Returning runs the deferred CloseNow, which errors the relay's
			// read on this side and so ends the session for both.
			s.logger.Warn("relay peer did not answer a ping",
				"token_prefix", protocol.RedactToken(token), "side", sideLabel(side), "error", err)
			return
		}
	}
}

// handleRelayWebSocket authenticates the side, upgrades the connection and hands
// it to the relay.
//
// token_prefix is redacted inline at each call site rather than through a local,
// the same convention the relay package follows, so the full token never reaches
// logs and the static gate can see that at every site.
func (s *Server) handleRelayWebSocket(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	if err := s.validateRelayToken(r, token); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			s.logger.Warn("relay token not found", "token_prefix", protocol.RedactToken(token))
			rejectWebSocket(w, r, "session not found")
		} else {
			s.logger.Error("relay token validation error", "token_prefix", protocol.RedactToken(token), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	side, ok := s.parseSide(w, r, token)
	if !ok {
		s.logger.Warn("relay invalid side param",
			"token_prefix", protocol.RedactToken(token), "side_param", r.URL.Query().Get("side"))
		return
	}

	label := sideLabel(side)

	wsConn := s.upgradeRelayWebSocket(w, r)
	if wsConn == nil {
		return
	}

	s.logger.Info("relay session connected", "token_prefix", protocol.RedactToken(token), "side", label)
	s.registerAndWait(r, wsConn, NewWSConn(wsConn, label), token, side)
	s.logger.Info("relay session disconnected", "token_prefix", protocol.RedactToken(token), "side", label)
}

// validateRelayToken checks that the given token exists in the agent session store.
func (s *Server) validateRelayToken(r *http.Request, token string) error {
	ctx := r.Context()
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		ctx = dbtx.WithDefaultTenant(ctx, true)
	}
	_, err := s.sessions.Get(ctx, token)
	return err
}
