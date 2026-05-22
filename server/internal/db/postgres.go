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

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver with database/sql
)

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

