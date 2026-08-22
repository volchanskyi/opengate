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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib" // also registers the pgx driver with database/sql
)

// migrationTenantScope gives the migration connection the cross-tenant reach
// its statements need. Application tables carry FORCE ROW LEVEL SECURITY and
// the deployed database role is NOBYPASSRLS, so a migration that reads or
// writes rows is subject to the tenant policy just like a request is. Those
// policies read their scope setting with no missing_ok fallback, so an absent
// GUC aborts the migration instead of merely filtering it. app.is_admin is set
// alongside because either side of the policy's OR may be evaluated first.
//
// A fresh database walks the whole chain on this one connection, so both scope
// settings are carried: the policies read app.current_org up to the tenancy
// rename and app.current_tenant from it onward.
//
// The scope lives only on the short-lived migration pool below. The pool that
// serves application traffic never carries it, so tenant isolation is
// unchanged.
const migrationTenantScope = "-c app.is_admin=true" +
	" -c app.current_org=00000000-0000-0000-0000-000000000000" +
	" -c app.current_tenant=00000000-0000-0000-0000-000000000000"

//go:embed migrations/*.sql
var migrationsFS embed.FS

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

	if err := runPostgresMigrations(databaseURL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

// openMigrationDB returns a single-connection pool dedicated to migrations,
// every connection of which carries migrationTenantScope. Any options the
// caller already supplied are kept — the scope is appended, not substituted.
func openMigrationDB(databaseURL string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if existing := cfg.RuntimeParams["options"]; existing != "" {
		cfg.RuntimeParams["options"] = existing + " " + migrationTenantScope
	} else {
		cfg.RuntimeParams["options"] = migrationTenantScope
	}
	migrationDB := stdlib.OpenDB(*cfg)
	migrationDB.SetMaxOpenConns(1)
	return migrationDB, nil
}

func runPostgresMigrations(databaseURL string) error {
	migrationDB, err := openMigrationDB(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = migrationDB.Close() }()

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}
	dbDriver, err := migratepgx.WithInstance(migrationDB, &migratepgx.Config{})
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

// PoolStats reports the connection pool's current occupancy and its running
// account of callers that had to queue for a connection.
//
// The pool is a resource a load run can exhaust before anything else gives way,
// and latency alone cannot say so: a request that waited 200 ms for a
// connection and one that spent 200 ms executing look identical from outside.
// The wait totals are what separate them.
func (s *PostgresStore) PoolStats() sql.DBStats {
	return s.db.Stats()
}

// Size returns the current Postgres database size in bytes via pg_database_size().
func (s *PostgresStore) Size(ctx context.Context) (int64, error) {
	var size int64
	if err := s.db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&size); err != nil {
		return 0, fmt.Errorf("query pg_database_size: %w", err)
	}
	return size, nil
}
