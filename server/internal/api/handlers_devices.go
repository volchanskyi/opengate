package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// ListDevices implements StrictServerInterface.
func (s *Server) ListDevices(ctx context.Context, request ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	devices, err := s.store.ListDevices(ctx, request.Params.GroupId)
	if err != nil {
		return nil, err
	}

	return ListDevices200JSONResponse(devicesToAPI(devices)), nil
}

// GetDevice implements StrictServerInterface.
func (s *Server) GetDevice(ctx context.Context, request GetDeviceRequestObject) (GetDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	return GetDevice200JSONResponse(deviceToAPI(device)), nil
}

// DeleteDevice implements StrictServerInterface.
func (s *Server) DeleteDevice(ctx context.Context, request DeleteDeviceRequestObject) (DeleteDeviceResponseObject, error) {
	if err := s.store.DeleteDevice(ctx, request.Id); err != nil {
		return nil, err
	}

	return DeleteDevice204Response{}, nil
}
