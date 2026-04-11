package db

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver with database/sql
)

//go:embed migrations/postgres/*.sql
var postgresMigrationsFS embed.FS

// PostgresStore implements Store using PostgreSQL via the pgx/v5 stdlib driver.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore opens a PostgreSQL connection pool, runs migrations, and
// returns a ready-to-use store.
//
// databaseURL follows the libpq URL form: "postgres://user:pass@host:port/db?sslmode=disable".
func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Conservative connection pool. Revisit after production metrics.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if err := runPostgresMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func runPostgresMigrations(db *sql.DB) error {
	sourceDriver, err := iofs.New(postgresMigrationsFS, "migrations/postgres")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}
	dbDriver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		return fmt.Errorf("migration db driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "pgx", dbDriver)
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct queries (e.g. pg_database_size for metrics).
func (s *PostgresStore) DB() *sql.DB {
	return s.db
}

// Size returns the current Postgres database size in bytes via pg_database_size().
func (s *PostgresStore) Size(ctx context.Context) (int64, error) {
	var size int64
	if err := s.db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&size); err != nil {
		return 0, fmt.Errorf("query pg_database_size: %w", err)
	}
	return size, nil
}

// execAndCheckAffected runs a mutation query and returns ErrNotFound when zero rows were affected.
func (s *PostgresStore) execAndCheckAffected(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// queryListPG runs a SELECT and scans all rows using the provided scan function.
// Kept package-private and parallel to the SQLite helper so that shared scan
// functions can be reused for whichever store runs them.
func queryListPG[T any](ctx context.Context, db *sql.DB, scan func(scanner) (*T, error), query string, args ...any) ([]*T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// --- Devices ---

// scanDevicePG mirrors scanDeviceFrom but consumes native Postgres types
// (UUID, TIMESTAMPTZ, JSONB) so no intermediate string parsing is needed.
func scanDevicePG(sc scanner) (*Device, error) {
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

func (s *PostgresStore) UpsertDevice(ctx context.Context, d *Device) error {
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
	_, err = s.db.ExecContext(ctx,
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

func (s *PostgresStore) GetDevice(ctx context.Context, id DeviceID) (*Device, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at FROM devices WHERE id = $1`,
		id)
	d, err := scanDevicePG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *PostgresStore) ListDevices(ctx context.Context, groupID GroupID) ([]*Device, error) {
	return queryListPG(ctx, s.db, scanDevicePG,
		`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at FROM devices WHERE group_id = $1`,
		groupID)
}

func (s *PostgresStore) ListAllDevices(ctx context.Context) ([]*Device, error) {
	return queryListPG(ctx, s.db, scanDevicePG,
		`SELECT id, group_id, hostname, os, os_display, agent_version, capabilities, status, last_seen, created_at, updated_at FROM devices ORDER BY hostname`)
}

func (s *PostgresStore) ListDevicesForOwner(ctx context.Context, ownerID UserID) ([]*Device, error) {
	return queryListPG(ctx, s.db, scanDevicePG,
		`SELECT d.id, d.group_id, d.hostname, d.os, d.os_display, d.agent_version, d.capabilities, d.status, d.last_seen, d.created_at, d.updated_at
		 FROM devices d LEFT JOIN groups_ g ON d.group_id = g.id
		 WHERE g.owner_id = $1 OR d.group_id IS NULL
		 ORDER BY d.hostname`, ownerID)
}

func (s *PostgresStore) DeleteDevice(ctx context.Context, id DeviceID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM devices WHERE id = $1`, id)
}

// UpdateDeviceGroup moves a device to a different group, or clears the group assignment
// when groupID is uuid.Nil.
func (s *PostgresStore) UpdateDeviceGroup(ctx context.Context, id DeviceID, groupID GroupID) error {
	var gid any
	if groupID != uuid.Nil {
		gid = groupID
	}
	return s.execAndCheckAffected(ctx,
		`UPDATE devices SET group_id = $1, updated_at = NOW() WHERE id = $2`,
		gid, id)
}

func (s *PostgresStore) SetDeviceStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	return s.execAndCheckAffected(ctx,
		`UPDATE devices SET status = $1, last_seen = NOW(), updated_at = NOW() WHERE id = $2`,
		string(status), id)
}

// ResetAllDeviceStatuses sets all online devices to offline. Used on server
// startup to clear stale statuses from a previous run.
func (s *PostgresStore) ResetAllDeviceStatuses(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE devices SET status = $1, updated_at = NOW() WHERE status = $2`,
		string(StatusOffline), string(StatusOnline))
	return err
}

// --- Groups ---

func scanGroupPG(sc scanner) (*Group, error) {
	var g Group
	if err := sc.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *PostgresStore) CreateGroup(ctx context.Context, g *Group) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO groups_ (id, name, owner_id, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`,
		g.ID, g.Name, g.OwnerID)
	return err
}

func (s *PostgresStore) GetGroup(ctx context.Context, id GroupID) (*Group, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE id = $1`,
		id)
	g, err := scanGroupPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *PostgresStore) ListGroups(ctx context.Context, ownerID UserID) ([]*Group, error) {
	return queryListPG(ctx, s.db, scanGroupPG,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE owner_id = $1`,
		ownerID)
}

func (s *PostgresStore) DeleteGroup(ctx context.Context, id GroupID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM groups_ WHERE id = $1`, id)
}

// --- Users ---

func scanUserPG(sc scanner) (*User, error) {
	var u User
	if err := sc.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) UpsertUser(ctx context.Context, u *User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		 ON CONFLICT (id) DO UPDATE SET
		   email = EXCLUDED.email,
		   password_hash = EXCLUDED.password_hash,
		   display_name = EXCLUDED.display_name,
		   is_admin = EXCLUDED.is_admin,
		   updated_at = NOW()`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.IsAdmin)
	return err
}

func (s *PostgresStore) GetUser(ctx context.Context, id UserID) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE id = $1`,
		id)
	u, err := scanUserPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE email = $1`,
		email)
	u, err := scanUserPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]*User, error) {
	return queryListPG(ctx, s.db, scanUserPG,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users`)
}

func (s *PostgresStore) DeleteUser(ctx context.Context, id UserID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM users WHERE id = $1`, id)
}

// --- Agent Sessions ---

func scanAgentSessionPG(sc scanner) (*AgentSession, error) {
	var as AgentSession
	if err := sc.Scan(&as.Token, &as.DeviceID, &as.UserID, &as.CreatedAt); err != nil {
		return nil, err
	}
	return &as, nil
}

func (s *PostgresStore) CreateAgentSession(ctx context.Context, as *AgentSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_sessions (token, device_id, user_id, created_at) VALUES ($1, $2, $3, NOW())`,
		as.Token, as.DeviceID, as.UserID)
	return err
}

func (s *PostgresStore) GetAgentSession(ctx context.Context, token string) (*AgentSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE token = $1`,
		token)
	as, err := scanAgentSessionPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return as, err
}

func (s *PostgresStore) DeleteAgentSession(ctx context.Context, token string) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM agent_sessions WHERE token = $1`, token)
}

func (s *PostgresStore) ListActiveSessionsForDevice(ctx context.Context, deviceID DeviceID) ([]*AgentSession, error) {
	return queryListPG(ctx, s.db, scanAgentSessionPG,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE device_id = $1`,
		deviceID)
}

// --- Web Push ---

func scanWebPushSubPG(sc scanner) (*WebPushSubscription, error) {
	var sub WebPushSubscription
	if err := sc.Scan(&sub.Endpoint, &sub.UserID, &sub.P256dh, &sub.Auth); err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *PostgresStore) UpsertWebPushSubscription(ctx context.Context, sub *WebPushSubscription) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO web_push_subscriptions (endpoint, user_id, p256dh, auth)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (endpoint) DO UPDATE SET
		   user_id = EXCLUDED.user_id,
		   p256dh = EXCLUDED.p256dh,
		   auth = EXCLUDED.auth`,
		sub.Endpoint, sub.UserID, sub.P256dh, sub.Auth)
	return err
}

func (s *PostgresStore) ListWebPushSubscriptions(ctx context.Context, userID UserID) ([]*WebPushSubscription, error) {
	return queryListPG(ctx, s.db, scanWebPushSubPG,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions WHERE user_id = $1`,
		userID)
}

// ListAllWebPushSubscriptions returns all push subscriptions across all users.
func (s *PostgresStore) ListAllWebPushSubscriptions(ctx context.Context) ([]*WebPushSubscription, error) {
	return queryListPG(ctx, s.db, scanWebPushSubPG,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions`)
}

func (s *PostgresStore) DeleteWebPushSubscription(ctx context.Context, endpoint string) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM web_push_subscriptions WHERE endpoint = $1`, endpoint)
}

// --- Audit ---

func scanAuditEventPG(sc scanner) (*AuditEvent, error) {
	var e AuditEvent
	if err := sc.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.Details, &e.CreatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *PostgresStore) WriteAuditEvent(ctx context.Context, event *AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (user_id, action, target, details, created_at) VALUES ($1, $2, $3, $4, NOW())`,
		event.UserID, event.Action, event.Target, event.Details)
	return err
}

func (s *PostgresStore) QueryAuditLog(ctx context.Context, q AuditQuery) ([]*AuditEvent, error) {
	var where []string
	var args []any
	argN := 1
	if q.UserID != nil {
		where = append(where, fmt.Sprintf("user_id = $%d", argN))
		args = append(args, *q.UserID)
		argN++
	}
	if q.Action != "" {
		where = append(where, fmt.Sprintf("action = $%d", argN))
		args = append(args, q.Action)
		argN++
	}

	query := `SELECT id, user_id, action, target, details, created_at FROM audit_events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if q.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argN)
		args = append(args, q.Limit)
		argN++
	}
	if q.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argN)
		args = append(args, q.Offset)
	}

	return queryListPG(ctx, s.db, scanAuditEventPG, query, args...)
}

// --- AMT Devices ---

func scanAMTDevicePG(sc scanner) (*AMTDevice, error) {
	var d AMTDevice
	if err := sc.Scan(&d.UUID, &d.Hostname, &d.Model, &d.Firmware, &d.Status, &d.LastSeen); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PostgresStore) UpsertAMTDevice(ctx context.Context, d *AMTDevice) error {
	_, err := s.db.ExecContext(ctx,
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

func (s *PostgresStore) GetAMTDevice(ctx context.Context, id uuid.UUID) (*AMTDevice, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices WHERE uuid = $1`,
		id)
	d, err := scanAMTDevicePG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *PostgresStore) ListAMTDevices(ctx context.Context) ([]*AMTDevice, error) {
	return queryListPG(ctx, s.db, scanAMTDevicePG,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices`)
}

func (s *PostgresStore) SetAMTDeviceStatus(ctx context.Context, id uuid.UUID, status DeviceStatus) error {
	return s.execAndCheckAffected(ctx,
		`UPDATE amt_devices SET status = $1, last_seen = NOW() WHERE uuid = $2`,
		string(status), id)
}

// --- Enrollment Tokens ---

func scanEnrollmentTokenPG(sc scanner) (*EnrollmentToken, error) {
	var t EnrollmentToken
	if err := sc.Scan(&t.ID, &t.Token, &t.Label, &t.CreatedBy, &t.MaxUses, &t.UseCount, &t.ExpiresAt, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *PostgresStore) CreateEnrollmentToken(ctx context.Context, t *EnrollmentToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (id, token, label, created_by, max_uses, use_count, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		t.ID, t.Token, t.Label, t.CreatedBy, t.MaxUses, t.UseCount, t.ExpiresAt.UTC())
	return err
}

func (s *PostgresStore) GetEnrollmentTokenByToken(ctx context.Context, token string) (*EnrollmentToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
		 FROM enrollment_tokens WHERE token = $1`, token)
	t, err := scanEnrollmentTokenPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *PostgresStore) ListEnrollmentTokens(ctx context.Context, createdBy UserID) ([]*EnrollmentToken, error) {
	return queryListPG(ctx, s.db, scanEnrollmentTokenPG,
		`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
		 FROM enrollment_tokens WHERE created_by = $1 ORDER BY created_at DESC`,
		createdBy)
}

func (s *PostgresStore) DeleteEnrollmentToken(ctx context.Context, id uuid.UUID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM enrollment_tokens WHERE id = $1`, id)
}

func (s *PostgresStore) IncrementEnrollmentTokenUseCount(ctx context.Context, id uuid.UUID) error {
	return s.execAndCheckAffected(ctx,
		`UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = $1`, id)
}

// --- Device Updates ---

func scanDeviceUpdatePG(sc scanner) (*DeviceUpdate, error) {
	var du DeviceUpdate
	var ackedAt sql.NullTime
	if err := sc.Scan(&du.ID, &du.DeviceID, &du.Version, &du.Status, &du.Error, &du.PushedAt, &ackedAt); err != nil {
		return nil, err
	}
	if ackedAt.Valid {
		t := ackedAt.Time
		du.AckedAt = &t
	}
	return &du, nil
}

// CreateDeviceUpdate inserts a new device update record and populates the
// generated numeric ID on the supplied DeviceUpdate value.
func (s *PostgresStore) CreateDeviceUpdate(ctx context.Context, du *DeviceUpdate) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO device_updates (device_id, version, status, error, pushed_at) VALUES ($1, $2, $3, $4, NOW()) RETURNING id`,
		du.DeviceID, du.Version, string(du.Status), du.Error).Scan(&du.ID)
}

// UpdateDeviceUpdateStatus sets the status and acked_at timestamp for a device update record.
func (s *PostgresStore) UpdateDeviceUpdateStatus(ctx context.Context, deviceID DeviceID, version string, status UpdateStatus, errMsg string) error {
	return s.execAndCheckAffected(ctx,
		`UPDATE device_updates SET status = $1, error = $2, acked_at = NOW() WHERE device_id = $3 AND version = $4`,
		string(status), errMsg, deviceID, version)
}

// ListDeviceUpdatesByVersion returns all device update records for a given version, ordered by push time.
func (s *PostgresStore) ListDeviceUpdatesByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error) {
	return queryListPG(ctx, s.db, scanDeviceUpdatePG,
		`SELECT id, device_id, version, status, error, pushed_at, acked_at FROM device_updates WHERE version = $1 ORDER BY pushed_at DESC`,
		version)
}

// --- Device Hardware ---

// UpsertDeviceHardware inserts or updates hardware inventory for a device.
func (s *PostgresStore) UpsertDeviceHardware(ctx context.Context, hw *DeviceHardware) error {
	niJSON, err := json.Marshal(hw.NetworkInterfaces)
	if err != nil {
		return fmt.Errorf("marshal network interfaces: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
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

// GetDeviceHardware returns the hardware inventory for a device.
func (s *PostgresStore) GetDeviceHardware(ctx context.Context, deviceID DeviceID) (*DeviceHardware, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT device_id, cpu_model, cpu_cores, ram_total_mb, disk_total_mb, disk_free_mb, network_interfaces, updated_at
		 FROM device_hardware WHERE device_id = $1`, deviceID)

	var hw DeviceHardware
	var niJSON []byte
	err := row.Scan(&hw.DeviceID, &hw.CPUModel, &hw.CPUCores,
		&hw.RAMTotalMB, &hw.DiskTotalMB, &hw.DiskFreeMB,
		&niJSON, &hw.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
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

// --- Security Groups ---

func scanSecurityGroupPG(sc scanner) (*SecurityGroup, error) {
	var g SecurityGroup
	if err := sc.Scan(&g.ID, &g.Name, &g.Description, &g.IsSystem, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *PostgresStore) CreateSecurityGroup(ctx context.Context, g *SecurityGroup) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO security_groups (id, name, description, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())`,
		g.ID, g.Name, g.Description, g.IsSystem)
	return err
}

func (s *PostgresStore) GetSecurityGroup(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, is_system, created_at, updated_at FROM security_groups WHERE id = $1`,
		id)
	g, err := scanSecurityGroupPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *PostgresStore) ListSecurityGroups(ctx context.Context) ([]*SecurityGroup, error) {
	return queryListPG(ctx, s.db, scanSecurityGroupPG,
		`SELECT id, name, description, is_system, created_at, updated_at FROM security_groups ORDER BY name`)
}

func (s *PostgresStore) DeleteSecurityGroup(ctx context.Context, id SecurityGroupID) error {
	// Prevent deletion of system groups.
	g, err := s.GetSecurityGroup(ctx, id)
	if err != nil {
		return err
	}
	if g.IsSystem {
		return ErrSystemGroup
	}
	return s.execAndCheckAffected(ctx, `DELETE FROM security_groups WHERE id = $1`, id)
}

func (s *PostgresStore) AddSecurityGroupMember(ctx context.Context, groupID SecurityGroupID, userID UserID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO security_group_members (group_id, user_id, added_at) VALUES ($1, $2, NOW())
		 ON CONFLICT DO NOTHING`,
		groupID, userID)
	if err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return s.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (s *PostgresStore) RemoveSecurityGroupMember(ctx context.Context, groupID SecurityGroupID, userID UserID) error {
	// Prevent removing the last administrator.
	if groupID == AdminGroupID {
		count, err := s.CountSecurityGroupMembers(ctx, groupID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	err := s.execAndCheckAffected(ctx,
		`DELETE FROM security_group_members WHERE group_id = $1 AND user_id = $2`,
		groupID, userID)
	if err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return s.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (s *PostgresStore) ListSecurityGroupMembers(ctx context.Context, groupID SecurityGroupID) ([]*User, error) {
	return queryListPG(ctx, s.db, scanUserPG,
		`SELECT u.id, u.email, u.password_hash, u.display_name, u.is_admin, u.created_at, u.updated_at
		 FROM users u
		 INNER JOIN security_group_members sgm ON sgm.user_id = u.id
		 WHERE sgm.group_id = $1
		 ORDER BY u.email`,
		groupID)
}

func (s *PostgresStore) IsUserInSecurityGroup(ctx context.Context, userID UserID, groupID SecurityGroupID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM security_group_members WHERE group_id = $1 AND user_id = $2)`,
		groupID, userID).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) CountSecurityGroupMembers(ctx context.Context, groupID SecurityGroupID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_group_members WHERE group_id = $1`,
		groupID).Scan(&count)
	return count, err
}

// --- Device Logs ---

// UpsertDeviceLogs replaces all cached log entries for a device with a new batch.
func (s *PostgresStore) UpsertDeviceLogs(ctx context.Context, deviceID DeviceID, entries []DeviceLogEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete existing logs for this device.
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

// QueryDeviceLogs returns filtered, paginated log entries and a total count.
//
// Both queries are inline string literals so Sonar's go:S2077 analyzer
// recognizes them as static SQL (any const-time concatenation or
// fmt.Sprintf still trips the hotspot rule). Every optional filter is
// guarded by a `$n = ” OR ...` sentinel — no dynamic concatenation,
// parameterized throughout. Level filtering is severity-based
// (WARN matches WARN+ERROR) to mirror mesh-agent/src/logs.rs semantics.
// Mirrors SQLiteStore.QueryDeviceLogs (see sqlite.go) byte-for-byte
// except for the $n placeholder syntax.
func (s *PostgresStore) QueryDeviceLogs(ctx context.Context, deviceID DeviceID, filter LogFilter) ([]DeviceLogEntry, int, error) {
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

	// Count total matching entries.
	var total int
	if err := s.db.QueryRowContext(ctx,
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

	// Fetch page.
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	dataArgs := append(filterArgs, limit, filter.Offset) //nolint:gocritic
	rows, err := s.db.QueryContext(ctx,
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

	var entries []DeviceLogEntry
	for rows.Next() {
		var e DeviceLogEntry
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Timestamp, &e.Level, &e.Target, &e.Message, &e.FetchedAt); err != nil {
			return nil, 0, fmt.Errorf("scan log entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, total, rows.Err()
}

// HasRecentLogs checks whether logs for a device were fetched within maxAge.
func (s *PostgresStore) HasRecentLogs(ctx context.Context, deviceID DeviceID, maxAge time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM device_logs WHERE device_id = $1 AND fetched_at > $2)`,
		deviceID, cutoff).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check recent logs: %w", err)
	}
	return exists, nil
}

// syncIsAdmin keeps the users.is_admin boolean in sync with Administrators group membership.
func (s *PostgresStore) syncIsAdmin(ctx context.Context, userID UserID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_admin = EXISTS(
			SELECT 1 FROM security_group_members
			WHERE user_id = $1 AND group_id = $2
		), updated_at = NOW() WHERE id = $1`,
		userID, AdminGroupID)
	return err
}
