package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// Reading the triage queue and the rooms in it.
//
// Authorization here is the tenant, not the customer. Reading incidents,
// transitioning them, assigning them and commenting on them are operational work
// on the tenant's own resources, so membership is the whole gate — the same rule
// device pages already follow. What the customer picker does is narrow, and a
// narrowed read must never return another customer's row even though both sit
// inside one tenant. Those are two different failures: the first would be a
// breach of the wall, and the second is a query defect that refuses nothing and
// simply shows the wrong estate.
//
// The device page's strip is the same read with the machine filter set, rather
// than a second list implementation — two implementations of one question drift,
// and the one that drifts is the one nobody is looking at.

// errInvestigationsUnavailable is a deployment wired without an investigation
// store. It is an error rather than an empty queue: a triage queue that reads
// empty is the one wrong answer nobody questions.
var errInvestigationsUnavailable = errors.New("investigations are not configured on this server")

// ListInvestigations implements StrictServerInterface. It answers one page of
// the queue, newest activity first, and says where the next page starts.
func (s *Server) ListInvestigations(ctx context.Context, request ListInvestigationsRequestObject) (ListInvestigationsResponseObject, error) {
	if s.investigations == nil {
		return nil, errInvestigationsUnavailable
	}
	filter, err := incidentFilterFromParams(request.Params)
	if err != nil {
		return ListInvestigations400JSONResponse{Error: err.Error()}, nil
	}
	page, err := s.investigations.Queue(ctx, filter)
	if err != nil {
		return nil, err
	}
	return ListInvestigations200JSONResponse(incidentPageToAPI(page)), nil
}

// GetInvestigation implements StrictServerInterface. It answers one room with
// the most recent of what folded into it and the history of what people did,
// which are the two halves an investigation is worked from.
func (s *Server) GetInvestigation(ctx context.Context, request GetInvestigationRequestObject) (GetInvestigationResponseObject, error) {
	if s.investigations == nil {
		return nil, errInvestigationsUnavailable
	}
	room, err := s.investigations.Investigation(ctx, request.Id, deref(request.Params.OrganizationId))
	if err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return GetInvestigation404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}
	return GetInvestigation200JSONResponse(investigationToAPI(room)), nil
}

// ListDeviceIncidents implements StrictServerInterface. It is the device page's
// strip: the same queue read narrowed to the rooms holding an alert this machine
// raised, including the customer-wide ones it is one of forty machines in.
func (s *Server) ListDeviceIncidents(ctx context.Context, request ListDeviceIncidentsRequestObject) (ListDeviceIncidentsResponseObject, error) {
	if s.investigations == nil {
		return nil, errInvestigationsUnavailable
	}
	// The machine is what the caller named, so it is what has to be resolved
	// inside the tenant before anything about it is answered.
	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return ListDeviceIncidents404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	cursor, err := decodeCursor(deref(request.Params.Cursor))
	if err != nil {
		return ListDeviceIncidents400JSONResponse{Error: err.Error()}, nil
	}
	filter := alerts.Filter{
		DeviceID: request.Id,
		After:    cursor,
		Limit:    deref(request.Params.Limit),
	}
	if request.Params.Status != nil {
		filter.Statuses = mapped[IncidentStatus, alerts.Status](*request.Params.Status)
	}

	page, err := s.investigations.Queue(ctx, filter)
	if err != nil {
		return nil, err
	}
	return ListDeviceIncidents200JSONResponse(incidentPageToAPI(page)), nil
}

// requireIncidentInScope is the named guard for every route addressed by an
// incident id, alongside requireDeviceInScope and its siblings. It answers the
// room so a handler that needs it afterwards does not read twice, and refuses a
// room outside the tenant exactly as it refuses one outside the customer on
// screen — a caller must not be able to tell either from a room that does not
// exist.
func (s *Server) requireIncidentInScope(ctx context.Context, incidentID, organizationID uuid.UUID) (alerts.Incident, error) {
	if s.investigations == nil {
		return alerts.Incident{}, errInvestigationsUnavailable
	}
	return s.investigations.Incident(ctx, incidentID, organizationID)
}
