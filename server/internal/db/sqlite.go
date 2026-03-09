package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
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

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanner abstracts *sql.Row and *sql.Rows for shared scan functions.
type scanner interface {
	Scan(dest ...any) error
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
	var idStr, groupIDStr, status, lastSeen, createdAt, updatedAt string
	if err := sc.Scan(&idStr, &groupIDStr, &d.Hostname, &d.OS, &status, &lastSeen, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	d.ID, err = parseUUID(idStr)
	if err != nil {
		return nil, err
	}
	d.GroupID, err = parseUUID(groupIDStr)
	if err != nil {
		return nil, err
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
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, status, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   group_id = excluded.group_id,
		   hostname = excluded.hostname,
		   os = excluded.os,
		   status = excluded.status,
		   last_seen = excluded.last_seen,
		   updated_at = excluded.updated_at`,
		d.ID.String(), d.GroupID.String(), d.Hostname, d.OS, string(d.Status), now, now, now)
	return err
}

func (s *SQLiteStore) GetDevice(ctx context.Context, id DeviceID) (*Device, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, group_id, hostname, os, status, last_seen, created_at, updated_at FROM devices WHERE id = ?`,
		id.String())
	d, err := scanDeviceFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *SQLiteStore) ListDevices(ctx context.Context, groupID GroupID) ([]*Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, group_id, hostname, os, status, last_seen, created_at, updated_at FROM devices WHERE group_id = ?`,
		groupID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d, err := scanDeviceFrom(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *SQLiteStore) DeleteDevice(ctx context.Context, id DeviceID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id.String())
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

func (s *SQLiteStore) SetDeviceStatus(ctx context.Context, id DeviceID, status DeviceStatus) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET status = ?, last_seen = ?, updated_at = ? WHERE id = ?`,
		string(status), now, now, id.String())
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
	now := time.Now().UTC().Format(time.RFC3339)
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, owner_id, created_at, updated_at FROM groups_ WHERE owner_id = ?`,
		ownerID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		g, err := scanGroupFrom(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *SQLiteStore) DeleteGroup(ctx context.Context, id GroupID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM groups_ WHERE id = ?`, id.String())
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
	now := time.Now().UTC().Format(time.RFC3339)
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u, err := scanUserFrom(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, id UserID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id.String())
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
	now := time.Now().UTC().Format(time.RFC3339)
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
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_sessions WHERE token = ?`, token)
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

func (s *SQLiteStore) ListActiveSessionsForDevice(ctx context.Context, deviceID DeviceID) ([]*AgentSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE device_id = ?`,
		deviceID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*AgentSession
	for rows.Next() {
		as, err := scanAgentSessionFrom(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, as)
	}
	return sessions, rows.Err()
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions WHERE user_id = ?`,
		userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*WebPushSubscription
	for rows.Next() {
		sub, err := scanWebPushSubFrom(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// ListAllWebPushSubscriptions returns all push subscriptions across all users.
func (s *SQLiteStore) ListAllWebPushSubscriptions(ctx context.Context) ([]*WebPushSubscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT endpoint, user_id, p256dh, auth FROM web_push_subscriptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*WebPushSubscription
	for rows.Next() {
		sub, err := scanWebPushSubFrom(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *SQLiteStore) DeleteWebPushSubscription(ctx context.Context, endpoint string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM web_push_subscriptions WHERE endpoint = ?`, endpoint)
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
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (user_id, action, target, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		event.UserID.String(), event.Action, event.Target, event.Details, now)
	return err
}

func (s *SQLiteStore) QueryAuditLog(ctx context.Context, q AuditQuery) ([]*AuditEvent, error) {
	query := `SELECT id, user_id, action, target, details, created_at FROM audit_events WHERE 1=1`
	var args []any
	if q.UserID != nil {
		query += ` AND user_id = ?`
		args = append(args, q.UserID.String())
	}
	if q.Action != "" {
		query += ` AND action = ?`
		args = append(args, q.Action)
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

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		e, err := scanAuditEventFrom(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
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
	now := time.Now().UTC().Format(time.RFC3339)
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*AMTDevice
	for rows.Next() {
		d, err := scanAMTDeviceFrom(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *SQLiteStore) SetAMTDeviceStatus(ctx context.Context, id uuid.UUID, status DeviceStatus) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE amt_devices SET status = ?, last_seen = ? WHERE uuid = ?`,
		string(status), now, id.String())
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
