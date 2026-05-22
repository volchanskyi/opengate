package amt

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/db"
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

func (p *PostgresAMTDevices) Upsert(ctx context.Context, d *db.AMTDevice) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO amt_devices (uuid, hostname, model, firmware, status, last_seen)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (uuid) DO UPDATE SET
		   hostname  = CASE WHEN EXCLUDED.hostname = '' THEN amt_devices.hostname ELSE EXCLUDED.hostname END,
		   model     = CASE WHEN EXCLUDED.model    = '' THEN amt_devices.model    ELSE EXCLUDED.model    END,
		   firmware  = CASE WHEN EXCLUDED.firmware = '' THEN amt_devices.firmware ELSE EXCLUDED.firmware END,
		   status    = EXCLUDED.status,
		   last_seen = NOW()`,
		d.UUID, d.Hostname, d.Model, d.Firmware, string(d.Status))
	return err
}

func (p *PostgresAMTDevices) Get(ctx context.Context, id uuid.UUID) (*db.AMTDevice, error) {
	var d db.AMTDevice
	err := p.db.QueryRowContext(ctx,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices WHERE uuid = $1`,
		id).Scan(&d.UUID, &d.Hostname, &d.Model, &d.Firmware, &d.Status, &d.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAMTDeviceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (p *PostgresAMTDevices) List(ctx context.Context) ([]*db.AMTDevice, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*db.AMTDevice
	for rows.Next() {
		var d db.AMTDevice
		if err := rows.Scan(&d.UUID, &d.Hostname, &d.Model, &d.Firmware, &d.Status, &d.LastSeen); err != nil {
			return nil, err
		}
		devices = append(devices, &d)
	}
	return devices, rows.Err()
}

func (p *PostgresAMTDevices) SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE amt_devices SET status = $1, last_seen = NOW() WHERE uuid = $2`,
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
}
