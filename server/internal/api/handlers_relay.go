package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"nhooyr.io/websocket"
)

// rejectWebSocket accepts the WebSocket handshake and immediately closes the
// connection with a policy-violation status code carrying the given reason.
func rejectWebSocket(w http.ResponseWriter, r *http.Request, reason string) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	c.Close(websocket.StatusPolicyViolation, reason)
}

// ensureBrowserAuth ensures the browser side has an Authorization header,
// falling back to the ?auth= query param (browser WebSocket API cannot set custom headers).
func ensureBrowserAuth(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return true
	}
	authParam := r.URL.Query().Get("auth")
	if authParam == "" {
		return false
	}
	r.Header.Set("Authorization", "Bearer "+authParam)
	return true
}

// parseSide determines the relay side from the ?side= query param.
// Returns the side and true on success, or rejects the WebSocket and returns false.
func parseSide(w http.ResponseWriter, r *http.Request) (relay.Side, bool) {
	switch r.URL.Query().Get("side") {
	case "browser":
		if !ensureBrowserAuth(r) {
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

// registerAndWait registers conn with the relay and blocks until the peer connects
// or the request context is cancelled. It closes wsConn on registration failure.
func (s *Server) registerAndWait(r *http.Request, wsConn *websocket.Conn, conn relay.Conn, token string, side relay.Side) {
	ctx := r.Context()
	tp := protocol.RedactToken(token)

	if err := s.relay.Register(ctx, protocol.SessionToken(token), conn, side); err != nil {
		s.logger.Error("relay register failed", "error", err, "token_prefix", tp)
		wsConn.Close(websocket.StatusInternalError, "relay error")
		return
	}

	if err := s.relay.WaitForPeer(ctx, protocol.SessionToken(token)); err != nil {
		s.logger.Error("relay wait for peer failed", "error", err, "token_prefix", tp)
		return
	}

	// Block until the request context is done (relay handles piping).
	<-ctx.Done()
}

func (s *Server) handleRelayWebSocket(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tp := protocol.RedactToken(token)

	if err := s.validateRelayToken(r, token); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			s.logger.Warn("relay token not found", "token_prefix", tp)
			rejectWebSocket(w, r, "session not found")
		} else {
			s.logger.Error("relay token validation error", "token_prefix", tp, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	side, ok := parseSide(w, r)
	if !ok {
		s.logger.Warn("relay invalid side param", "token_prefix", tp, "side_param", r.URL.Query().Get("side"))
		return
	}

	sideLabel := "agent"
	if side == relay.SideBrowser {
		sideLabel = "browser"
	}

	wsConn := s.upgradeRelayWebSocket(w, r)
	if wsConn == nil {
		return
	}

	s.logger.Info("relay session connected", "token_prefix", tp, "side", sideLabel)
	s.registerAndWait(r, wsConn, NewWSConn(wsConn, sideLabel), token, side)
	s.logger.Info("relay session disconnected", "token_prefix", tp, "side", sideLabel)
}

// validateRelayToken checks that the given token exists in the agent session store.
func (s *Server) validateRelayToken(r *http.Request, token string) error {
	_, err := s.store.GetAgentSession(r.Context(), token)
	return err
}
