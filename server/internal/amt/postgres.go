package amt

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresAMTDevices implements [Repository] against PostgreSQL. The db
// package owns the amt_devices schema and migrations; this adapter only
// issues queries.
type PostgresAMTDevices struct {
	db *sql.DB
}

// NewPostgresAMTDevices returns a Postgres-backed [Repository].
func NewPostgresAMTDevices(d *sql.DB) *PostgresAMTDevices {
	return &PostgresAMTDevices{db: d}
}

// Upsert records the connection state of one AMT device. The caller resolves the
// device and its organization from the CIRA system UUID first and supplies both
// on ctx, so this write always lands in the tenant that owns the machine.
func (p *PostgresAMTDevices) Upsert(ctx context.Context, d *db.AMTDevice) error {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return dbtx.ErrTenantRequired
	}
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO amt_devices (uuid, org_id, device_id, status, last_seen)
			 VALUES ($1, $2, $3, $4, NOW())
			 ON CONFLICT (uuid) DO UPDATE SET
			   org_id    = EXCLUDED.org_id,
			   device_id = EXCLUDED.device_id,
			   status    = EXCLUDED.status,
			   last_seen = NOW()`,
			d.UUID, tenant.OrgID, d.DeviceID, string(d.Status))
		return err
	})
}

func (p *PostgresAMTDevices) SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error {
	return dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE amt_devices SET status = $1, last_seen = NOW()
			 WHERE org_id = current_setting('app.current_org')::uuid AND uuid = $2`,
			string(status), id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrAMTDeviceNotFound
		}
		return nil
	})
}
