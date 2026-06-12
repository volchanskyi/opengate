package amt

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// ErrAMTDeviceNotFound is returned when a Get / SetStatus operation targets
// an AMT device that does not exist.
var ErrAMTDeviceNotFound = errors.New("amt device not found")

// Repository is the outbound persistence port for AMT device records.
// The interface lives with the consuming module (amt); the
// Postgres adapter lives alongside in this package.
//
// The AMTDevice and DeviceStatus types deliberately remain in [db] for this
// extraction round — moving them here would create a cycle with the mps
// package (which calls Upsert/SetStatus and is itself a dependency of
// amt.Service). Consolidate the types only when transport ownership can move
// without creating a dependency cycle.
type Repository interface {
	Upsert(ctx context.Context, d *db.AMTDevice) error
	Get(ctx context.Context, id uuid.UUID) (*db.AMTDevice, error)
	List(ctx context.Context) ([]*db.AMTDevice, error)
	SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error
}
