package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"

	_ "modernc.org/sqlite" // register pure-Go SQLite driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-backed store at the given path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite only supports one writer

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func runMigrations(db *sql.DB) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}
	dbDriver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("migration db driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", dbDriver)
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// nowRFC3339 returns the current UTC time formatted as RFC3339.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// execAndCheckAffected runs a mutation query and returns ErrNotFound when zero rows were affected.
func (s *SQLiteStore) execAndCheckAffected(ctx context.Context, query string, args ...any) error {
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

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanner abstracts *sql.Row and *sql.Rows for shared scan functions.
type scanner interface {
	Scan(dest ...any) error
}

// queryList runs a SELECT and scans all rows using the provided scan function.
func queryList[T any](ctx context.Context, db *sql.DB, scan func(scanner) (*T, error), query string, args ...any) ([]*T, error) {
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

func parseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse UUID %q: %w", s, err)
	}
	return id, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}

// --- Devices ---

func scanDeviceFrom(sc scanner) (*Device, error) {
	var d Device
	var idStr, status, lastSeen, createdAt, updatedAt string
	var groupIDStr sql.NullString
	if err := sc.Scan(&idStr, &groupIDStr, &d.Hostname, &d.OS, &d.AgentVersion, &status, &lastSeen, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	d.ID, err = parseUUID(idStr)
	if err != nil {
		return nil, err
	}
	if groupIDStr.Valid {
		d.GroupID, err = parseUUID(groupIDStr.String)
		if err != nil {
			return nil, err
		}
	}
	d.Status = DeviceStatus(status)
	d.LastSeen, err = parseTime(lastSeen)
	if err != nil {
		return nil, err
	}
	d.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	d.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *SQLiteStore) UpsertDevice(ctx context.Context, d *Device) error {
	now := nowRFC3339()
	// Store NULL for group_id when it's uuid.Nil (device not yet assigned to a group).
	var groupID any
	if d.GroupID != uuid.Nil {
		groupID = d.GroupID.String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, agent_version, status, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   group_id = COALESCE(excluded.group_id, devices.group_id),
		   hostname = excluded.hostname,
		   os = excluded.os,
		   agent_version = excluded.agent_version,
		   status = excluded.status,
		   last_seen = excluded.last_seen,
		   updated_at = excluded.updated_at`,
		d.ID.String(), groupID, d.Hostname, d.OS, d.AgentVersion, string(d.Status), now, now, now)
	return err
}

func (s *SQLiteStore) GetDevice(ctx context.Context, id DeviceID) (*Device, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, group_id, hostname, os, agent_version, status, last_seen, created_at, updated_at FROM devices WHERE id = ?`,
		id.String())
	d, err := scanDeviceFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *SQLiteStore) ListDevices(ctx context.Context, groupID GroupID) ([]*Device, error) {
	return queryList(ctx, s.db, scanDeviceFrom,
		`SELECT id, group_id, hostname, os, agent_version, status, last_seen, created_at, updated_at FROM devices WHERE group_id = ?`,
		groupID.String())
}

func (s *SQLiteStore) ListAllDevices(ctx context.Context) ([]*Device, error) {
	return queryList(ctx, s.db, scanDeviceFrom,
		`SELECT id, group_id, hostname, os, agent_version, status, last_seen, created_at, updated_at FROM devices ORDER BY hostname`)
}

func (s *SQLiteStore) ListDevicesForOwner(ctx context.Context, ownerID UserID) ([]*Device, error) {
	return queryList(ctx, s.db, scanDeviceFrom,
		`SELECT d.id, d.group_id, d.hostname, d.os, d.agent_version, d.status, d.last_seen, d.created_at, d.updated_at
		 FROM devices d JOIN groups_ g ON d.group_id = g.id WHERE g.owner_id = ? ORDER BY d.hostname`, ownerID.String())
}

func (s *SQLiteStore) DeleteDevice(ctx context.Context, id DeviceID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM devices WHERE id = ?`, id.String())
}

// UpdateDeviceGroup moves a device to a different group.
func (s *SQLiteStore) UpdateDeviceGroup(ctx context.Context, id DeviceID, groupID GroupID) error {
	now := nowRFC3339()
	var gid any
	if groupID != uuid.Nil {
		gid = groupID.String()
	}
	return s.execAndCheckAffected(ctx,
		`UPDATE devices SET group_id = ?, updated_at = ? WHERE id = ?`,
		gid, now, id.String())
}

func (s *SQLiteStore) SetDeviceStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	now := nowRFC3339()
	return s.execAndCheckAffected(ctx,
		`UPDATE devices SET status = ?, last_seen = ?, updated_at = ? WHERE id = ?`,
		string(status), now, now, id.String())
}

// --- Groups ---

func scanGroupFrom(sc scanner) (*Group, error) {
	var g Group
	var idStr, ownerIDStr, createdAt, updatedAt string
	if err := sc.Scan(&idStr, &g.Name, &ownerIDStr, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	g.ID, err = parseUUID(idStr)
	if err != nil {
		return nil, err
	}
	g.OwnerID, err = parseUUID(ownerIDStr)
	if err != nil {
		return nil, err
	}
	g.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	g.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *SQLiteStore) CreateGroup(ctx context.Context, g *Group) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO groups_ (id, name, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		g.ID.String(), g.Name, g.OwnerID.String(), now, now)
	return err
}

func (s *SQLiteStore) GetGroup(ctx context.Context, id GroupID) (*Group, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE id = ?`,
		id.String())
	g, err := scanGroupFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *SQLiteStore) ListGroups(ctx context.Context, ownerID UserID) ([]*Group, error) {
	return queryList(ctx, s.db, scanGroupFrom,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE owner_id = ?`,
		ownerID.String())
}

func (s *SQLiteStore) DeleteGroup(ctx context.Context, id GroupID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM groups_ WHERE id = ?`, id.String())
}

// --- Users ---

func scanUserFrom(sc scanner) (*User, error) {
	var u User
	var idStr, createdAt, updatedAt string
	if err := sc.Scan(&idStr, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsAdmin, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	u.ID, err = parseUUID(idStr)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	u.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *SQLiteStore) UpsertUser(ctx context.Context, u *User) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   email = excluded.email,
		   password_hash = excluded.password_hash,
		   display_name = excluded.display_name,
		   is_admin = excluded.is_admin,
		   updated_at = excluded.updated_at`,
		u.ID.String(), u.Email, u.PasswordHash, u.DisplayName, u.IsAdmin, now, now)
	return err
}

func (s *SQLiteStore) GetUser(ctx context.Context, id UserID) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE id = ?`,
		id.String())
	u, err := scanUserFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE email = ?`,
		email)
	u, err := scanUserFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *SQLiteStore) ListUsers(ctx context.Context) ([]*User, error) {
	return queryList(ctx, s.db, scanUserFrom,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users`)
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, id UserID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM users WHERE id = ?`, id.String())
}

// --- Agent Sessions ---

func scanAgentSessionFrom(sc scanner) (*AgentSession, error) {
	var as AgentSession
	var deviceIDStr, userIDStr, createdAt string
	if err := sc.Scan(&as.Token, &deviceIDStr, &userIDStr, &createdAt); err != nil {
		return nil, err
	}
	var err error
	as.DeviceID, err = parseUUID(deviceIDStr)
	if err != nil {
		return nil, err
	}
	as.UserID, err = parseUUID(userIDStr)
	if err != nil {
		return nil, err
	}
	as.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	return &as, nil
}

func (s *SQLiteStore) CreateAgentSession(ctx context.Context, as *AgentSession) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_sessions (token, device_id, user_id, created_at) VALUES (?, ?, ?, ?)`,
		as.Token, as.DeviceID.String(), as.UserID.String(), now)
	return err
}

func (s *SQLiteStore) GetAgentSession(ctx context.Context, token string) (*AgentSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE token = ?`,
		token)
	as, err := scanAgentSessionFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return as, err
}

func (s *SQLiteStore) DeleteAgentSession(ctx context.Context, token string) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM agent_sessions WHERE token = ?`, token)
}

func (s *SQLiteStore) ListActiveSessionsForDevice(ctx context.Context, deviceID DeviceID) ([]*AgentSession, error) {
	return queryList(ctx, s.db, scanAgentSessionFrom,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE device_id = ?`,
		deviceID.String())
}

// --- Web Push ---

func scanWebPushSubFrom(sc scanner) (*WebPushSubscription, error) {
	var sub WebPushSubscription
	var userIDStr string
	if err := sc.Scan(&sub.Endpoint, &userIDStr, &sub.P256dh, &sub.Auth); err != nil {
		return nil, err
	}
	var err error
	sub.UserID, err = parseUUID(userIDStr)
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *SQLiteStore) UpsertWebPushSubscription(ctx context.Context, sub *WebPushSubscription) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO web_push_subscriptions (endpoint, user_id, p256dh, auth)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(endpoint) DO UPDATE SET
		   user_id = excluded.user_id,
		   p256dh = excluded.p256dh,
		   auth = excluded.auth`,
		sub.Endpoint, sub.UserID.String(), sub.P256dh, sub.Auth)
	return err
}

func (s *SQLiteStore) ListWebPushSubscriptions(ctx context.Context, userID UserID) ([]*WebPushSubscription, error) {
	return queryList(ctx, s.db, scanWebPushSubFrom,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions WHERE user_id = ?`,
		userID.String())
}

// ListAllWebPushSubscriptions returns all push subscriptions across all users.
func (s *SQLiteStore) ListAllWebPushSubscriptions(ctx context.Context) ([]*WebPushSubscription, error) {
	return queryList(ctx, s.db, scanWebPushSubFrom,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions`)
}

func (s *SQLiteStore) DeleteWebPushSubscription(ctx context.Context, endpoint string) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM web_push_subscriptions WHERE endpoint = ?`, endpoint)
}

// --- Audit ---

func scanAuditEventFrom(sc scanner) (*AuditEvent, error) {
	var e AuditEvent
	var userIDStr, createdAt string
	if err := sc.Scan(&e.ID, &userIDStr, &e.Action, &e.Target, &e.Details, &createdAt); err != nil {
		return nil, err
	}
	var err error
	e.UserID, err = parseUUID(userIDStr)
	if err != nil {
		return nil, err
	}
	e.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *SQLiteStore) WriteAuditEvent(ctx context.Context, event *AuditEvent) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (user_id, action, target, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		event.UserID.String(), event.Action, event.Target, event.Details, now)
	return err
}

func (s *SQLiteStore) QueryAuditLog(ctx context.Context, q AuditQuery) ([]*AuditEvent, error) {
	var where []string
	var args []any
	if q.UserID != nil {
		where = append(where, "user_id = ?")
		args = append(args, q.UserID.String())
	}
	if q.Action != "" {
		where = append(where, "action = ?")
		args = append(args, q.Action)
	}

	query := `SELECT id, user_id, action, target, details, created_at FROM audit_events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY created_at DESC`
	if q.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, q.Limit)
	}
	if q.Offset > 0 {
		query += ` OFFSET ?`
		args = append(args, q.Offset)
	}

	return queryList(ctx, s.db, scanAuditEventFrom, query, args...)
}

// --- AMT Devices ---

func scanAMTDeviceFrom(sc scanner) (*AMTDevice, error) {
	var d AMTDevice
	var uuidStr, status, lastSeen string
	if err := sc.Scan(&uuidStr, &d.Hostname, &d.Model, &d.Firmware, &status, &lastSeen); err != nil {
		return nil, err
	}
	var err error
	d.UUID, err = parseUUID(uuidStr)
	if err != nil {
		return nil, err
	}
	d.Status = DeviceStatus(status)
	d.LastSeen, err = parseTime(lastSeen)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *SQLiteStore) UpsertAMTDevice(ctx context.Context, d *AMTDevice) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO amt_devices (uuid, hostname, model, firmware, status, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(uuid) DO UPDATE SET
		   hostname = CASE WHEN excluded.hostname = '' THEN amt_devices.hostname ELSE excluded.hostname END,
		   model = CASE WHEN excluded.model = '' THEN amt_devices.model ELSE excluded.model END,
		   firmware = CASE WHEN excluded.firmware = '' THEN amt_devices.firmware ELSE excluded.firmware END,
		   status = excluded.status,
		   last_seen = excluded.last_seen`,
		d.UUID.String(), d.Hostname, d.Model, d.Firmware, string(d.Status), now)
	return err
}

func (s *SQLiteStore) GetAMTDevice(ctx context.Context, id uuid.UUID) (*AMTDevice, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices WHERE uuid = ?`,
		id.String())
	d, err := scanAMTDeviceFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *SQLiteStore) ListAMTDevices(ctx context.Context) ([]*AMTDevice, error) {
	return queryList(ctx, s.db, scanAMTDeviceFrom,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices`)
}

func (s *SQLiteStore) SetAMTDeviceStatus(ctx context.Context, id uuid.UUID, status DeviceStatus) error {
	now := nowRFC3339()
	return s.execAndCheckAffected(ctx,
		`UPDATE amt_devices SET status = ?, last_seen = ? WHERE uuid = ?`,
		string(status), now, id.String())
}

// --- Enrollment Tokens ---

func scanEnrollmentTokenFrom(sc scanner) (*EnrollmentToken, error) {
	var t EnrollmentToken
	var idStr, createdByStr, expiresAt, createdAt string
	if err := sc.Scan(&idStr, &t.Token, &t.Label, &createdByStr, &t.MaxUses, &t.UseCount, &expiresAt, &createdAt); err != nil {
		return nil, err
	}
	var err error
	t.ID, err = parseUUID(idStr)
	if err != nil {
		return nil, err
	}
	t.CreatedBy, err = parseUUID(createdByStr)
	if err != nil {
		return nil, err
	}
	t.ExpiresAt, err = parseTime(expiresAt)
	if err != nil {
		return nil, err
	}
	t.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *SQLiteStore) CreateEnrollmentToken(ctx context.Context, t *EnrollmentToken) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (id, token, label, created_by, max_uses, use_count, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID.String(), t.Token, t.Label, t.CreatedBy.String(), t.MaxUses, t.UseCount,
		t.ExpiresAt.UTC().Format(time.RFC3339), now)
	return err
}

func (s *SQLiteStore) GetEnrollmentTokenByToken(ctx context.Context, token string) (*EnrollmentToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
		 FROM enrollment_tokens WHERE token = ?`, token)
	t, err := scanEnrollmentTokenFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *SQLiteStore) ListEnrollmentTokens(ctx context.Context, createdBy UserID) ([]*EnrollmentToken, error) {
	return queryList(ctx, s.db, scanEnrollmentTokenFrom,
		`SELECT id, token, label, created_by, max_uses, use_count, expires_at, created_at
		 FROM enrollment_tokens WHERE created_by = ? ORDER BY created_at DESC`,
		createdBy.String())
}

func (s *SQLiteStore) DeleteEnrollmentToken(ctx context.Context, id uuid.UUID) error {
	return s.execAndCheckAffected(ctx, `DELETE FROM enrollment_tokens WHERE id = ?`, id.String())
}

func (s *SQLiteStore) IncrementEnrollmentTokenUseCount(ctx context.Context, id uuid.UUID) error {
	return s.execAndCheckAffected(ctx,
		`UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = ?`, id.String())
}

// --- Device Updates ---

func scanDeviceUpdateFrom(sc scanner) (*DeviceUpdate, error) {
	var du DeviceUpdate
	var deviceIDStr, status, pushedAt string
	var ackedAt sql.NullString
	if err := sc.Scan(&du.ID, &deviceIDStr, &du.Version, &status, &du.Error, &pushedAt, &ackedAt); err != nil {
		return nil, err
	}
	var err error
	du.DeviceID, err = parseUUID(deviceIDStr)
	if err != nil {
		return nil, err
	}
	du.Status = UpdateStatus(status)
	du.PushedAt, err = parseTime(pushedAt)
	if err != nil {
		return nil, err
	}
	if ackedAt.Valid {
		t, err := parseTime(ackedAt.String)
		if err != nil {
			return nil, err
		}
		du.AckedAt = &t
	}
	return &du, nil
}

// CreateDeviceUpdate inserts a new device update record (typically with status "pending").
func (s *SQLiteStore) CreateDeviceUpdate(ctx context.Context, du *DeviceUpdate) error {
	now := nowRFC3339()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO device_updates (device_id, version, status, error, pushed_at) VALUES (?, ?, ?, ?, ?)`,
		du.DeviceID.String(), du.Version, string(du.Status), du.Error, now)
	if err != nil {
		return err
	}
	du.ID, err = res.LastInsertId()
	return err
}

// UpdateDeviceUpdateStatus sets the status and acked_at timestamp for a device update record.
func (s *SQLiteStore) UpdateDeviceUpdateStatus(ctx context.Context, deviceID DeviceID, version string, status UpdateStatus, errMsg string) error {
	now := nowRFC3339()
	return s.execAndCheckAffected(ctx,
		`UPDATE device_updates SET status = ?, error = ?, acked_at = ? WHERE device_id = ? AND version = ?`,
		string(status), errMsg, now, deviceID.String(), version)
}

// ListDeviceUpdatesByVersion returns all device update records for a given version, ordered by push time.
func (s *SQLiteStore) ListDeviceUpdatesByVersion(ctx context.Context, version string) ([]*DeviceUpdate, error) {
	return queryList(ctx, s.db, scanDeviceUpdateFrom,
		`SELECT id, device_id, version, status, error, pushed_at, acked_at FROM device_updates WHERE version = ? ORDER BY pushed_at DESC`,
		version)
}

// --- Security Groups ---

func scanSecurityGroupFrom(sc scanner) (*SecurityGroup, error) {
	var g SecurityGroup
	var idStr, createdAt, updatedAt string
	if err := sc.Scan(&idStr, &g.Name, &g.Description, &g.IsSystem, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	g.ID, err = parseUUID(idStr)
	if err != nil {
		return nil, err
	}
	g.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	g.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *SQLiteStore) CreateSecurityGroup(ctx context.Context, g *SecurityGroup) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO security_groups (id, name, description, is_system, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		g.ID.String(), g.Name, g.Description, g.IsSystem, now, now)
	return err
}

func (s *SQLiteStore) GetSecurityGroup(ctx context.Context, id SecurityGroupID) (*SecurityGroup, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, is_system, created_at, updated_at FROM security_groups WHERE id = ?`,
		id.String())
	g, err := scanSecurityGroupFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *SQLiteStore) ListSecurityGroups(ctx context.Context) ([]*SecurityGroup, error) {
	return queryList(ctx, s.db, scanSecurityGroupFrom,
		`SELECT id, name, description, is_system, created_at, updated_at FROM security_groups ORDER BY name`)
}

func (s *SQLiteStore) DeleteSecurityGroup(ctx context.Context, id SecurityGroupID) error {
	// Prevent deletion of system groups.
	g, err := s.GetSecurityGroup(ctx, id)
	if err != nil {
		return err
	}
	if g.IsSystem {
		return ErrSystemGroup
	}
	return s.execAndCheckAffected(ctx, `DELETE FROM security_groups WHERE id = ?`, id.String())
}

func (s *SQLiteStore) AddSecurityGroupMember(ctx context.Context, groupID SecurityGroupID, userID UserID) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO security_group_members (group_id, user_id, added_at) VALUES (?, ?, ?)`,
		groupID.String(), userID.String(), now)
	if err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return s.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (s *SQLiteStore) RemoveSecurityGroupMember(ctx context.Context, groupID SecurityGroupID, userID UserID) error {
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
		`DELETE FROM security_group_members WHERE group_id = ? AND user_id = ?`,
		groupID.String(), userID.String())
	if err != nil {
		return err
	}
	if groupID == AdminGroupID {
		return s.syncIsAdmin(ctx, userID)
	}
	return nil
}

func (s *SQLiteStore) ListSecurityGroupMembers(ctx context.Context, groupID SecurityGroupID) ([]*User, error) {
	return queryList(ctx, s.db, scanUserFrom,
		`SELECT u.id, u.email, u.password_hash, u.display_name, u.is_admin, u.created_at, u.updated_at
		 FROM users u
		 INNER JOIN security_group_members sgm ON sgm.user_id = u.id
		 WHERE sgm.group_id = ?
		 ORDER BY u.email`,
		groupID.String())
}

func (s *SQLiteStore) IsUserInSecurityGroup(ctx context.Context, userID UserID, groupID SecurityGroupID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM security_group_members WHERE group_id = ? AND user_id = ?)`,
		groupID.String(), userID.String()).Scan(&exists)
	return exists, err
}

func (s *SQLiteStore) CountSecurityGroupMembers(ctx context.Context, groupID SecurityGroupID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_group_members WHERE group_id = ?`,
		groupID.String()).Scan(&count)
	return count, err
}

// syncIsAdmin keeps the users.is_admin boolean in sync with Administrators group membership.
func (s *SQLiteStore) syncIsAdmin(ctx context.Context, userID UserID) error {
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_admin = (
			SELECT COUNT(*) > 0 FROM security_group_members
			WHERE user_id = ? AND group_id = ?
		), updated_at = ? WHERE id = ?`,
		userID.String(), AdminGroupID.String(), now, userID.String())
	return err
}
