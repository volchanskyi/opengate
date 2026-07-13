// Package lifecycle owns Edge Sentinel right-to-be-forgotten data erasure: a
// persisted tombstone/deny-list, a resumable purge orchestrator that fans a
// deletion out across every telemetry store (VictoriaMetrics series, Postgres
// descriptive tables, optional cold-tier objects) and verifies emptiness, and a
// reconciliation sweep that garbage-collects orphaned series. The orchestrator
// runs server-side only: the VM delete key and object-store credentials never
// reach the edge.
package lifecycle

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Scope distinguishes a single-device purge from a whole-org (tenant/fleet)
// purge.
type Scope string

const (
	// ScopeDevice purges one device's telemetry.
	ScopeDevice Scope = "device"
	// ScopeOrg purges every device in an organization.
	ScopeOrg Scope = "org"
)

// PurgeState is the operator-visible deletion state machine. "Logical" completion
// (ingest blocked and the subject no longer queryable) is distinct from
// "physical" completion: VM delete-series is async and does not free disk until
// later merges, and edge erasure waits for the agent to reconnect.
type PurgeState string

const (
	// StateRequested is the initial state: the job row and tombstone exist, no
	// store has been touched yet.
	StateRequested PurgeState = "requested"
	// StateCentralLogicalComplete means ingest is blocked and central stores no
	// longer return the subject's data (VM delete issued, Postgres rows removed).
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
	// StateComplete means every central store is verified empty. Offline-edge
	// erasure, if still pending, is harmless: the tombstone rejects the subject at
	// ingest regardless.
	StateComplete PurgeState = "complete"
)

// Tombstone is one entry in the persisted deny-list.
type Tombstone struct {
	OrgID     uuid.UUID
	DeviceID  *uuid.UUID // nil for an org-wide tombstone
	Scope     Scope
	DeletedAt time.Time
}

// PurgeJob is the persisted, resumable record of one purge and its per-store
// progress.
type PurgeJob struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	DeviceID      *uuid.UUID // nil for an org-wide purge
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
// implementation always scopes the selector to org_id server-side.
type SeriesPurger interface {
	// DeleteSeries issues an async delete-series for the org (and device, when
	// non-nil). It returns once VM has accepted the request.
	DeleteSeries(ctx context.Context, orgID uuid.UUID, deviceID *uuid.UUID) error
	// CountSeries returns how many series still match the subject selector, used
	// to verify emptiness before a job may complete.
	CountSeries(ctx context.Context, orgID uuid.UUID, deviceID *uuid.UUID) (int, error)
}

// ObjectPurger deletes cold-tier object prefixes. It is optional: a deployment
// without a cold tier wires nil and the orchestrator skips the object stage.
type ObjectPurger interface {
	// DeletePrefix removes every object under the subject's prefix.
	DeletePrefix(ctx context.Context, orgID uuid.UUID, deviceID *uuid.UUID) error
}

// EdgeDeregistrar tombstones a subject in the agent server's in-memory deny-list
// and instructs any connected agent to wipe its local store and stop. The agent
// server implements it; the orchestrator calls it so a deleted device is denied
// at ingest immediately, not only after the next server restart.
type EdgeDeregistrar interface {
	// DeregisterAgent tombstones one device and deregisters it if connected.
	DeregisterAgent(ctx context.Context, deviceID uuid.UUID)
	// DeregisterOrg tombstones an org and deregisters every connected agent in it.
	DeregisterOrg(ctx context.Context, orgID uuid.UUID)
}
