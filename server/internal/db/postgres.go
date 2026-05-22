package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver with database/sql
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// scanner abstracts *sql.Row and *sql.Rows so scan helpers work with both.
type scanner interface {
	Scan(dest ...any) error
}

// PostgresStore implements Store using PostgreSQL via the pgx/v5 stdlib driver.
type PostgresStore struct {
	db *sql.DB
}

// PostgresOptions tunes the connection pool used by NewPostgresStoreWithOptions.
// A zero value means "use the production default".
type PostgresOptions struct {
	MaxOpenConns int
	MaxIdleConns int
}

// NewPostgresStore opens a PostgreSQL connection pool, runs migrations, and
// returns a ready-to-use store.
//
// databaseURL follows the libpq URL form: "postgres://user:pass@host:port/db?sslmode=disable".
func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	return NewPostgresStoreWithOptions(ctx, databaseURL, PostgresOptions{})
}

// NewPostgresStoreWithOptions is NewPostgresStore with explicit pool sizing.
// Test code uses this to keep many parallel per-schema stores within
// Postgres's max_connections budget.
func NewPostgresStoreWithOptions(ctx context.Context, databaseURL string, opts PostgresOptions) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	maxOpen := opts.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 25 // production default; conservative
	}
	maxIdle := opts.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 5
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if err := runPostgresMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func runPostgresMigrations(db *sql.DB) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
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

// queryOnePG runs a SELECT that returns a single row and maps sql.ErrNoRows to
// ErrNotFound so callers don't need to repeat the check.
func queryOnePG[T any](ctx context.Context, db *sql.DB, scan func(scanner) (*T, error), query string, args ...any) (*T, error) {
	item, err := scan(db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
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
	return queryOnePG(ctx, s.db, scanUserPG,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE id = $1`,
		id)
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return queryOnePG(ctx, s.db, scanUserPG,
		`SELECT id, email, password_hash, display_name, is_admin, created_at, updated_at FROM users WHERE email = $1`,
		email)
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
	return queryOnePG(ctx, s.db, scanAgentSessionPG,
		`SELECT token, device_id, user_id, created_at FROM agent_sessions WHERE token = $1`,
		token)
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
	return queryOnePG(ctx, s.db, scanAMTDevicePG,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices WHERE uuid = $1`,
		id)
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

