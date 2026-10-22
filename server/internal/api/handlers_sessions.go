package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

// The relay close reason the old tab shows when its holder opens the host again.
const reasonOpenedElsewhere = "Opened elsewhere"

const (
	msgAgentNotConnected = "agent not connected"
	msgSessionOpen       = "another technician holds a session on this host"
	msgSessionEnding     = "the session on this host is ending; open it again"
	msgNotSessionHolder  = "only the session's holder or an administrator may end it"
	msgNotOutcomeHolder  = "only the session's holder reports how its direct link ended"
	msgUnknownOutcome    = "outcome must be direct, no_route, unsupported or dropped"
)

// CreateSession implements StrictServerInterface. A host takes one session: another technician's
// is refused naming its holder, and the caller's own earlier session is replaced.
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
		return CreateSession409JSONResponse{Error: msgAgentNotConnected}, nil
	}

	token := protocol.GenerateSessionToken()
	perms := permissionsToProtocol(request.Body.Permissions)
	userID := ContextUserID(ctx)
	replaced, err := s.sessions.Create(ctx, &session.Session{
		Token:    string(token),
		DeviceID: deviceID,
		UserID:   userID,
	})
	if errors.Is(err, session.ErrDeviceHeld) {
		return s.refuseHeldDevice(ctx, deviceID)
	}
	if err != nil {
		return nil, err
	}
	if replaced != "" && s.relay != nil {
		s.relay.End(protocol.SessionToken(replaced), reasonOpenedElsewhere)
	}

	// The host dials the configured address; a request header never decides where it connects.
	hostRelayURL := s.signaling.AgentRelayURL(string(token))
	if err := agentConn.SendSessionRequest(ctx, token, hostRelayURL, perms, s.signaling.Helpers); err != nil {
		s.logger.Error("send session request to agent", "error", err, "device_id", deviceID)
		if delErr := s.sessions.Delete(ctx, string(token)); delErr != nil {
			s.logger.Warn("orphan session cleanup failed", "token_prefix", protocol.RedactToken(string(token)), "error", delErr)
		}
		return CreateSession409JSONResponse{Error: "agent communication failed"}, nil
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
		RelayUrl:   browserRelayURL(ctx, token),
		IceServers: helpersToAPI(s.signaling.Helpers),
	}, nil
}

// browserRelayURL builds the relay address from the address the browser's own request reached.
func browserRelayURL(ctx context.Context, token protocol.SessionToken) string {
	scheme := "wss"
	host := "localhost"
	if r := httpRequestFromContext(ctx); r != nil {
		if !reachedOverTLS(r) {
			scheme = "ws"
		}
		if r.Host != "" {
			host = r.Host
		}
	}
	return fmt.Sprintf("%s://%s/ws/relay/%s", scheme, host, token)
}

// refuseHeldDevice answers a create on a host another technician holds, naming the holder.
func (s *Server) refuseHeldDevice(ctx context.Context, deviceID uuid.UUID) (CreateSessionResponseObject, error) {
	held, err := s.sessions.ForDevice(ctx, deviceID)
	if errors.Is(err, session.ErrSessionNotFound) {
		// The holder's session ended between the refusal and this read.
		return CreateSession409JSONResponse{Error: msgSessionEnding}, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateSession409JSONResponse{
		Error:      msgSessionOpen,
		HolderName: &held.HolderName,
		StartedAt:  &held.CreatedAt,
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

// DeleteSession implements StrictServerInterface; the token resolves in the caller's tenant scope,
// and only the holder or an administrator ends the session.
func (s *Server) DeleteSession(ctx context.Context, request DeleteSessionRequestObject) (DeleteSessionResponseObject, error) {
	userID := ContextUserID(ctx)
	err := s.sessionUC.Delete(ctx, usecase.DeleteSessionInput{
		Token:     request.Token,
		UserID:    userID,
		ActorName: s.displayName(ctx, userID),
		IsAdmin:   isAdmin(ctx),
	})
	switch {
	case err == nil:
		return DeleteSession204Response{}, nil
	case errors.Is(err, usecase.ErrSessionNotFound):
		return DeleteSession404JSONResponse{Error: msgSessionNotFound}, nil
	case errors.Is(err, usecase.ErrForbidden):
		return DeleteSession403JSONResponse{Error: msgNotSessionHolder}, nil
	default:
		return nil, err
	}
}

// EndDeviceSession implements StrictServerInterface: the holder or an administrator ends the
// device's one session.
func (s *Server) EndDeviceSession(ctx context.Context, request EndDeviceSessionRequestObject) (EndDeviceSessionResponseObject, error) {
	userID := ContextUserID(ctx)
	err := s.sessionUC.Delete(ctx, usecase.DeleteSessionInput{
		DeviceID:  request.Id,
		UserID:    userID,
		ActorName: s.displayName(ctx, userID),
		IsAdmin:   isAdmin(ctx),
	})
	switch {
	case err == nil:
		return EndDeviceSession204Response{}, nil
	case errors.Is(err, usecase.ErrSessionNotFound):
		return EndDeviceSession404JSONResponse{Error: msgSessionNotFound}, nil
	case errors.Is(err, usecase.ErrForbidden):
		return EndDeviceSession403JSONResponse{Error: msgNotSessionHolder}, nil
	default:
		return nil, err
	}
}

// displayName is how the reason a holder's tab shows names a user: display name, else email.
func (s *Server) displayName(ctx context.Context, userID uuid.UUID) string {
	u, err := s.users.Get(ctx, userID)
	if err != nil {
		return "another technician"
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

// ReportDirectLinkOutcome implements StrictServerInterface: the holder's browser reports how the
// session's direct-link attempt ended, and each outcome counts once per session.
func (s *Server) ReportDirectLinkOutcome(ctx context.Context, request ReportDirectLinkOutcomeRequestObject) (ReportDirectLinkOutcomeResponseObject, error) {
	outcome := string(request.Body.Outcome)
	bit := slices.Index(appmetrics.DirectLinkOutcomes(), outcome)
	if bit < 0 {
		return ReportDirectLinkOutcome400JSONResponse{Error: msgUnknownOutcome}, nil
	}

	held, err := s.sessions.Get(ctx, request.Token)
	if errors.Is(err, session.ErrSessionNotFound) {
		return ReportDirectLinkOutcome404JSONResponse{Error: msgSessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	if held.UserID != ContextUserID(ctx) {
		return ReportDirectLinkOutcome403JSONResponse{Error: msgNotOutcomeHolder}, nil
	}

	first, err := s.sessions.MarkOutcome(ctx, request.Token, bit)
	if errors.Is(err, session.ErrSessionNotFound) {
		return ReportDirectLinkOutcome404JSONResponse{Error: msgSessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	if first && s.metrics != nil {
		s.metrics.ObserveDirectLinkOutcome(outcome)
	}
	return ReportDirectLinkOutcome204Response{}, nil
}
