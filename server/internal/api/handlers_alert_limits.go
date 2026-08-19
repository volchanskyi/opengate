package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// A customer's alert budget.
//
// It sits on its own page rather than on a rule, because it is not a property of
// any rule: it is the safety net under all of them, and the thing it protects is
// the customer's own detection. Both halves were chosen from an estimate of a
// rate nobody had measured, which is why they move at all — being unable to
// change a wrong guess without cutting a release turns it into an outage.

// errAlertLimitsUnavailable is a deployment wired without the alert store.
var errAlertLimitsUnavailable = errors.New("alert budgets are not configured on this server")

// GetAlertLimits implements StrictServerInterface. Every member of the tenant
// reads it: a technician looking at a storm room needs to know what refused the
// alerts it counts.
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

// PutAlertLimits implements StrictServerInterface. Neither half may pass the
// maximum the code allows — a limit an operator can raise without bound is not a
// limit — and neither may be set to nothing, which would silence the customer's
// detection outright.
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
