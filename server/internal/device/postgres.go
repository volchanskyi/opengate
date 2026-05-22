package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// --- Devices -----------------------------------------------------------------

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

func (p *PostgresDevices) Upsert(ctx context.Context, d *Device) error {
	var groupID any
	if d.GroupID != uuid.Nil {
		groupID = d.GroupID
	}
	caps := d.Capabilities
	if caps == nil {
		caps = []string{}
	}
	capsJSON, err := json.Marshal(caps)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW(), NOW())
		 ON CONFLICT (id) DO UPDATE SET
		   group_id = COALESCE(EXCLUDED.group_id, devices.group_id),
		   hostname = EXCLUDED.hostname,
		   os = EXCLUDED.os,
		   os_display = EXCLUDED.os_display,
		   agent_version = EXCLUDED.agent_version,
		   capabilities = EXCLUDED.capabilities,
		   status = EXCLUDED.status,
		   last_seen = NOW(),
		   updated_at = NOW()`,
		d.ID, groupID, d.Hostname, d.OS, d.OsDisplay, d.AgentVersion, capsJSON, string(d.Status))
	return err
}

func (p *PostgresDevices) Get(ctx context.Context, id DeviceID) (*Device, error) {
	d, err := scanDevice(p.db.QueryRowContext(ctx,
		`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at FROM devices WHERE id = $1`,
		id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceNotFound
	}
	return d, err
}

func (p *PostgresDevices) List(ctx context.Context, groupID GroupID) ([]*Device, error) {
	return queryDevices(ctx, p.db,
		`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at FROM devices WHERE group_id = $1`,
		groupID)
}

func (p *PostgresDevices) ListAll(ctx context.Context) ([]*Device, error) {
	return queryDevices(ctx, p.db,
		`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at FROM devices ORDER BY hostname`)
}

func (p *PostgresDevices) ListForOwner(ctx context.Context, ownerID uuid.UUID) ([]*Device, error) {
	return queryDevices(ctx, p.db,
		`SELECT d.id, d.group_id, d.hostname, d.os, d.os_display, d.agent_version, d.capabilities, d.status, d.last_seen, d.created_at, d.updated_at
		 FROM devices d LEFT JOIN groups_ g ON d.group_id = g.id
		 WHERE g.owner_id = $1 OR d.group_id IS NULL
		 ORDER BY d.hostname`, ownerID)
}

func (p *PostgresDevices) Delete(ctx context.Context, id DeviceID) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM devices WHERE id = $1`, id)
	return checkAffected(res, err, ErrDeviceNotFound)
}

func (p *PostgresDevices) UpdateGroup(ctx context.Context, id DeviceID, groupID GroupID) error {
	var gid any
	if groupID != uuid.Nil {
		gid = groupID
	}
	res, err := p.db.ExecContext(ctx,
		`UPDATE devices SET group_id = $1, updated_at = NOW() WHERE id = $2`, gid, id)
	return checkAffected(res, err, ErrDeviceNotFound)
}

func (p *PostgresDevices) SetStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE devices SET status = $1, last_seen = NOW(), updated_at = NOW() WHERE id = $2`,
		string(status), id)
	return checkAffected(res, err, ErrDeviceNotFound)
}

func (p *PostgresDevices) ResetAllStatuses(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE devices SET status = $1, updated_at = NOW() WHERE status = $2`,
		string(StatusOffline), string(StatusOnline))
	return err
}

// --- Groups ------------------------------------------------------------------

// PostgresGroups implements [GroupRepository] against PostgreSQL.
type PostgresGroups struct {
	db *sql.DB
}

// NewPostgresGroups returns a Postgres-backed GroupRepository.
func NewPostgresGroups(db *sql.DB) *PostgresGroups {
	return &PostgresGroups{db: db}
}

func (p *PostgresGroups) Create(ctx context.Context, g *Group) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO groups_ (id, name, owner_id, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`,
		g.ID, g.Name, g.OwnerID)
	return err
}

func (p *PostgresGroups) Get(ctx context.Context, id GroupID) (*Group, error) {
	var g Group
	err := p.db.QueryRowContext(ctx,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE id = $1`, id).
		Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (p *PostgresGroups) List(ctx context.Context, ownerID uuid.UUID) ([]*Group, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE owner_id = $1`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, &g)
	}
	return groups, rows.Err()
}

func (p *PostgresGroups) Delete(ctx context.Context, id GroupID) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM groups_ WHERE id = $1`, id)
	return checkAffected(res, err, ErrGroupNotFound)
}

// --- Device Hardware ---------------------------------------------------------

// PostgresHardware implements [HardwareRepository] against PostgreSQL.
type PostgresHardware struct {
	db *sql.DB
}

// NewPostgresHardware returns a Postgres-backed HardwareRepository.
func NewPostgresHardware(db *sql.DB) *PostgresHardware {
	return &PostgresHardware{db: db}
}

func (p *PostgresHardware) Upsert(ctx context.Context, hw *Hardware) error {
	niJSON, err := json.Marshal(hw.NetworkInterfaces)
	if err != nil {
		return fmt.Errorf("marshal network interfaces: %w", err)
	}
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO device_hardware (device_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		 ON CONFLICT (device_id) DO UPDATE SET
		   cpu_model = EXCLUDED.cpu_model,
		   cpu_cores = EXCLUDED.cpu_cores,
		   ram_total_mb = EXCLUDED.ram_total_mb,
		   disk_total_mb = EXCLUDED.disk_total_mb,
		   disk_free_mb = EXCLUDED.disk_free_mb,
		   network_interfaces = EXCLUDED.network_interfaces,
		   updated_at = NOW()`,
		hw.DeviceID, hw.CPUModel, hw.CPUCores,
		hw.RAMTotalMB, hw.DiskTotalMB, hw.DiskFreeMB,
		niJSON)
	return err
}

func (p *PostgresHardware) Get(ctx context.Context, deviceID DeviceID) (*Hardware, error) {
	var hw Hardware
	var niJSON []byte
	err := p.db.QueryRowContext(ctx,
		`SELECT device_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces, updated_at
		 FROM device_hardware WHERE device_id = $1`, deviceID).
		Scan(&hw.DeviceID, &hw.CPUModel, &hw.CPUCores,
			&hw.RAMTotalMB, &hw.DiskTotalMB, &hw.DiskFreeMB,
			&niJSON, &hw.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHardwareNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(niJSON, &hw.NetworkInterfaces); err != nil {
		return nil, fmt.Errorf("unmarshal network interfaces: %w", err)
	}
	if hw.NetworkInterfaces == nil {
		hw.NetworkInterfaces = []NetworkInterfaceInfo{}
	}
	return &hw, nil
}

// --- Device Logs -------------------------------------------------------------

// PostgresLogs implements [LogsRepository] against PostgreSQL.
type PostgresLogs struct {
	db *sql.DB
}

// NewPostgresLogs returns a Postgres-backed LogsRepository.
func NewPostgresLogs(db *sql.DB) *PostgresLogs {
	return &PostgresLogs{db: db}
}

func (p *PostgresLogs) Upsert(ctx context.Context, deviceID DeviceID, entries []LogEntry) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // harmless after Commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM device_logs WHERE device_id = $1`, deviceID); err != nil {
		return fmt.Errorf("delete old logs: %w", err)
	}

	for _, e := range entries {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO device_logs (device_id, timestamp, level, target, message, fetched_at) VALUES ($1, $2, $3, $4, $5, NOW())`,
			deviceID, e.Timestamp, e.Level, e.Target, e.Message); err != nil {
			return fmt.Errorf("insert log entry: %w", err)
		}
	}
	return tx.Commit()
}

// Query is sentinel-parameterized to keep the SQL static (Sonar go:S2077).
// Level is compared via a severity-ordered CASE so "WARN" matches WARN+ERROR,
// mirroring the agent-side logger semantics.
func (p *PostgresLogs) Query(ctx context.Context, deviceID DeviceID, filter LogFilter) ([]LogEntry, int, error) {
	searchPattern := ""
	if filter.Search != "" {
		searchPattern = "%" + filter.Search + "%"
	}

	filterArgs := []any{
		deviceID,
		filter.Level,
		filter.From,
		filter.To,
		filter.Search, searchPattern,
	}

	var total int
	if err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM device_logs
		WHERE device_id = $1
		  AND ($2 = '' OR (CASE level
		        WHEN 'TRACE' THEN 0
		        WHEN 'DEBUG' THEN 1
		        WHEN 'INFO'  THEN 2
		        WHEN 'WARN'  THEN 3
		        WHEN 'ERROR' THEN 4
		        ELSE -1
		      END) >= (CASE $2
		        WHEN 'TRACE' THEN 0
		        WHEN 'DEBUG' THEN 1
		        WHEN 'INFO'  THEN 2
		        WHEN 'WARN'  THEN 3
		        WHEN 'ERROR' THEN 4
		        ELSE -1
		      END))
		  AND ($3 = '' OR timestamp >= $3)
		  AND ($4 = '' OR timestamp <= $4)
		  AND ($5 = '' OR message LIKE $6)`,
		filterArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count logs: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	dataArgs := make([]any, len(filterArgs)+2)
	copy(dataArgs, filterArgs)
	dataArgs[len(filterArgs)] = limit
	dataArgs[len(filterArgs)+1] = filter.Offset
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, device_id, timestamp, level, target, message, fetched_at FROM device_logs
		WHERE device_id = $1
		  AND ($2 = '' OR (CASE level
		        WHEN 'TRACE' THEN 0
		        WHEN 'DEBUG' THEN 1
		        WHEN 'INFO'  THEN 2
		        WHEN 'WARN'  THEN 3
		        WHEN 'ERROR' THEN 4
		        ELSE -1
		      END) >= (CASE $2
		        WHEN 'TRACE' THEN 0
		        WHEN 'DEBUG' THEN 1
		        WHEN 'INFO'  THEN 2
		        WHEN 'WARN'  THEN 3
		        WHEN 'ERROR' THEN 4
		        ELSE -1
		      END))
		  AND ($3 = '' OR timestamp >= $3)
		  AND ($4 = '' OR timestamp <= $4)
		  AND ($5 = '' OR message LIKE $6)
		ORDER BY timestamp DESC
		LIMIT $7 OFFSET $8`,
		dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Timestamp, &e.Level, &e.Target, &e.Message, &e.FetchedAt); err != nil {
			return nil, 0, fmt.Errorf("scan log entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

func (p *PostgresLogs) HasRecent(ctx context.Context, deviceID DeviceID, maxAge time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	var exists bool
	err := p.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM device_logs WHERE device_id = $1 AND fetched_at > $2)`,
		deviceID, cutoff).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check recent logs: %w", err)
	}
	return exists, nil
}

// --- internal helpers --------------------------------------------------------

func queryDevices(ctx context.Context, db *sql.DB, query string, args ...any) ([]*Device, error) {
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

func checkAffected(res sql.Result, execErr error, notFound error) error {
	if execErr != nil {
		return execErr
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}
