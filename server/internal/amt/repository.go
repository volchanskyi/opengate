package amt

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// ErrAMTDeviceNotFound is returned when SetStatus targets a missing AMT device.
var ErrAMTDeviceNotFound = errors.New("amt device not found")

// Repository is the persistence port for AMT connection state.
type Repository interface {
	Upsert(ctx context.Context, d *db.AMTDevice) error
	SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error
}
