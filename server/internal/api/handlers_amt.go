package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/mps/wsman"
)

// powerActionMap maps OpenAPI enum strings to WSMAN PowerState int values.
var powerActionMap = map[AMTPowerRequestAction]int{
	PowerOn:    int(wsman.PowerOn),
	PowerCycle: int(wsman.PowerCycle),
	SoftOff:    int(wsman.SoftOff),
	HardReset:  int(wsman.HardReset),
}

// ListAMTDevices returns all AMT devices from the database.
func (s *Server) ListAMTDevices(ctx context.Context, _ ListAMTDevicesRequestObject) (ListAMTDevicesResponseObject, error) {
	devices, err := s.store.ListAMTDevices(ctx)
	if err != nil {
		return nil, err
	}

	result := make(ListAMTDevices200JSONResponse, 0, len(devices))
	for _, d := range devices {
		result = append(result, toAMTDeviceResponse(d))
	}
	return result, nil
}

// GetAMTDevice returns a single AMT device by UUID.
func (s *Server) GetAMTDevice(ctx context.Context, request GetAMTDeviceRequestObject) (GetAMTDeviceResponseObject, error) {
	d, err := s.store.GetAMTDevice(ctx, request.Uuid)
	if err != nil {
		return GetAMTDevice404JSONResponse{Error: "device not found"}, nil
	}
	return GetAMTDevice200JSONResponse(toAMTDeviceResponse(d)), nil
}

// AmtPowerAction sends a power command to a connected AMT device.
func (s *Server) AmtPowerAction(ctx context.Context, request AmtPowerActionRequestObject) (AmtPowerActionResponseObject, error) {
	state, ok := powerActionMap[request.Body.Action]
	if !ok {
		return AmtPowerAction409JSONResponse{Error: "unknown power action"}, nil
	}

	if err := s.amt.PowerAction(ctx, request.Uuid, state); err != nil {
		if errors.Is(err, amt.ErrDeviceNotConnected) {
			return AmtPowerAction409JSONResponse{Error: err.Error()}, nil
		}
		return nil, err
	}
	return AmtPowerAction200Response{}, nil
}

func toAMTDeviceResponse(d *db.AMTDevice) AMTDevice {
	status := AMTDeviceStatusOffline
	if d.Status == db.StatusOnline {
		status = AMTDeviceStatusOnline
	}
	return AMTDevice{
		Uuid:     d.UUID,
		Hostname: d.Hostname,
		Model:    d.Model,
		Firmware: d.Firmware,
		Status:   status,
		LastSeen: d.LastSeen,
	}
}
