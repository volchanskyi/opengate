package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shared test constants and subtest names (reused across CRUD tables).
const (
	testNameUpsertUpdates = "upsert updates existing"
	testNameGetNotFound   = "get not found"
	testNameDeleteNF      = "delete not found"
	postgresTestURLEnv    = "POSTGRES_TEST_URL"
)

// storeFactory constructs a fresh Store for a single test.
type storeFactory struct {
	name string
	new  func(t *testing.T) Store
}

// storeFactories are the backends every shared test runs against.
var storeFactories = []storeFactory{
	{name: "postgres", new: newPostgresTestStore},
}

// --- Postgres factory (shared store + per-test TRUNCATE) ---

// pgTestDB is the shared Postgres store for this package, migrated into a
// fixed test schema. Each test TRUNCATEs tables before use to isolate state.
// Using a shared pool + TRUNCATE is much faster than creating a new schema
// per test, and uses only static SQL (no dynamic identifiers) — so go:S2077
// stays green.
var pgTestDB *PostgresStore

// TestMain provisions the shared Postgres store once per package run.
func TestMain(m *testing.M) {
	baseURL := os.Getenv(postgresTestURLEnv)
	if baseURL != "" {
		if err := setupPostgresTestDB(baseURL); err != nil {
			fmt.Fprintf(os.Stderr, "postgres test setup failed: %v\n", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	if pgTestDB != nil {
		_ = pgTestDB.Close()
	}
	os.Exit(code)
}

// setupPostgresTestDB drops and recreates the opengate_test schema, then runs
// migrations into it. Schema name is a compile-time literal, so this is all
// static SQL.
func setupPostgresTestDB(baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Open a temp connection to drop/create the isolation schema. The schema
	// name is the fixed literal opengate_test — no user input, no dynamic SQL.
	setup, err := NewPostgresStore(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("open base url: %w", err)
	}
	if _, err := setup.db.ExecContext(ctx, `DROP SCHEMA IF EXISTS opengate_test CASCADE`); err != nil {
		_ = setup.Close()
		return fmt.Errorf("drop schema: %w", err)
	}
	if _, err := setup.db.ExecContext(ctx, `CREATE SCHEMA opengate_test`); err != nil {
		_ = setup.Close()
		return fmt.Errorf("create schema: %w", err)
	}
	_ = setup.Close()

	// Reopen pinned to the isolation schema so migrations land in opengate_test.
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	testURL := baseURL + sep + "search_path=opengate_test"
	store, err := NewPostgresStore(ctx, testURL)
	if err != nil {
		return fmt.Errorf("open test url: %w", err)
	}
	pgTestDB = store
	return nil
}

// newPostgresTestStore returns the shared test store after wiping all rows.
// Tests run sequentially (no t.Parallel), so a shared pool is safe.
func newPostgresTestStore(t *testing.T) Store {
	t.Helper()
	if pgTestDB == nil {
		t.Skipf("%s not set; skipping Postgres tests", postgresTestURLEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, truncatePostgresTestDB(ctx, pgTestDB))
	return pgTestDB
}

// truncatePostgresTestDB wipes every table and re-seeds the built-in
// Administrators group. One static TRUNCATE ... CASCADE touches all tables;
// no dynamic identifiers.
func truncatePostgresTestDB(ctx context.Context, s *PostgresStore) error {
	if _, err := s.db.ExecContext(ctx, `
		TRUNCATE TABLE
			security_group_members,
			device_logs,
			device_hardware,
			device_updates,
			enrollment_tokens,
			amt_devices,
			audit_events,
			web_push_subscriptions,
			agent_sessions,
			devices,
			groups_,
			security_groups,
			users
		RESTART IDENTITY CASCADE`); err != nil {
		return fmt.Errorf("truncate tables: %w", err)
	}
	// Re-seed the Administrators group normally inserted by migration 005.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO security_groups (id, name, description, is_system)
		VALUES ('00000000-0000-0000-0000-000000000001', 'Administrators', 'Full system access', TRUE)`); err != nil {
		return fmt.Errorf("seed administrators group: %w", err)
	}
	return nil
}

// --- Shared tests (run against every storeFactory) ---

func TestPing(t *testing.T) {
	for _, f := range storeFactories {
		t.Run(f.name, func(t *testing.T) {
			s := f.new(t)
			assert.NoError(t, s.Ping(context.Background()))
		})
	}
}

func TestUserCRUD(t *testing.T) {
	for _, f := range storeFactories {
		t.Run(f.name, func(t *testing.T) {
			s := f.new(t)
			ctx := context.Background()

			t.Run("upsert and get", func(t *testing.T) {
				u := &User{
					ID:           uuid.New(),
					Email:        "alice-" + uuid.New().String()[:8] + "@example.com",
					PasswordHash: "argon2",
					DisplayName:  "Alice",
					IsAdmin:      true,
				}
				require.NoError(t, s.UpsertUser(ctx, u))

				got, err := s.GetUser(ctx, u.ID)
				require.NoError(t, err)
				assert.Equal(t, u.ID, got.ID)
				assert.Equal(t, u.Email, got.Email)
				assert.Equal(t, u.PasswordHash, got.PasswordHash)
				assert.Equal(t, u.DisplayName, got.DisplayName)
				assert.True(t, got.IsAdmin)
				assert.False(t, got.CreatedAt.IsZero())
				assert.False(t, got.UpdatedAt.IsZero())
			})

			t.Run(testNameUpsertUpdates, func(t *testing.T) {
				u := &User{ID: uuid.New(), Email: "update-" + uuid.New().String()[:8] + "@example.com", DisplayName: "Before"}
				require.NoError(t, s.UpsertUser(ctx, u))

				u.DisplayName = "After"
				require.NoError(t, s.UpsertUser(ctx, u))

				got, err := s.GetUser(ctx, u.ID)
				require.NoError(t, err)
				assert.Equal(t, "After", got.DisplayName)
			})

			t.Run("get by email", func(t *testing.T) {
				email := "byemail-" + uuid.New().String()[:8] + "@example.com"
				u := &User{ID: uuid.New(), Email: email}
				require.NoError(t, s.UpsertUser(ctx, u))

				got, err := s.GetUserByEmail(ctx, email)
				require.NoError(t, err)
				assert.Equal(t, u.ID, got.ID)
			})

			t.Run("get by email not found", func(t *testing.T) {
				_, err := s.GetUserByEmail(ctx, "nope-"+uuid.New().String()[:8]+"@example.com")
				assert.True(t, errors.Is(err, ErrNotFound))
			})

			t.Run(testNameGetNotFound, func(t *testing.T) {
				_, err := s.GetUser(ctx, uuid.New())
				assert.True(t, errors.Is(err, ErrNotFound))
			})

			t.Run("list users", func(t *testing.T) {
				users, err := s.ListUsers(ctx)
				require.NoError(t, err)
				assert.GreaterOrEqual(t, len(users), 2) // from earlier subtests
			})

			t.Run("delete", func(t *testing.T) {
				u := &User{ID: uuid.New(), Email: "delete-" + uuid.New().String()[:8] + "@example.com"}
				require.NoError(t, s.UpsertUser(ctx, u))
				require.NoError(t, s.DeleteUser(ctx, u.ID))
				_, err := s.GetUser(ctx, u.ID)
				assert.True(t, errors.Is(err, ErrNotFound))
			})

			t.Run(testNameDeleteNF, func(t *testing.T) {
				err := s.DeleteUser(ctx, uuid.New())
				assert.True(t, errors.Is(err, ErrNotFound))
			})
		})
	}
}

func TestStoreSize(t *testing.T) {
	type sizer interface {
		Size(ctx context.Context) (int64, error)
	}
	for _, f := range storeFactories {
		t.Run(f.name, func(t *testing.T) {
			s := f.new(t)
			sz, ok := s.(sizer)
			require.True(t, ok, "store must implement Size(ctx)")
			size, err := sz.Size(context.Background())
			require.NoError(t, err)
			assert.Greater(t, size, int64(0))
		})
	}
}

// TestPostgresStoreDB exercises the DB() accessor used by metrics and test
// helpers to reach the underlying *sql.DB.
func TestPostgresStoreDB(t *testing.T) {
	s := newPostgresTestStore(t).(*PostgresStore)
	pool := s.DB()
	require.NotNil(t, pool)
	require.NoError(t, pool.PingContext(context.Background()))
}

// TestNewPostgresStoreErrors covers the failure branches of NewPostgresStore:
// malformed URL (open fails), and unreachable server (ping fails).
func TestNewPostgresStoreErrors(t *testing.T) {
	t.Run("malformed url", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := NewPostgresStore(ctx, "://not-a-url")
		require.Error(t, err)
	})

	t.Run("unreachable host", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// 192.0.2.0/24 is TEST-NET-1 — never routable. Ping will fail fast via context.
		_, err := NewPostgresStore(ctx, "postgres://u:p@192.0.2.1:5432/db?sslmode=disable&connect_timeout=1")
		require.Error(t, err)
	})
}
