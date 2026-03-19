package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// ListDevices implements StrictServerInterface.
func (s *Server) ListDevices(ctx context.Context, request ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	var devices []*db.Device
	var err error
	if request.Params.GroupId != nil {
		if !s.isGroupOwner(ctx, *request.Params.GroupId) {
			return ListDevices403JSONResponse{Error: msgForbidden}, nil
		}
		devices, err = s.store.ListDevices(ctx, *request.Params.GroupId)
	} else if isAdmin(ctx) {
		devices, err = s.store.ListAllDevices(ctx)
	} else {
		devices, err = s.store.ListDevicesForOwner(ctx, ContextUserID(ctx))
	}
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

	if !s.isGroupOwner(ctx, device.GroupID) {
		return GetDevice403JSONResponse{Error: msgForbidden}, nil
	}

	return GetDevice200JSONResponse(deviceToAPI(device)), nil
}

// UpdateDevice implements StrictServerInterface.
func (s *Server) UpdateDevice(ctx context.Context, request UpdateDeviceRequestObject) (UpdateDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return UpdateDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return UpdateDevice403JSONResponse{Error: msgForbidden}, nil
	}

	if request.Body.GroupId != nil {
		newGroupID := *request.Body.GroupId
		// Verify the target group exists and the user owns it.
		if _, err := s.store.GetGroup(ctx, newGroupID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return UpdateDevice400JSONResponse{Error: "target group not found"}, nil
			}
			return nil, err
		}
		if !s.isGroupOwner(ctx, newGroupID) {
			return UpdateDevice403JSONResponse{Error: msgForbidden}, nil
		}
		if err := s.store.UpdateDeviceGroup(ctx, request.Id, newGroupID); err != nil {
			return nil, err
		}
	}

	updated, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		return nil, err
	}

	return UpdateDevice200JSONResponse(deviceToAPI(updated)), nil
}

// DeleteDevice implements StrictServerInterface.
func (s *Server) DeleteDevice(ctx context.Context, request DeleteDeviceRequestObject) (DeleteDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return DeleteDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return DeleteDevice403JSONResponse{Error: msgForbidden}, nil
	}

	if err := s.store.DeleteDevice(ctx, request.Id); err != nil {
		return nil, err
	}

	return DeleteDevice204Response{}, nil
}
