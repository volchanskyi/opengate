package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// msgAssigneeNotFound is the answer for a person outside the caller's tenant.
const msgAssigneeNotFound = "assignee not found"

// SetInvestigationAssignee implements StrictServerInterface; an absent assignee returns the
// incident to the queue.
func (s *Server) SetInvestigationAssignee(ctx context.Context, request SetInvestigationAssigneeRequestObject) (SetInvestigationAssigneeResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return SetInvestigationAssignee404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	var assignee uuid.UUID
	if request.Body.AssigneeId != nil {
		assignee = *request.Body.AssigneeId
		// The tenant-scoped user read makes an outside user answer as nonexistent.
		if _, err := s.users.Get(ctx, assignee); err != nil {
			if errors.Is(err, auth.ErrUserNotFound) {
				return SetInvestigationAssignee404JSONResponse{Error: msgAssigneeNotFound}, nil
			}
			return nil, err
		}
	}

	switch err := s.investigations.Assign(ctx, request.Id, assignee, ContextUserID(ctx)); {
	case err == nil:
	case errors.Is(err, alerts.ErrIncidentNotFound):
		return SetInvestigationAssignee404JSONResponse{Error: msgIncidentNotFound}, nil
	default:
		return nil, err
	}

	taken, err := s.investigations.Incident(ctx, request.Id, uuid.Nil)
	if err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "incident.assign", request.Id.String(), assignee.String())
	return SetInvestigationAssignee200JSONResponse(incidentToAPI(taken)), nil
}
