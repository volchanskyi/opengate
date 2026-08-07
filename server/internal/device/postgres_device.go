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
const deviceSelect = `SELECT d.id, d.organization_id, d.group_id, d.hostname, d.os, d.os_display, d.agent_version, d.capabilities, d.status, d.last_seen, d.created_at, d.updated_at,
	        d.maintenance_on, d.maintenance_since, d.maintenance_by, d.maintenance_reason,
	        h.amt_available, a.status, a.uuid
	 FROM devices d
	 LEFT JOIN device_hardware h ON h.device_id = d.id
	 LEFT JOIN amt_devices a ON a.device_id = d.id `

// Every device read is a fixed statement built at compile time from
// deviceSelect; nothing here is assembled from runtime input.
const (
	getDeviceQuery = deviceSelect +
		`WHERE d.tenant_id = current_setting('app.current_tenant')::uuid AND d.id = $1`

	// The four list statements are the four shapes of Filter, each a fixed
	// statement rather than a predicate assembled at call time. A zero field
	// drops out of the WHERE clause by choosing a different statement, so
	// nothing here is built from runtime input.
	listDevicesQuery = deviceSelect +
		`WHERE d.tenant_id = current_setting('app.current_tenant')::uuid
		 ORDER BY d.hostname`

	listDevicesByGroupQuery = deviceSelect +
		`WHERE d.tenant_id = current_setting('app.current_tenant')::uuid AND d.group_id = $1
		 ORDER BY d.hostname`

	listDevicesByOrganizationQuery = deviceSelect +
		`WHERE d.tenant_id = current_setting('app.current_tenant')::uuid AND d.organization_id = $1
		 ORDER BY d.hostname`

	listDevicesByGroupAndOrganizationQuery = deviceSelect +
		`WHERE d.tenant_id = current_setting('app.current_tenant')::uuid
		   AND d.group_id = $1 AND d.organization_id = $2
		 ORDER BY d.hostname`

	getDeviceByAMTUUIDQuery = deviceSelect +
		`WHERE d.tenant_id = current_setting('app.current_tenant')::uuid AND a.uuid = $1`
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
	if err := sc.Scan(&d.ID, &d.OrganizationID, &groupID, &d.Hostname, &d.OS, &d.OsDisplay, &d.AgentVersion, &capsJSON, &d.Status, &d.LastSeen, &d.CreatedAt, &d.UpdatedAt,
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

// TenantForDevice resolves the owning tenant for a device id in the current
// tenant scope. Internal agent-ingest code calls this with an admin-scoped
// tenant so the subsequent control loop can run as the device's actual tenant.
func (p *PostgresDevices) TenantForDevice(ctx context.Context, id DeviceID) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT tenant_id
			 FROM devices
			 WHERE id = $1
			   AND (tenant_id = current_setting('app.current_tenant')::uuid OR current_setting('app.is_admin', true)::boolean)`,
			id).Scan(&tenantID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrDeviceNotFound
	}
	return tenantID, err
}

// List implements Repository. The tenant is the wall, so it is in every one of
// the four statements; the filter fields narrow inside it.
func (p *PostgresDevices) List(ctx context.Context, filter Filter) ([]*Device, error) {
	query, args := listStatementFor(filter)
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx, query, args...)
		return err
	})
	return devices, err
}

// listStatementFor picks the fixed statement matching which filter fields are
// set, and the arguments that go with it.
func listStatementFor(filter Filter) (string, []any) {
	hasGroup := filter.GroupID != uuid.Nil
	hasOrganization := filter.OrganizationID != uuid.Nil
	switch {
	case hasGroup && hasOrganization:
		return listDevicesByGroupAndOrganizationQuery, []any{filter.GroupID, filter.OrganizationID}
	case hasGroup:
		return listDevicesByGroupQuery, []any{filter.GroupID}
	case hasOrganization:
		return listDevicesByOrganizationQuery, []any{filter.OrganizationID}
	default:
		return listDevicesQuery, nil
	}
}

func (p *PostgresDevices) GetByAMTUUID(ctx context.Context, amtUUID uuid.UUID) (*Device, error) {
	var d *Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		d, err = scanDevice(tx.QueryRowContext(ctx, getDeviceByAMTUUIDQuery, amtUUID))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceNotFound
	}
	return d, err
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
