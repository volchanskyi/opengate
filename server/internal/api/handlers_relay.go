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

func (s *Server) handleRelayWebSocket(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	// Validate token exists in DB
	if _, err := s.store.GetAgentSession(r.Context(), token); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			wsConn, acceptErr := websocket.Accept(w, r, &websocket.AcceptOptions{
				InsecureSkipVerify: true,
			})
			if acceptErr != nil {
				return
			}
			wsConn.Close(websocket.StatusPolicyViolation, "session not found")
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Determine side from query param
	sideParam := r.URL.Query().Get("side")
	var side relay.Side
	switch sideParam {
	case "browser":
		// Validate JWT for browser side
		header := r.Header.Get("Authorization")
		if header == "" {
			wsConn, acceptErr := websocket.Accept(w, r, &websocket.AcceptOptions{
				InsecureSkipVerify: true,
			})
			if acceptErr != nil {
				return
			}
			wsConn.Close(websocket.StatusPolicyViolation, "browser side requires authorization")
			return
		}
		side = relay.SideBrowser
	case "agent":
		side = relay.SideAgent
	default:
		wsConn, acceptErr := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if acceptErr != nil {
			return
		}
		wsConn.Close(websocket.StatusPolicyViolation, "invalid side")
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
	conn := NewWSConn(ctx, wsConn)

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
