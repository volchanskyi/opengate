package amt

import (
	"context"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
)

// Operator is the inbound port for high-level AMT device operations, implemented by *Service.
type Operator interface {
	PowerAction(ctx context.Context, amtUUID uuid.UUID, state int) error
	QueryDeviceInfo(ctx context.Context, amtUUID uuid.UUID) (*wsman.DeviceInfo, error)
	ConnectedDeviceCount() int
}
