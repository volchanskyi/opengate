package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// errInvestigationsUnavailable is a deployment wired without an investigation store; it
// surfaces as an error so a missing store never reads as an empty queue.
var errInvestigationsUnavailable = errors.New("investigations are not configured on this server")

// ListInvestigations implements StrictServerInterface: one page of the queue, newest activity
// first, gated by tenant membership with the customer picker only narrowing within it.
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

// GetInvestigation implements StrictServerInterface with one room's recent alerts and history.
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

// ListDeviceIncidents implements StrictServerInterface as the queue read narrowed to rooms
// holding an alert this machine raised, customer-wide rooms included.
func (s *Server) ListDeviceIncidents(ctx context.Context, request ListDeviceIncidentsRequestObject) (ListDeviceIncidentsResponseObject, error) {
	if s.investigations == nil {
		return nil, errInvestigationsUnavailable
	}
	// The named machine is resolved inside the tenant before anything about it is answered.
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

// requireIncidentInScope guards every route addressed by an incident id and returns the room;
// a room outside the tenant or customer answers as nonexistent.
func (s *Server) requireIncidentInScope(ctx context.Context, incidentID, organizationID uuid.UUID) (alerts.Incident, error) {
	if s.investigations == nil {
		return alerts.Incident{}, errInvestigationsUnavailable
	}
	return s.investigations.Incident(ctx, incidentID, organizationID)
}
