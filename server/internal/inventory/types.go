// Package inventory owns Edge Sentinel server-side auto-discovery persistence:
// a device's discovered footprint (ports, services, DB engines, containers,
// packages) in a tenant-scoped Postgres RLS table. It holds descriptive,
// relational attack-surface data only — never a VictoriaMetrics label and never
// a connection string or credential.
package inventory

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Component kinds. These mirror the WS-16 DiscoveryReport categories and are the
// only values the device_inventory.kind CHECK constraint accepts.
const (
	KindPort      = "port"
	KindService   = "service"
	KindDBEngine  = "db_engine"
	KindContainer = "container"
	KindPackage   = "package"
)

// Component is one discovered inventory component for a device. Fields not
// applicable to a kind stay empty/zero (e.g. a package carries no port; a
// service carries no image). Name is the component's primary label: the owning
// process for a port, the unit for a service, the engine for a DB engine, and
// the package/container name otherwise.
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
	// Replace records the components of one discovery scan as the device's
	// current footprint: it upserts each component (advancing last_seen while
	// preserving first_seen) and prunes components absent from this scan, so the
	// stored rows always reflect the latest scan. An empty component list is a
	// no-op so a collector hiccup cannot erase the last known footprint.
	Replace(ctx context.Context, deviceID uuid.UUID, ts time.Time, components []Component) error
	// ListForDevice returns the current inventory rows for a device in the
	// caller's tenant, ordered by kind then name.
	ListForDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]Component, error)
}
