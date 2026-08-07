package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/device"
)

// ListDevices implements StrictServerInterface. The repository predicate is the
// whole gate: a caller sees its own tenant's devices. Narrowing to a customer or
// a site is a filter, not a permission — a technician sees every customer in
// the tenant, and the picker chooses which one to look at.
func (s *Server) ListDevices(ctx context.Context, request ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	devices, err := s.devices.List(ctx, deviceFilterFromParams(request.Params))
	if err != nil {
		return nil, err
	}

	apiDevices := devicesToAPI(devices)
	s.enrichAnomalyRates(ctx, apiDevices)
	return ListDevices200JSONResponse(apiDevices), nil
}

// deviceFilterFromParams turns the optional query parameters into the repository
// filter. An absent parameter leaves that field zero, which does not narrow.
func deviceFilterFromParams(params ListDevicesParams) device.Filter {
	var filter device.Filter
	if params.SiteId != nil {
		filter.SiteID = *params.SiteId
	}
	if params.OrganizationId != nil {
		filter.OrganizationID = *params.OrganizationId
	}
	return filter
}

// MoveDeviceOrganization implements StrictServerInterface. Reassigning a device
// to another customer changes who it is billed and reported under, so it sits
// behind the admin gate.
func (s *Server) MoveDeviceOrganization(ctx context.Context, request MoveDeviceOrganizationRequestObject) (MoveDeviceOrganizationResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, MoveDeviceOrganization403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	switch err := s.devices.UpdateOrganization(ctx, request.Id, request.Body.OrganizationId); {
	case err == nil:
	case errors.Is(err, device.ErrDeviceNotFound):
		return MoveDeviceOrganization404JSONResponse{Error: msgDeviceNotFound}, nil
	case errors.Is(err, device.ErrOrganizationNotFound):
		return MoveDeviceOrganization404JSONResponse{Error: msgOrganizationNotFound}, nil
	default:
		return nil, err
	}

	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "device.move-organization", request.Id.String(), request.Body.OrganizationId.String())
	return MoveDeviceOrganization200JSONResponse(deviceToAPI(d)), nil
}

// GetDevice implements StrictServerInterface.
func (s *Server) GetDevice(ctx context.Context, request GetDeviceRequestObject) (GetDeviceResponseObject, error) {
	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDevice404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	apiDevice := deviceToAPI(d)
	single := []Device{apiDevice}
	s.enrichAnomalyRates(ctx, single)
	return GetDevice200JSONResponse(single[0]), nil
}
