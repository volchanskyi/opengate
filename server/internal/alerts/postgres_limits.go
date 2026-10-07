package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

const (
	upsertLimitsSQL = `INSERT INTO organization_alert_limits
		   (tenant_id, organization_id, hourly_ceiling, device_hourly_ceiling,
		    updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, NOW(), $5)
		 ON CONFLICT (organization_id)
		 DO UPDATE SET hourly_ceiling        = EXCLUDED.hourly_ceiling,
		               device_hourly_ceiling = EXCLUDED.device_hourly_ceiling,
		               updated_at            = NOW(),
		               updated_by            = EXCLUDED.updated_by`

	readLimitsSQL = `SELECT hourly_ceiling, device_hourly_ceiling, updated_by
		   FROM organization_alert_limits
		  WHERE tenant_id = current_setting('app.current_tenant')::uuid
		    AND organization_id = $1`
)

// UpsertLimits validates and stores one customer's budget, refusing an unusable one at write time.
func (s *Store) UpsertLimits(ctx context.Context, l Limits) error {
	if err := ValidateLimits(l); err != nil {
		return err
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, upsertLimitsSQL,
			tenant.TenantID, l.OrganizationID, l.OrganizationHourly,
			l.DeviceHourly, l.UpdatedBy); err != nil {
			return fmt.Errorf("store alert limits: %w", err)
		}
		return nil
	})
}

// Limits reads one customer's budget, falling back to the shipped one when none is stored.
func (s *Store) Limits(ctx context.Context, organizationID uuid.UUID) (Limits, error) {
	limits := DefaultLimits(organizationID)
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		stored, err := limitsIn(ctx, tx, organizationID)
		if err != nil {
			return err
		}
		limits = stored
		return nil
	})
	if err != nil {
		return Limits{}, err
	}
	return limits, nil
}

// limitsIn reads the budget inside an open transaction, so an insert spends the budget in force.
func limitsIn(ctx context.Context, tx *sql.Tx, organizationID uuid.UUID) (Limits, error) {
	limits := DefaultLimits(organizationID)
	err := tx.QueryRowContext(ctx, readLimitsSQL, organizationID).
		Scan(&limits.OrganizationHourly, &limits.DeviceHourly, &limits.UpdatedBy)
	switch {
	case err == nil, errors.Is(err, sql.ErrNoRows):
		return limits, nil
	default:
		return Limits{}, fmt.Errorf("read alert limits: %w", err)
	}
}
