package db

import (
	"context"
	"database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/url"
	"strings"
	"testing"
	"time"
)

func assertMultitenancyDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var organizations sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.organizations')`).Scan(&organizations))
	assert.False(t, organizations.Valid)

	var orgIDColumns int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND column_name = 'org_id'
		  AND table_name IN (
		    'users', 'groups_', 'devices', 'agent_sessions', 'web_push_subscriptions',
		    'audit_events', 'amt_devices', 'enrollment_tokens', 'security_groups',
		    'security_group_members', 'device_updates', 'device_hardware', 'device_logs')`).Scan(&orgIDColumns))
	assert.Zero(t, orgIDColumns)
}

func assertTelemetryDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var deviceProcesses sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.device_processes')`).Scan(&deviceProcesses))
	assert.False(t, deviceProcesses.Valid)
}

// assertInventoryDownReversal confirms migration 005's down rollback dropped the
// device_inventory table.
func assertInventoryDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var deviceInventory sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.device_inventory')`).Scan(&deviceInventory))
	assert.False(t, deviceInventory.Valid)
}

// assertDataLifecycleTables confirms migration 006 created the non-RLS
// deleted_ids deny-list and purge_jobs progress tables.
func assertDataLifecycleTables(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"public.deleted_ids", "public.purge_jobs"} {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, table).Scan(&reg))
		assert.Truef(t, reg.Valid, "%s should exist after migration 006", table)
	}
}

// assertDataLifecycleDownReversal confirms the 006 down rollback dropped both
// lifecycle tables.
func assertDataLifecycleDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"public.deleted_ids", "public.purge_jobs"} {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, table).Scan(&reg))
		assert.Falsef(t, reg.Valid, "%s should be gone after 006 down rollback", table)
	}
}

// assertDeviceLogsRetired confirms migration 004 dropped the central log cache.
func assertDeviceLogsRetired(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var deviceLogs sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.device_logs')`).Scan(&deviceLogs))
	assert.False(t, deviceLogs.Valid)
}

// assertDeviceLogsRestored confirms the 004 down rollback recreated the table.
func assertDeviceLogsRestored(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var deviceLogs sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.device_logs')`).Scan(&deviceLogs))
	assert.True(t, deviceLogs.Valid)
}

func restoredDatabaseURL(t *testing.T, dbURL, dbName string) string {
	t.Helper()
	parsed, err := url.Parse(dbURL)
	require.NoError(t, err)
	parsed.Path = "/" + dbName
	return parsed.String()
}

func sqlIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func sqlQuoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
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
