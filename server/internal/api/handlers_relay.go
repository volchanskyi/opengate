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

func (s *Server) handleRelayWebSocket(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	// Validate token exists in DB
	if _, err := s.store.GetAgentSession(r.Context(), token); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			rejectWebSocket(w, r, "session not found")
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	side, ok := parseSide(w, r)
	if !ok {
		return
	}

	// Upgrade to WebSocket
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Error("websocket accept", "error", err)
		return
	}

	// Wrap into relay.Conn
	ctx := r.Context()
	conn := NewWSConn(wsConn)

	// Register with relay
	if err := s.relay.Register(ctx, protocol.SessionToken(token), conn, side); err != nil {
		s.logger.Error("relay register", "error", err, "token", token)
		wsConn.Close(websocket.StatusInternalError, "relay error")
		return
	}

	// Wait for peer or context cancellation
	if err := s.relay.WaitForPeer(ctx, protocol.SessionToken(token)); err != nil {
		if !errors.Is(err, r.Context().Err()) {
			s.logger.Error("relay wait for peer", "error", err, "token", token)
		}
		return
	}

	// Block until context is done (relay handles piping)
	<-ctx.Done()
}
