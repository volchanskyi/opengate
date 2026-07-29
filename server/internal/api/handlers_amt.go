package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
)

// powerActionMap maps OpenAPI enum strings to WSMAN PowerState int values.
var powerActionMap = map[AMTPowerRequestAction]int{
	PowerOn:    int(wsman.PowerOn),
	PowerCycle: int(wsman.PowerCycle),
	SoftOff:    int(wsman.SoftOff),
	HardReset:  int(wsman.HardReset),
}

// AmtPowerAction sends a power command to a connected AMT device.
func (s *Server) AmtPowerAction(ctx context.Context, request AmtPowerActionRequestObject) (AmtPowerActionResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, AmtPowerAction403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
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
