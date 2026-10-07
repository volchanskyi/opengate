package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// AddInvestigationComment implements StrictServerInterface by appending to the incident history;
// the audit log omits it because the history line is the record.
func (s *Server) AddInvestigationComment(ctx context.Context, request AddInvestigationCommentRequestObject) (AddInvestigationCommentResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return AddInvestigationComment404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	event, err := s.investigations.Comment(ctx, request.Id, ContextUserID(ctx), request.Body.Body)
	switch {
	case err == nil:
	case errors.Is(err, alerts.ErrIncidentNotFound):
		return AddInvestigationComment404JSONResponse{Error: msgIncidentNotFound}, nil
	case errors.Is(err, alerts.ErrCommentUnusable):
		return AddInvestigationComment400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}
	return AddInvestigationComment201JSONResponse(incidentEventToAPI(event)), nil
}
