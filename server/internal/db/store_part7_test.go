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

// maintenanceColumnCount returns how many of migration 007's maintenance-mode
// columns are present on the devices table.
func maintenanceColumnCount(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'devices'
		  AND column_name IN ('maintenance_on', 'maintenance_since', 'maintenance_by', 'maintenance_reason')`).Scan(&count))
	return count
}

// assertMaintenanceColumns confirms migration 007 added the four maintenance
// columns to devices.
func assertMaintenanceColumns(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.Equal(t, 4, maintenanceColumnCount(t, ctx, db), "all four maintenance columns should exist after migration 007")
}

// assertMaintenanceColumnsDownReversal confirms the 007 down rollback dropped
// the maintenance columns.
func assertMaintenanceColumnsDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.Zero(t, maintenanceColumnCount(t, ctx, db), "maintenance columns should be gone after 007 down rollback")
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

// amtLinkColumnCount returns how many of migration 008's AMT columns are present
// on device_hardware.
func amtLinkColumnCount(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'device_hardware'
		  AND column_name IN ('system_uuid', 'amt_available', 'amt_version', 'amt_model', 'amt_firmware')`).Scan(&count))
	return count
}

// amtDeviceColumnNames returns the column names migration 008 moves off
// amt_devices, plus the device link it adds.
func amtDeviceColumnNames(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'amt_devices'
		  AND column_name IN ('device_id', 'hostname', 'model', 'firmware')
		ORDER BY column_name`)
	require.NoError(t, err)
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

// assertAMTDeviceLink confirms migration 008 moved the AMT attributes onto the
// hardware row, reduced amt_devices to connection state keyed by device, and
// discarded the seeded AMT row that matches no managed device — an AMT
// connection with no device has no organization to live in.
func assertAMTDeviceLink(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.Equal(t, 5, amtLinkColumnCount(t, ctx, db), "all five AMT hardware columns should exist after migration 008")
	assert.Equal(t, []string{"device_id"}, amtDeviceColumnNames(t, ctx, db),
		"amt_devices should keep connection state only, linked by device_id")

	var linked string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT device_id::text FROM amt_devices`).Scan(&linked),
		"exactly the linkable AMT row should survive — the orphan has no organization to live in")
	assert.Equal(t, "00000000-0000-0000-0000-000000000103", linked,
		"the surviving row should point at the device that shares its hostname")

	var indexed int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'idx_device_hardware_system_uuid'`).Scan(&indexed))
	assert.Equal(t, 1, indexed, "the CIRA lookup index on system_uuid should exist")
}

// assertAMTDeviceLinkDownReversal confirms the 008 down rollback restored the
// original amt_devices shape and removed the hardware columns.
func assertAMTDeviceLinkDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.Zero(t, amtLinkColumnCount(t, ctx, db), "AMT hardware columns should be gone after 008 down rollback")
	assert.Equal(t, []string{"firmware", "hostname", "model"}, amtDeviceColumnNames(t, ctx, db),
		"the 008 down rollback should restore the original amt_devices columns")
}
