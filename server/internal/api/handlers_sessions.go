package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

type createSessionRequest struct {
	DeviceID    string               `json:"device_id"`
	Permissions *protocol.Permissions `json:"permissions,omitempty"`
}

type createSessionResponse struct {
	Token    string `json:"token"`
	RelayURL string `json:"relay_url"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid device_id")
		return
	}

	// Verify device exists in DB
	if _, err := s.store.GetDevice(r.Context(), deviceID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up device")
		return
	}

	// Check agent is connected
	agentConn := s.agents.GetAgent(deviceID)
	if agentConn == nil {
		writeError(w, http.StatusConflict, "agent not connected")
		return
	}

	// Generate session token
	token := protocol.GenerateSessionToken()

	// Default permissions
	perms := protocol.Permissions{}
	if req.Permissions != nil {
		perms = *req.Permissions
	}

	// Store session in DB
	userID := ContextUserID(r.Context())
	sess := &db.AgentSession{
		Token:    string(token),
		DeviceID: deviceID,
		UserID:   userID,
	}
	if err := s.store.CreateAgentSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Build relay URL
	scheme := "wss"
	if r.TLS == nil {
		scheme = "ws"
	}
	relayURL := fmt.Sprintf("%s://%s/ws/relay/%s", scheme, r.Host, token)

	// Send SessionRequest to agent
	if err := agentConn.SendSessionRequest(r.Context(), token, relayURL, perms); err != nil {
		s.logger.Error("send session request to agent", "error", err, "device_id", deviceID)
		// Session is created but agent notification failed — caller can still use the relay URL
	}

	writeJSON(w, http.StatusCreated, createSessionResponse{
		Token:    string(token),
		RelayURL: relayURL,
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	deviceIDStr := r.URL.Query().Get("device_id")
	if deviceIDStr == "" {
		writeError(w, http.StatusBadRequest, "device_id query parameter is required")
		return
	}

	deviceID, err := uuid.Parse(deviceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid device_id")
		return
	}

	sessions, err := s.store.ListActiveSessionsForDevice(r.Context(), deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	if sessions == nil {
		sessions = []*db.AgentSession{}
	}

	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	if err := s.store.DeleteAgentSession(r.Context(), token); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
