package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/device"
)

// ListDevices implements StrictServerInterface. The repository predicate is the
// whole gate: a caller sees its own tenant's devices, and an admin sees
// every tenant's. Narrowing to a group is a filter, not a permission.
func (s *Server) ListDevices(ctx context.Context, request ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	var devices []*device.Device
	var err error
	if request.Params.GroupId != nil {
		devices, err = s.devices.List(ctx, *request.Params.GroupId)
	} else {
		devices, err = s.devices.ListAll(ctx)
	}
	if err != nil {
		return nil, err
	}

	apiDevices := devicesToAPI(devices)
	s.enrichAnomalyRates(ctx, apiDevices)
	return ListDevices200JSONResponse(apiDevices), nil
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
