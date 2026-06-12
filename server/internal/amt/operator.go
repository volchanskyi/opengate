package amt

import (
	"context"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
)

// Operator is the inbound port for high-level AMT device operations.
//
// The amt module owns this contract and the api layer consumes it. *Service is the
// canonical implementation; tests may supply their own double.
type Operator interface {
	PowerAction(ctx context.Context, amtUUID uuid.UUID, state int) error
	QueryDeviceInfo(ctx context.Context, amtUUID uuid.UUID) (*wsman.DeviceInfo, error)
	ConnectedDeviceCount() int
}
