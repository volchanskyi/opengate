package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

// CreateSession implements StrictServerInterface.
func (s *Server) CreateSession(ctx context.Context, request CreateSessionRequestObject) (CreateSessionResponseObject, error) {
	deviceID := request.Body.DeviceId

	// Verify device exists and user owns it.
	d, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return CreateSession404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, d.GroupID) {
		return CreateSession403JSONResponse{Error: msgForbidden}, nil
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
	sess := &session.Session{
		Token:    string(token),
		DeviceID: deviceID,
		UserID:   userID,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}

	// Build relay URL
	scheme := "wss"
	host := "localhost"
	if r := httpRequestFromContext(ctx); r != nil {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			if proto == "http" {
				scheme = "ws"
			}
		} else if r.TLS == nil {
			scheme = "ws"
		}
		if r.Host != "" {
			host = r.Host
		}
	}
	relayURL := fmt.Sprintf("%s://%s/ws/relay/%s", scheme, host, token)

	// Send SessionRequest to agent — clean up orphaned session on failure
	if err := agentConn.SendSessionRequest(ctx, token, relayURL, perms); err != nil {
		s.logger.Error("send session request to agent", "error", err, "device_id", deviceID)
		if delErr := s.sessions.Delete(ctx, string(token)); delErr != nil {
			s.logger.Warn("orphan session cleanup failed", "token_prefix", protocol.RedactToken(string(token)), "error", delErr)
		}
		return CreateSession409JSONResponse{Error: "agent communication failed"}, nil
	}

	// Build ICE server list from signaling config
	var iceServers *[]ICEServer
	if s.signaling != nil {
		servers := iceServersToAPI(s.signaling.Config().ICEServers)
		iceServers = &servers
	}

	s.auditLog(userID, "session.create", deviceID.String(), "")
	startedEvt := notifications.Event{
		Type:      notifications.EventSessionStarted,
		DeviceID:  deviceID,
		UserID:    userID,
		Timestamp: time.Now(),
	}
	_ = s.notifier.Notify(ctx, startedEvt) // fire-and-forget

	return CreateSession201JSONResponse{
		Token:      string(token),
		RelayUrl:   relayURL,
		IceServers: iceServers,
	}, nil
}

// ListSessions implements StrictServerInterface.
func (s *Server) ListSessions(ctx context.Context, request ListSessionsRequestObject) (ListSessionsResponseObject, error) {
	// Verify user owns the device's group.
	d, err := s.devices.Get(ctx, request.Params.DeviceId)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return ListSessions200JSONResponse([]AgentSession{}), nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, d.GroupID) {
		return ListSessions403JSONResponse{Error: msgForbidden}, nil
	}

	sessions, err := s.sessions.ListActiveForDevice(ctx, request.Params.DeviceId)
	if err != nil {
		return nil, err
	}

	if sessions == nil {
		sessions = []*session.Session{}
	}

	return ListSessions200JSONResponse(sessionsToAPI(sessions)), nil
}

// DeleteSession implements StrictServerInterface.
//
// Per ADR-019, this transport handler delegates orchestration to
// usecase.SessionService.Delete: the use case owns the ownership check,
// the persistence, the audit write, and the push event. The handler is a
// thin translator — extract userID/isAdmin from JWT claims, map domain
// errors to HTTP status codes.
func (s *Server) DeleteSession(ctx context.Context, request DeleteSessionRequestObject) (DeleteSessionResponseObject, error) {
	err := s.sessionUC.Delete(ctx, usecase.DeleteSessionInput{
		Token:   request.Token,
		UserID:  ContextUserID(ctx),
		IsAdmin: isAdmin(ctx),
	})
	switch {
	case err == nil:
		return DeleteSession204Response{}, nil
	case errors.Is(err, usecase.ErrSessionNotFound):
		return DeleteSession404JSONResponse{Error: "session not found"}, nil
	case errors.Is(err, usecase.ErrSessionForbidden):
		return DeleteSession403JSONResponse{Error: msgForbidden}, nil
	default:
		return nil, err
	}
}
