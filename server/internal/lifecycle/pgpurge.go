package lifecycle

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PGPurger removes a purge subject's Postgres rows. Deleting a device row
// cascades to device_processes and device_inventory via ON DELETE CASCADE.
type PGPurger interface {
	// DeleteDevice removes one device row (cascading its telemetry) in an org.
	DeleteDevice(ctx context.Context, orgID, deviceID uuid.UUID) error
	// DeleteOrgDevices removes every device row in an org and returns the count.
	DeleteOrgDevices(ctx context.Context, orgID uuid.UUID) (int, error)
	// ListOrgDeviceIDs returns every device id in an org (for edge deregistration
	// and verification).
	ListOrgDeviceIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
	// ListAllDeviceIDs returns every device id across all orgs, for the
	// reconciliation sweep to detect orphaned telemetry.
	ListAllDeviceIDs(ctx context.Context) ([]uuid.UUID, error)
}

// PostgresPurger is the Postgres-backed PGPurger. It runs under an admin-scoped
// tenant transaction so a server-side purge can act on any org's rows while
// still passing through RLS.
type PostgresPurger struct {
	db *sql.DB
}

// NewPostgresPurger returns a PGPurger over db.
func NewPostgresPurger(db *sql.DB) *PostgresPurger {
	return &PostgresPurger{db: db}
}

// DeleteDevice implements PGPurger.
func (p *PostgresPurger) DeleteDevice(ctx context.Context, orgID, deviceID uuid.UUID) error {
	ctx = dbtx.WithTenant(ctx, orgID, true)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// Idempotent: a resumed purge whose device row is already gone deletes zero
		// rows and succeeds. The cascade removes device_processes/device_inventory.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM devices WHERE org_id = $1 AND id = $2`, orgID, deviceID); err != nil {
			return fmt.Errorf("delete device row: %w", err)
		}
		return nil
	})
}

// DeleteOrgDevices implements PGPurger.
func (p *PostgresPurger) DeleteOrgDevices(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = dbtx.WithTenant(ctx, orgID, true)
	var count int
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE org_id = $1`, orgID)
		if err != nil {
			return fmt.Errorf("delete org devices: %w", err)
		}
		n, _ := res.RowsAffected()
		count = int(n)
		return nil
	})
	return count, err
}

// ListOrgDeviceIDs implements PGPurger.
func (p *PostgresPurger) ListOrgDeviceIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	ctx = dbtx.WithTenant(ctx, orgID, true)
	var ids []uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM devices WHERE org_id = $1`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list org device ids: %w", err)
	}
	return ids, nil
}

// ListAllDeviceIDs implements PGPurger.
func (p *PostgresPurger) ListAllDeviceIDs(ctx context.Context) ([]uuid.UUID, error) {
	ctx = dbtx.WithDefaultTenant(ctx, true)
	var ids []uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM devices`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list all device ids: %w", err)
	}
	return ids, nil
}
