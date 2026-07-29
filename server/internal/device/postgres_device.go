package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// PostgresDevices implements [Repository] against PostgreSQL.
type PostgresDevices struct {
	db *sql.DB
}

// NewPostgresDevices returns a Postgres-backed Repository.
func NewPostgresDevices(db *sql.DB) *PostgresDevices {
	return &PostgresDevices{db: db}
}

// deviceSelect is the projection every device read shares. The two LEFT JOINs
// carry the device's Intel AMT property: capability from the hardware row, live
// connection state from the AMT row when one is linked. Both join by primary key
// and serve the badge straight from the device payload, with no second request.
const deviceSelect = `SELECT d.id, d.group_id, d.hostname, d.os, d.os_display, d.agent_version, d.capabilities, d.status, d.last_seen, d.created_at, d.updated_at,
	        d.maintenance_on, d.maintenance_since, d.maintenance_by, d.maintenance_reason,
	        h.amt_available, a.status, a.uuid
	 FROM devices d
	 LEFT JOIN device_hardware h ON h.device_id = d.id
	 LEFT JOIN amt_devices a ON a.device_id = d.id `

// Every device read is a fixed statement built at compile time from
// deviceSelect; nothing here is assembled from runtime input.
const (
	getDeviceQuery = deviceSelect +
		`WHERE d.org_id = current_setting('app.current_org')::uuid AND d.id = $1`

	listDevicesByGroupQuery = deviceSelect +
		`WHERE d.org_id = current_setting('app.current_org')::uuid AND d.group_id = $1`

	listAllDevicesQuery = deviceSelect +
		`WHERE d.org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean
		 ORDER BY d.hostname`

	listDevicesForOwnerQuery = deviceSelect +
		`LEFT JOIN groups_ g ON d.group_id = g.id
		 WHERE d.org_id = current_setting('app.current_org')::uuid
		   AND (g.owner_id = $1 OR d.group_id IS NULL)
		 ORDER BY d.hostname`
)

func scanDevice(sc interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var groupID uuid.NullUUID
	var capsJSON []byte
	var maintSince sql.NullTime
	var maintBy uuid.NullUUID
	var amtAvailable sql.NullBool
	var amtStatus sql.NullString
	var amtUUID uuid.NullUUID
	if err := sc.Scan(&d.ID, &groupID, &d.Hostname, &d.OS, &d.OsDisplay, &d.AgentVersion, &capsJSON, &d.Status, &d.LastSeen, &d.CreatedAt, &d.UpdatedAt,
		&d.MaintenanceOn, &maintSince, &maintBy, &d.MaintenanceReason,
		&amtAvailable, &amtStatus, &amtUUID); err != nil {
		return nil, err
	}
	if groupID.Valid {
		d.GroupID = groupID.UUID
	}
	if maintSince.Valid {
		d.MaintenanceSince = &maintSince.Time
	}
	if maintBy.Valid {
		d.MaintenanceBy = &maintBy.UUID
	}
	d.AMT = buildAMT(amtAvailable, amtStatus, amtUUID)
	if len(capsJSON) > 0 {
		if err := json.Unmarshal(capsJSON, &d.Capabilities); err != nil {
			return nil, fmt.Errorf("parse capabilities: %w", err)
		}
	}
	if d.Capabilities == nil {
		d.Capabilities = []string{}
	}
	return &d, nil
}

// buildAMT assembles the AMT property from the two joined sources, or nil when
// the device neither supports AMT nor has an AMT connection — the common case,
// which keeps the field off the wire entirely.
func buildAMT(available sql.NullBool, status sql.NullString, amtUUID uuid.NullUUID) *AMT {
	supported := available.Valid && available.Bool
	if !supported && !amtUUID.Valid {
		return nil
	}
	amt := &AMT{Available: supported}
	if status.Valid {
		amt.Status = status.String
	}
	if amtUUID.Valid {
		amt.UUID = &amtUUID.UUID
	}
	return amt
}

func (p *PostgresDevices) Get(ctx context.Context, id DeviceID) (*Device, error) {
	var d *Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		d, err = scanDevice(tx.QueryRowContext(ctx, getDeviceQuery, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceNotFound
	}
	return d, err
}

// OrgForDevice resolves the owning organization for a device id in the current
// tenant scope. Internal agent-ingest code calls this with an admin-scoped
// tenant so the subsequent control loop can run as the device's actual org.
func (p *PostgresDevices) OrgForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT org_id
			 FROM devices
			 WHERE id = $1
			   AND (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)`,
			id).Scan(&orgID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrDeviceNotFound
	}
	return orgID, err
}

func (p *PostgresDevices) List(ctx context.Context, groupID GroupID) ([]*Device, error) {
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx, listDevicesByGroupQuery, groupID)
		return err
	})
	return devices, err
}

func (p *PostgresDevices) ListAll(ctx context.Context) ([]*Device, error) {
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx, listAllDevicesQuery)
		return err
	})
	return devices, err
}

func (p *PostgresDevices) ListForOwner(ctx context.Context, ownerID uuid.UUID) ([]*Device, error) {
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx, listDevicesForOwnerQuery, ownerID)
		return err
	})
	return devices, err
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryDevices(ctx context.Context, db queryer, query string, args ...any) ([]*Device, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}
