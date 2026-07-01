package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/device"
)

// ListDevices implements StrictServerInterface.
func (s *Server) ListDevices(ctx context.Context, request ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	var devices []*device.Device
	var err error
	if request.Params.GroupId != nil {
		if !s.isGroupOwner(ctx, *request.Params.GroupId) {
			return ListDevices403JSONResponse{Error: msgForbidden}, nil
		}
		devices, err = s.devices.List(ctx, *request.Params.GroupId)
	} else if isAdmin(ctx) {
		devices, err = s.devices.ListAll(ctx)
	} else {
		devices, err = s.devices.ListForOwner(ctx, ContextUserID(ctx))
	}
	if err != nil {
		return nil, err
	}

	return ListDevices200JSONResponse(devicesToAPI(devices)), nil
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

	if !s.isGroupOwner(ctx, d.GroupID) {
		return GetDevice403JSONResponse{Error: msgForbidden}, nil
	}

	return GetDevice200JSONResponse(deviceToAPI(d)), nil
}
