package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// errAlertLimitsUnavailable is a deployment wired without the alert store.
var errAlertLimitsUnavailable = errors.New("alert budgets are not configured on this server")

// GetAlertLimits implements StrictServerInterface; every member of the tenant may read it.
func (s *Server) GetAlertLimits(ctx context.Context, request GetAlertLimitsRequestObject) (GetAlertLimitsResponseObject, error) {
	if s.alertBudget == nil {
		return nil, errAlertLimitsUnavailable
	}
	limits, err := s.alertBudget.Limits(ctx, deref(request.Params.OrganizationId))
	if err != nil {
		return nil, err
	}
	return GetAlertLimits200JSONResponse(limitsToAPI(limits)), nil
}

// PutAlertLimits implements StrictServerInterface. Each limit stays within the code's maximum
// and above zero, since zero would silence the customer's detection.
func (s *Server) PutAlertLimits(ctx context.Context, request PutAlertLimitsRequestObject) (PutAlertLimitsResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PutAlertLimits403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.alertBudget == nil {
		return nil, errAlertLimitsUnavailable
	}

	organizationID, err := s.customerOrDefault(ctx, request.Params.OrganizationId)
	if err != nil {
		return nil, err
	}
	limits := alerts.Limits{
		OrganizationID:     organizationID,
		OrganizationHourly: request.Body.OrganizationHourly,
		DeviceHourly:       request.Body.DeviceHourly,
		UpdatedBy:          ContextUserID(ctx).String(),
	}

	switch err := s.alertBudget.UpsertLimits(ctx, limits); {
	case err == nil:
	case errors.Is(err, alerts.ErrInvalidLimits):
		return PutAlertLimits400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "alert.limits.set", limits.OrganizationID.String(),
		fmt.Sprintf("customer=%d/h machine=%d/h", limits.OrganizationHourly, limits.DeviceHourly))
	return PutAlertLimits200JSONResponse(limitsToAPI(limits)), nil
}
