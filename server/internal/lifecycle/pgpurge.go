package lifecycle

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// InvestigationPurger erases a subject's alerts and repairs the incident counts they fed.
type InvestigationPurger interface {
	// EraseDeviceAlerts removes one machine's alerts and evidence, restates the
	// counts on every incident it was in, and closes the ones it emptied.
	EraseDeviceAlerts(ctx context.Context, tenantID, deviceID uuid.UUID) error
	// EraseTenantInvestigations removes a tenant's alerts, incidents and
	// incident history outright.
	EraseTenantInvestigations(ctx context.Context, tenantID uuid.UUID) error
}

// PGPurger removes a purge subject's Postgres rows; a device row cascades to its dependent tables.
type PGPurger interface {
	// DeleteDevice removes one device row (cascading its telemetry) in a tenant.
	DeleteDevice(ctx context.Context, tenantID, deviceID uuid.UUID) error
	// DeleteTenantDevices removes every device row in a tenant and returns the count.
	DeleteTenantDevices(ctx context.Context, tenantID uuid.UUID) (int, error)
	// ListTenantDeviceIDs returns every device id in a tenant (for edge deregistration
	// and verification).
	ListTenantDeviceIDs(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error)
	// ListAllDeviceIDs returns every device id across all tenants, for the
	// reconciliation sweep to detect orphaned telemetry.
	ListAllDeviceIDs(ctx context.Context) ([]uuid.UUID, error)
}

// PostgresPurger is the Postgres-backed PGPurger, run under an admin-scoped tenant transaction.
type PostgresPurger struct {
	db *sql.DB
	// investigations is optional; nil leaves incident counts describing erased machines.
	investigations InvestigationPurger
}

// NewPostgresPurger returns a PGPurger over db. investigations may be nil.
func NewPostgresPurger(db *sql.DB, investigations InvestigationPurger) *PostgresPurger {
	return &PostgresPurger{db: db, investigations: investigations}
}

// DeleteDevice implements PGPurger.
func (p *PostgresPurger) DeleteDevice(ctx context.Context, tenantID, deviceID uuid.UUID) error {
	// The cascade deletes the alerts that name the affected incidents, so they are erased first.
	if p.investigations != nil {
		if err := p.investigations.EraseDeviceAlerts(ctx, tenantID, deviceID); err != nil {
			return fmt.Errorf("erase device investigations: %w", err)
		}
	}
	ctx = dbtx.WithTenant(ctx, tenantID, true)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// A device row already gone deletes zero rows, so a resumed purge succeeds.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM devices WHERE tenant_id = $1 AND id = $2`, tenantID, deviceID); err != nil {
			return fmt.Errorf("delete device row: %w", err)
		}
		return nil
	})
}

// DeleteTenantDevices implements PGPurger.
func (p *PostgresPurger) DeleteTenantDevices(ctx context.Context, tenantID uuid.UUID) (int, error) {
	// The tenant row stays as the audit-trail anchor, so incidents are erased explicitly.
	if p.investigations != nil {
		if err := p.investigations.EraseTenantInvestigations(ctx, tenantID); err != nil {
			return 0, fmt.Errorf("erase tenant investigations: %w", err)
		}
	}
	ctx = dbtx.WithTenant(ctx, tenantID, true)
	var count int
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE tenant_id = $1`, tenantID)
		if err != nil {
			return fmt.Errorf("delete tenant devices: %w", err)
		}
		n, _ := res.RowsAffected()
		count = int(n)
		return nil
	})
	return count, err
}

// ListTenantDeviceIDs implements PGPurger.
func (p *PostgresPurger) ListTenantDeviceIDs(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	ctx = dbtx.WithTenant(ctx, tenantID, true)
	var ids []uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM devices WHERE tenant_id = $1`, tenantID)
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
		return nil, fmt.Errorf("list tenant device ids: %w", err)
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
