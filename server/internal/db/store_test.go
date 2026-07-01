package db

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testpg"
	"os"
	"strings"
	"testing"
	"time"
)

// pgTestDB is the shared Postgres store for this package, migrated into a
// fixed test schema. Each test TRUNCATEs tables before use to isolate state.
// Using a shared pool + TRUNCATE is much faster than creating a new schema
// per test, and uses only static SQL (no dynamic identifiers) — so go:S2077
// stays green.
var pgTestDB *PostgresStore

// TestMain provisions the shared Postgres store once per package run. The base
// database comes from POSTGRES_TEST_URL or an auto-provisioned container; either
// way the tests run — they never skip on a missing database.
func TestMain(m *testing.M) {
	baseURL, err := testpg.URL()
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres test setup failed: %v\n", err)
		os.Exit(1)
	}
	if err := setupPostgresTestDB(baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "postgres test setup failed: %v\n", err)
		os.Exit(1)
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
func newPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	require.NotNil(t, pgTestDB, "shared Postgres store not initialised by TestMain")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, truncatePostgresTestDB(ctx, pgTestDB))
	return pgTestDB
}
