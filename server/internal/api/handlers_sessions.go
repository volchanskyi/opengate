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

	// Verify the device exists in the caller's tenant.
	if err := s.requireDeviceInScope(ctx, deviceID); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return CreateSession404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	agentConn := s.agents.GetAgent(deviceID)
	if agentConn == nil {
		return CreateSession409JSONResponse{Error: "agent not connected"}, nil
	}

	token := protocol.GenerateSessionToken()

	perms := permissionsToProtocol(request.Body.Permissions)

	userID := ContextUserID(ctx)
	sess := &session.Session{
		Token:    string(token),
		DeviceID: deviceID,
		UserID:   userID,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}

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

	if err := agentConn.SendSessionRequest(ctx, token, relayURL, perms); err != nil {
		s.logger.Error("send session request to agent", "error", err, "device_id", deviceID)
		if delErr := s.sessions.Delete(ctx, string(token)); delErr != nil {
			s.logger.Warn("orphan session cleanup failed", "token_prefix", protocol.RedactToken(string(token)), "error", delErr)
		}
		return CreateSession409JSONResponse{Error: "agent communication failed"}, nil
	}

	// An empty server list is omitted from the response; a client would read it as "try nothing".
	var iceServers *[]ICEServer
	if len(s.signaling.ICEServers) > 0 {
		servers := iceServersToAPI(s.signaling.ICEServers)
		iceServers = &servers
	}

	s.auditLog(ctx, userID, "session.create", deviceID.String(), "")
	startedEvt := notifications.Event{
		Type:      notifications.EventSessionStarted,
		DeviceID:  deviceID,
		UserID:    userID,
		Timestamp: time.Now(),
	}
	_ = s.notifier.Notify(ctx, startedEvt)

	return CreateSession201JSONResponse{
		Token:      string(token),
		RelayUrl:   relayURL,
		IceServers: iceServers,
	}, nil
}

// ListSessions implements StrictServerInterface.
func (s *Server) ListSessions(ctx context.Context, request ListSessionsRequestObject) (ListSessionsResponseObject, error) {
	// Verify the device exists in the caller's tenant.
	if err := s.requireDeviceInScope(ctx, request.Params.DeviceId); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return ListSessions200JSONResponse([]AgentSession{}), nil
		}
		return nil, err
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

// DeleteSession implements StrictServerInterface; the token resolves in the caller's tenant scope.
func (s *Server) DeleteSession(ctx context.Context, request DeleteSessionRequestObject) (DeleteSessionResponseObject, error) {
	if err := s.requireSessionInScope(ctx, request.Token); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return DeleteSession404JSONResponse{Error: msgSessionNotFound}, nil
		}
		return nil, err
	}

	err := s.sessionUC.Delete(ctx, usecase.DeleteSessionInput{
		Token:  request.Token,
		UserID: ContextUserID(ctx),
	})
	switch {
	case err == nil:
		return DeleteSession204Response{}, nil
	case errors.Is(err, usecase.ErrSessionNotFound):
		return DeleteSession404JSONResponse{Error: msgSessionNotFound}, nil
	default:
		return nil, err
	}
}
