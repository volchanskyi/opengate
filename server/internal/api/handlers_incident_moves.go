package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// msgIncidentNotFound is the one answer every boundary gives, so a caller
// cannot tell a room they may not see from one that does not exist.
const msgIncidentNotFound = "incident not found"

// SetInvestigationStatus implements StrictServerInterface: tenant membership is the whole gate,
// and each refused move is its own typed 400 answer.
func (s *Server) SetInvestigationStatus(ctx context.Context, request SetInvestigationStatusRequestObject) (SetInvestigationStatusResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return SetInvestigationStatus404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	change := alerts.Change{
		To:    alerts.Status(request.Body.Status),
		Actor: ContextUserID(ctx),
	}
	if request.Body.CauseCode != nil {
		change.Cause = alerts.CauseCode(*request.Body.CauseCode)
	}

	switch err := s.investigations.Transition(ctx, request.Id, change); {
	case err == nil:
	case errors.Is(err, alerts.ErrIncidentNotFound):
		return SetInvestigationStatus404JSONResponse{Error: msgIncidentNotFound}, nil
	case isRefusedMove(err):
		return SetInvestigationStatus400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	moved, err := s.investigations.Incident(ctx, request.Id, uuid.Nil)
	if err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "incident.status", request.Id.String(), string(change.To))
	return SetInvestigationStatus200JSONResponse(incidentToAPI(moved)), nil
}

// isRefusedMove reports whether the store refused a transition as a caller's mistake.
func isRefusedMove(err error) bool {
	return errors.Is(err, alerts.ErrIllegalTransition) ||
		errors.Is(err, alerts.ErrUnknownStatus) ||
		errors.Is(err, alerts.ErrUnknownCause) ||
		errors.Is(err, alerts.ErrCauseRequired) ||
		errors.Is(err, alerts.ErrCauseNotAllowed)
}
