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

func scanDevice(sc interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var groupID uuid.NullUUID
	var capsJSON []byte
	if err := sc.Scan(&d.ID, &groupID, &d.Hostname, &d.OS, &d.OsDisplay, &d.AgentVersion, &capsJSON, &d.Status, &d.LastSeen, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	if groupID.Valid {
		d.GroupID = groupID.UUID
	}
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

func (p *PostgresDevices) Get(ctx context.Context, id DeviceID) (*Device, error) {
	var d *Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		d, err = scanDevice(tx.QueryRowContext(ctx,
			`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at
			 FROM devices
			 WHERE org_id = current_setting('app.current_org')::uuid AND id = $1`,
			id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceNotFound
	}
	return d, err
}

func (p *PostgresDevices) List(ctx context.Context, groupID GroupID) ([]*Device, error) {
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx,
			`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at
			 FROM devices
			 WHERE org_id = current_setting('app.current_org')::uuid AND group_id = $1`,
			groupID)
		return err
	})
	return devices, err
}

func (p *PostgresDevices) ListAll(ctx context.Context) ([]*Device, error) {
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx,
			`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at
			 FROM devices
			 WHERE org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean
			 ORDER BY hostname`)
		return err
	})
	return devices, err
}

func (p *PostgresDevices) ListForOwner(ctx context.Context, ownerID uuid.UUID) ([]*Device, error) {
	var devices []*Device
	err := dbtx.Scoped(ctx, p.db, func(tx *sql.Tx) error {
		var err error
		devices, err = queryDevices(ctx, tx,
			`SELECT d.id, d.group_id, d.hostname, d.os, d.os_display, d.agent_version, d.capabilities, d.status, d.last_seen, d.created_at, d.updated_at
			 FROM devices d LEFT JOIN groups_ g ON d.group_id = g.id
			 WHERE d.org_id = current_setting('app.current_org')::uuid
			   AND (g.owner_id = $1 OR d.group_id IS NULL)
			 ORDER BY d.hostname`, ownerID)
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
