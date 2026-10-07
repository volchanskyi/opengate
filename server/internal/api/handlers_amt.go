package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// powerActionMap maps OpenAPI enum strings to WSMAN PowerState int values.
var powerActionMap = map[AMTPowerRequestAction]int{
	PowerOn:    int(wsman.PowerOn),
	PowerCycle: int(wsman.PowerCycle),
	SoftOff:    int(wsman.SoftOff),
	HardReset:  int(wsman.HardReset),
}

// AmtPowerAction sends a power command to a connected AMT device, open to any tenant member.
// The CIRA map is keyed by AMT UUID alone, so the device is resolved tenant-scoped first.
func (s *Server) AmtPowerAction(ctx context.Context, request AmtPowerActionRequestObject) (AmtPowerActionResponseObject, error) {
	if err := s.requireAMTDeviceInScope(ctx, request.Uuid); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return AmtPowerAction404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	state, ok := powerActionMap[request.Body.Action]
	if !ok {
		return AmtPowerAction409JSONResponse{Error: "unknown power action"}, nil
	}

	if err := s.amtHandlers.PowerAction(ctx, request.Uuid, state); err != nil {
		if errors.Is(err, amt.ErrDeviceNotConnected) {
			return AmtPowerAction409JSONResponse{Error: "device not connected"}, nil
		}
		return nil, err
	}
	return AmtPowerAction200Response{}, nil
}
