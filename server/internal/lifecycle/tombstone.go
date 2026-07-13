package lifecycle

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// TombstoneStore is the persisted deny-list backing every write path's
// resurrection check. It sits on the non-tenant deleted_ids table (no RLS, no
// FK to organizations) so it keeps rejecting a subject after the org's own rows
// are gone.
type TombstoneStore struct {
	db *sql.DB
}

// NewTombstoneStore returns a deny-list store over db.
func NewTombstoneStore(db *sql.DB) *TombstoneStore {
	return &TombstoneStore{db: db}
}

// TombstoneDevice records a device as deleted. It is idempotent: re-recording
// the same device (a resumed purge) is a no-op. by is the requesting user, or
// nil for a system sweep.
func (s *TombstoneStore) TombstoneDevice(ctx context.Context, orgID, deviceID uuid.UUID, by *uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deleted_ids (org_id, device_id, scope, deleted_by)
		 VALUES ($1, $2, 'device', $3)
		 ON CONFLICT (org_id, device_id) WHERE device_id IS NOT NULL DO NOTHING`,
		orgID, deviceID, nullableUUID(by))
	if err != nil {
		return fmt.Errorf("tombstone device: %w", err)
	}
	return nil
}

// TombstoneOrg records a whole organization as deleted. Idempotent.
func (s *TombstoneStore) TombstoneOrg(ctx context.Context, orgID uuid.UUID, by *uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deleted_ids (org_id, device_id, scope, deleted_by)
		 VALUES ($1, NULL, 'org', $2)
		 ON CONFLICT (org_id) WHERE device_id IS NULL DO NOTHING`,
		orgID, nullableUUID(by))
	if err != nil {
		return fmt.Errorf("tombstone org: %w", err)
	}
	return nil
}

// IsDeviceTombstoned reports whether a device is denied at ingest: either the
// device itself is tombstoned or its whole org is.
func (s *TombstoneStore) IsDeviceTombstoned(ctx context.Context, orgID, deviceID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM deleted_ids
		   WHERE org_id = $1 AND (device_id = $2 OR (scope = 'org' AND device_id IS NULL))
		 )`,
		orgID, deviceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check device tombstone: %w", err)
	}
	return exists, nil
}

// IsOrgTombstoned reports whether a whole organization is tombstoned.
func (s *TombstoneStore) IsOrgTombstoned(ctx context.Context, orgID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM deleted_ids WHERE org_id = $1 AND scope = 'org' AND device_id IS NULL
		 )`,
		orgID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check org tombstone: %w", err)
	}
	return exists, nil
}

// ListAll returns every tombstone. The agent server loads it at startup to warm
// its in-memory deny-list, and the reconciliation sweep consults it.
func (s *TombstoneStore) ListAll(ctx context.Context) ([]Tombstone, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT org_id, device_id, scope, deleted_at FROM deleted_ids ORDER BY deleted_at`)
	if err != nil {
		return nil, fmt.Errorf("list tombstones: %w", err)
	}
	defer rows.Close()

	var out []Tombstone
	for rows.Next() {
		var (
			tomb   Tombstone
			device uuid.NullUUID
			scope  string
		)
		if err := rows.Scan(&tomb.OrgID, &device, &scope, &tomb.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan tombstone: %w", err)
		}
		tomb.Scope = Scope(scope)
		if device.Valid {
			id := device.UUID
			tomb.DeviceID = &id
		}
		out = append(out, tomb)
	}
	return out, rows.Err()
}

// nullableUUID converts an optional UUID into a driver value: nil pointer maps
// to SQL NULL.
func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}
