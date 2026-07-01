package db

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
			users,
			organizations
		RESTART IDENTITY CASCADE`); err != nil {
		return fmt.Errorf("truncate tables: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO organizations (id, name)
		VALUES ('00000000-0000-0000-0000-000000000002', 'Default Organization')`); err != nil {
		return fmt.Errorf("seed default organization: %w", err)
	}
	// Re-seed the Administrators group normally inserted by migration 005.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO security_groups (id, org_id, name, description, is_system)
		VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 'Administrators', 'Full system access', TRUE)`); err != nil {
		return fmt.Errorf("seed administrators group: %w", err)
	}
	return nil
}

func TestPing(t *testing.T) {
	s := newPostgresTestStore(t)
	assert.NoError(t, s.Ping(context.Background()))
}

func TestStoreSize(t *testing.T) {
	s := newPostgresTestStore(t)
	size, err := s.Size(context.Background())
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))
}

// TestPostgresStoreDB exercises the DB() accessor used by metrics and test
// helpers to reach the underlying *sql.DB.
func TestPostgresStoreDB(t *testing.T) {
	s := newPostgresTestStore(t)
	pool := s.DB()
	require.NotNil(t, pool)
	require.NoError(t, pool.PingContext(context.Background()))
}
