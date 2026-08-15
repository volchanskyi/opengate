package lifecycle

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// InvestigationPurger erases a subject's alerts and repairs what no foreign key
// can: the counts on the incidents those alerts folded into, and an incident
// left holding nothing at all.
type InvestigationPurger interface {
	// EraseDeviceAlerts removes one machine's alerts and evidence, restates the
	// counts on every incident it was in, and closes the ones it emptied.
	EraseDeviceAlerts(ctx context.Context, tenantID, deviceID uuid.UUID) error
	// EraseTenantInvestigations removes a tenant's alerts, incidents and
	// incident history outright.
	EraseTenantInvestigations(ctx context.Context, tenantID uuid.UUID) error
}

// PGPurger removes a purge subject's Postgres rows. Deleting a device row
// cascades to device_processes, device_inventory, rule_coverage_unsupported and
// alerts via ON DELETE CASCADE.
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

// PostgresPurger is the Postgres-backed PGPurger. It runs under an admin-scoped
// tenant transaction so a server-side purge can act on any tenant's rows while
// still passing through RLS.
type PostgresPurger struct {
	db *sql.DB
	// investigations repairs the incident bookkeeping the cascade cannot.
	// Optional: nil leaves the cascade to erase the alerts on its own, which
	// still removes the data but leaves the counts describing machines that are
	// gone.
	investigations InvestigationPurger
}

// NewPostgresPurger returns a PGPurger over db. investigations may be nil.
func NewPostgresPurger(db *sql.DB, investigations InvestigationPurger) *PostgresPurger {
	return &PostgresPurger{db: db, investigations: investigations}
}

// DeleteDevice implements PGPurger.
func (p *PostgresPurger) DeleteDevice(ctx context.Context, tenantID, deviceID uuid.UUID) error {
	// Before the device row goes, not after: once the cascade has taken the
	// alerts there is nothing left to say which incidents the machine was in, so
	// the counts on them could never be restated. Failing here leaves the device
	// row standing and the resumed job simply runs both again.
	if p.investigations != nil {
		if err := p.investigations.EraseDeviceAlerts(ctx, tenantID, deviceID); err != nil {
			return fmt.Errorf("erase device investigations: %w", err)
		}
	}
	ctx = dbtx.WithTenant(ctx, tenantID, true)
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		// Idempotent: a resumed purge whose device row is already gone deletes zero
		// rows and succeeds. The cascade removes the machine's telemetry,
		// inventory and rule-coverage rows with it.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM devices WHERE tenant_id = $1 AND id = $2`, tenantID, deviceID); err != nil {
			return fmt.Errorf("delete device row: %w", err)
		}
		return nil
	})
}

// DeleteTenantDevices implements PGPurger.
func (p *PostgresPurger) DeleteTenantDevices(ctx context.Context, tenantID uuid.UUID) (int, error) {
	// The tenant row is retained as the anchor for the retained audit trail, so
	// nothing cascades from it: a tenant's incidents outlive every machine
	// beneath them unless they are erased by name.
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
