// Package lifecycle owns data erasure: a tombstone deny-list, a resumable purge orchestrator and
// a reconciliation sweep. It runs server-side, so delete credentials never reach the edge.
package lifecycle

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Scope distinguishes a single-device purge from a whole-tenant (tenant/fleet)
// purge.
type Scope string

const (
	// ScopeDevice purges one device's telemetry.
	ScopeDevice Scope = "device"
	// ScopeTenant purges every device in a tenant.
	ScopeTenant Scope = "tenant"
)

// PurgeState is the operator-visible deletion state machine; logical completion (ingest blocked)
// precedes physical completion (VM frees disk on merge, edge wipes on reconnect).
type PurgeState string

const (
	// StateRequested is the initial state: the job row and tombstone exist, no
	// store has been touched yet.
	StateRequested PurgeState = "requested"
	// StateCentralLogicalComplete means ingest is blocked and the VM delete is issued.
	StateCentralLogicalComplete PurgeState = "central-logical-complete"
	// StateCentralPhysicalPending means the central logical delete is done but VM
	// has not yet compacted the series off disk; verification is polling.
	StateCentralPhysicalPending PurgeState = "central-physical-compaction-pending"
	// StateObjectDeletePending means cold-tier object prefixes are still being
	// removed.
	StateObjectDeletePending PurgeState = "object-delete-pending"
	// StateEdgeErasePending means central erasure is verified but the agent has
	// not yet acknowledged wiping its local store (pending reconnect).
	StateEdgeErasePending PurgeState = "edge-erase-pending"
	// StateComplete means every central store is verified empty; a pending offline-edge erasure
	// is harmless because the tombstone rejects the subject at ingest.
	StateComplete PurgeState = "complete"
)

// Tombstone is one entry in the persisted deny-list.
type Tombstone struct {
	TenantID  uuid.UUID
	DeviceID  *uuid.UUID // nil for a tenant-wide tombstone
	Scope     Scope
	DeletedAt time.Time
}

// PurgeJob is the persisted, resumable record of one purge and its per-store
// progress.
type PurgeJob struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	DeviceID      *uuid.UUID // nil for a tenant-wide purge
	Scope         Scope
	State         PurgeState
	VMDeleted     bool
	ObjectDeleted bool
	PGDeleted     bool
	Verified      bool
	RequestedBy   *uuid.UUID
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

// SeriesPurger deletes and counts VictoriaMetrics series for a subject. The
// implementation always scopes the selector to tenant_id server-side.
type SeriesPurger interface {
	// DeleteSeries issues an async delete-series for the tenant (and device, when
	// non-nil). It returns once VM has accepted the request.
	DeleteSeries(ctx context.Context, tenantID uuid.UUID, deviceID *uuid.UUID) error
	// CountSeries returns how many series still match the subject selector, used
	// to verify emptiness before a job may complete.
	CountSeries(ctx context.Context, tenantID uuid.UUID, deviceID *uuid.UUID) (int, error)
}

// ObjectPurger deletes cold-tier object prefixes. It is optional: a deployment
// without a cold tier wires nil and the orchestrator skips the object stage.
type ObjectPurger interface {
	// DeletePrefix removes every object under the subject's prefix.
	DeletePrefix(ctx context.Context, tenantID uuid.UUID, deviceID *uuid.UUID) error
}

// EdgeDeregistrar tombstones a subject in the agent server's in-memory deny-list and tells any
// connected agent to wipe its local store, so a deleted device is denied at ingest at once.
type EdgeDeregistrar interface {
	// DeregisterAgent tombstones one device and deregisters it if connected.
	DeregisterAgent(ctx context.Context, deviceID uuid.UUID)
	// DeregisterTenant tombstones a tenant and deregisters every connected agent in it.
	DeregisterTenant(ctx context.Context, tenantID uuid.UUID)
}
