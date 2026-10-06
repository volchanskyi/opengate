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

// pgTestDB is the shared Postgres store, migrated into a fixed schema and truncated per test.
var pgTestDB *PostgresStore

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

// setupPostgresTestDB recreates the opengate_test schema, whose literal name keeps the SQL static.
func setupPostgresTestDB(baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

// newPostgresTestStore returns the shared store after wiping all rows; tests run sequentially.
func newPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	require.NotNil(t, pgTestDB, "shared Postgres store not initialised by TestMain")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, truncatePostgresTestDB(ctx, pgTestDB))
	return pgTestDB
}
