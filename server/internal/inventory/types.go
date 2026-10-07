// Package inventory persists a device's discovered footprint in a tenant-scoped Postgres table
// holding descriptive data only, never a credential or connection string.
package inventory

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Component kinds, the only values the device_inventory.kind CHECK constraint accepts.
const (
	KindPort      = "port"
	KindService   = "service"
	KindDBEngine  = "db_engine"
	KindContainer = "container"
	KindPackage   = "package"
)

// Component is one discovered inventory component; Name is its primary label (process, unit,
// engine, package or container name by kind).
type Component struct {
	Kind      string
	Name      string
	Version   string
	Port      uint16
	Proto     string
	State     string
	Runtime   string
	Image     string
	FirstSeen time.Time
	LastSeen  time.Time
}

// Repository persists and reads a device's tenant-scoped discovered inventory.
type Repository interface {
	// Replace upserts the scan's components as the device's footprint and prunes absent ones;
	// an empty list is a no-op.
	Replace(ctx context.Context, deviceID uuid.UUID, ts time.Time, components []Component) error
	// ListForDevice returns the current inventory rows for a device in the
	// caller's tenant, ordered by kind then name.
	ListForDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]Component, error)
}
