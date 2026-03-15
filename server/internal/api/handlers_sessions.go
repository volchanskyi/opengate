package api

import (
	"context"
	"errors"
	"fmt"

	"time"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// CreateSession implements StrictServerInterface.
func (s *Server) CreateSession(ctx context.Context, request CreateSessionRequestObject) (CreateSessionResponseObject, error) {
	deviceID := request.Body.DeviceId

	// Verify device exists in DB
	if _, err := s.store.GetDevice(ctx, deviceID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return CreateSession404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	// Check agent is connected
	agentConn := s.agents.GetAgent(deviceID)
	if agentConn == nil {
		return CreateSession409JSONResponse{Error: "agent not connected"}, nil
	}

	// Generate session token
	token := protocol.GenerateSessionToken()

	// Convert permissions
	perms := permissionsToProtocol(request.Body.Permissions)

	// Store session in DB
	userID := ContextUserID(ctx)
	sess := &db.AgentSession{
		Token:    string(token),
		DeviceID: deviceID,
		UserID:   userID,
	}
	if err := s.store.CreateAgentSession(ctx, sess); err != nil {
		return nil, err
	}

	// Build relay URL
	scheme := "wss"
	host := "localhost"
	if r := httpRequestFromContext(ctx); r != nil {
		if r.TLS == nil {
			scheme = "ws"
		}
		if r.Host != "" {
			host = r.Host
		}
	}
	relayURL := fmt.Sprintf("%s://%s/ws/relay/%s", scheme, host, token)

	// Send SessionRequest to agent
	if err := agentConn.SendSessionRequest(ctx, token, relayURL, perms); err != nil {
		s.logger.Error("send session request to agent", "error", err, "device_id", deviceID)
	}

	// Build ICE server list from signaling config
	var iceServers *[]ICEServer
	if s.signaling != nil {
		servers := iceServersToAPI(s.signaling.Config().ICEServers)
		iceServers = &servers
	}

	s.auditLog(userID, "session.create", deviceID.String(), "")
	_ = s.notifier.Notify(ctx, notifications.Event{
		Type:     notifications.EventSessionStarted,
		DeviceID: deviceID,
		UserID:   userID,
		Timestamp: time.Now(),
	})

	return CreateSession201JSONResponse{
		Token:      string(token),
		RelayUrl:   relayURL,
		IceServers: iceServers,
	}, nil
}

// ListSessions implements StrictServerInterface.
func (s *Server) ListSessions(ctx context.Context, request ListSessionsRequestObject) (ListSessionsResponseObject, error) {
	sessions, err := s.store.ListActiveSessionsForDevice(ctx, request.Params.DeviceId)
	if err != nil {
		return nil, err
	}

	if sessions == nil {
		sessions = []*db.AgentSession{}
	}

	return ListSessions200JSONResponse(sessionsToAPI(sessions)), nil
}

// DeleteSession implements StrictServerInterface.
func (s *Server) DeleteSession(ctx context.Context, request DeleteSessionRequestObject) (DeleteSessionResponseObject, error) {
	if err := s.store.DeleteAgentSession(ctx, request.Token); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return DeleteSession404JSONResponse{Error: "session not found"}, nil
		}
		return nil, err
	}

	s.auditLog(ContextUserID(ctx), "session.delete", protocol.RedactToken(request.Token), "")
	_ = s.notifier.Notify(ctx, notifications.Event{
		Type:      notifications.EventSessionEnded,
		UserID:    ContextUserID(ctx),
		Timestamp: time.Now(),
	})

	return DeleteSession204Response{}, nil
}
