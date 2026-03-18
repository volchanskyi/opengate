package api

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// ListAuditEvents implements StrictServerInterface.
func (s *Server) ListAuditEvents(ctx context.Context, request ListAuditEventsRequestObject) (ListAuditEventsResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, ListAuditEvents403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	q := db.AuditQuery{
		Limit:  50,
		Offset: 0,
	}
	if request.Params.Limit != nil {
		q.Limit = *request.Params.Limit
		if q.Limit > 200 {
			q.Limit = 200
		}
	}
	if request.Params.Offset != nil {
		q.Offset = *request.Params.Offset
	}
	if request.Params.Action != nil {
		q.Action = *request.Params.Action
	}
	if request.Params.UserId != nil {
		uid := *request.Params.UserId
		q.UserID = &uid
	}

	events, err := s.store.QueryAuditLog(ctx, q)
	if err != nil {
		return nil, err
	}

	return ListAuditEvents200JSONResponse(auditEventsToAPI(events)), nil
}
