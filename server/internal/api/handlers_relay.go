package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
)

// defaultRelayPeerTimeout bounds half-open relay entries so a missing peer cannot keep a
// session row and relay token alive indefinitely.
const defaultRelayPeerTimeout = 30 * time.Second

// defaultRelayPingInterval is the ping cadence and pong budget of a parked relay handler; one
// unanswered ping ends the session, which has no duration cap.
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

// browserAuth is how the browser side proved who it is: ok when it did, and the console's
// session cookie when that is what it presented, so the live session can be re-checked.
type browserAuth struct {
	ok      bool
	console string
}

// authenticateBrowser requires the session's holder, by the console's session cookie or a
// server-signed bearer header, so a leaked relay token attaches nobody else.
func (s *Server) authenticateBrowser(r *http.Request, sessionToken string) browserAuth {
	var claims *auth.Claims
	var console string
	if bearer := bearerToken(r.Header.Get("Authorization")); bearer != "" {
		validated, err := s.jwt.ValidateToken(bearer)
		if err != nil {
			return browserAuth{}
		}
		claims = validated
	} else if cookie, err := r.Cookie(consoleCookieName); err == nil {
		session, err := s.liveConsoleSession(r.Context(), cookie.Value, true)
		if err != nil {
			return browserAuth{}
		}
		claims = &auth.Claims{UserID: session.UserID, TenantID: session.TenantID, IsAdmin: session.IsAdmin}
		console = cookie.Value
	} else {
		return browserAuth{}
	}

	// The caller's tenant scope keeps a relay token from joining another tenant's session.
	scoped := dbtx.WithTenant(r.Context(), claims.TenantID, claims.IsAdmin)
	held, err := s.sessions.Get(scoped, sessionToken)
	if err != nil || held.UserID != claims.UserID {
		return browserAuth{}
	}
	return browserAuth{ok: true, console: console}
}

// renewLiveConsole keeps the console session behind an open live connection out of its idle
// limit, at most once a minute, since a technician in a live session is at work.
func (s *Server) renewLiveConsole(cookie string, renewedAt time.Time) time.Time {
	now := s.now()
	if now.Sub(renewedAt) < consoleTouchEvery {
		return renewedAt
	}
	id, ok := s.openSessionCookie(cookie)
	if !ok {
		return renewedAt
	}
	if err := s.consoleSessions.Touch(context.Background(), sha256.Sum256(id), now); err != nil {
		s.logger.Warn("could not renew the console session behind a live session", "error", err)
		return renewedAt
	}
	return now
}

// sameOrigin reports whether a request's Origin is the console's own host, which a browser always
// sends on a WebSocket and another site cannot forge.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

// parseSide determines the relay side from the ?side= query param and authenticates a browser.
// Returns the side and how the browser proved itself, or rejects the WebSocket and returns false.
func (s *Server) parseSide(w http.ResponseWriter, r *http.Request, sessionToken string) (relay.Side, browserAuth, bool) {
	switch r.URL.Query().Get("side") {
	case "browser":
		if !sameOrigin(r) {
			http.Error(w, "foreign origin", http.StatusForbidden)
			return 0, browserAuth{}, false
		}
		authed := s.authenticateBrowser(r, sessionToken)
		if !authed.ok {
			rejectWebSocket(w, r, "browser side requires authorization")
			return 0, browserAuth{}, false
		}
		return relay.SideBrowser, authed, true
	case "agent":
		return relay.SideAgent, browserAuth{}, true
	default:
		rejectWebSocket(w, r, "invalid side")
		return 0, browserAuth{}, false
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

// registerAndWait parks conn until its session, or the console session of a cookie-opened browser
// side, ends; the deferred release frees the relay entry and session row when no pipe ever runs.
func (s *Server) registerAndWait(r *http.Request, wsConn *websocket.Conn, conn relay.Conn, token string,
	side relay.Side, console string,
) {
	ctx := r.Context()

	sessionDone, err := s.relay.Register(ctx, protocol.SessionToken(token), conn, side)
	if err != nil {
		s.logger.Error("relay register failed", "error", err, "token_prefix", protocol.RedactToken(token))
		_ = wsConn.Close(websocket.StatusInternalError, "relay error")
		return
	}
	// Accept hijacked the connection, so only this deferred CloseNow closes it; it runs last,
	// after the relay's graceful close, and skips the close handshake.
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

	// Parks until the session ends or the process shuts down; Accept hijacked the connection,
	// so nothing cancels the request context and it is not selected on.
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()
	var recheck <-chan time.Time
	var renewedAt time.Time
	if console != "" {
		recheckTicker := time.NewTicker(s.liveRecheck)
		defer recheckTicker.Stop()
		recheck = recheckTicker.C
	}
	for {
		select {
		case <-sessionDone:
			return
		case <-s.lifetime:
			return
		case <-recheck:
			// Console idleness never ends a live session; a sign-out or the session's total does.
			if _, err := s.liveConsoleSession(context.Background(), console, false); errors.Is(err, errNoConsoleSession) {
				s.logger.Info("console session ended; closing its live session",
					"token_prefix", protocol.RedactToken(token))
				_ = wsConn.Close(StatusSessionEnded, "your console session ended")
				return
			}
		case <-ticker.C:
			// A background parent keeps a shutdown from reading as an unanswered peer.
			pingCtx, cancelPing := context.WithTimeout(context.Background(), s.pingInterval)
			err := wsConn.Ping(pingCtx)
			cancelPing()
			if err == nil {
				if console != "" {
					renewedAt = s.renewLiveConsole(console, renewedAt)
				}
				continue
			}
			// Returning runs the deferred CloseNow, which ends the session for both sides.
			s.logger.Warn("relay peer did not answer a ping",
				"token_prefix", protocol.RedactToken(token), "side", sideLabel(side), "error", err)
			return
		}
	}
}

// handleRelayWebSocket authenticates the side, upgrades the connection and hands it to the
// relay; token_prefix is redacted inline at each call site so the static gate sees it.
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

	side, authed, ok := s.parseSide(w, r, token)
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
	s.registerAndWait(r, wsConn, NewWSConn(wsConn, label), token, side, authed.console)
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
