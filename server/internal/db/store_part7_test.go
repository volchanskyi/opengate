package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/url"
	"strings"
	"testing"
	"time"
)

func assertMultitenancyDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var tenants sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.organizations')`).Scan(&tenants))
	assert.False(t, tenants.Valid)

	var tenantIDColumns int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND column_name = 'org_id'
		  AND table_name IN (
		    'users', 'groups_', 'devices', 'agent_sessions', 'web_push_subscriptions',
		    'audit_events', 'amt_devices', 'enrollment_tokens', 'security_groups',
		    'security_group_members', 'device_updates', 'device_hardware', 'device_logs')`).Scan(&tenantIDColumns))
	assert.Zero(t, tenantIDColumns)
}

// tenantScopedTables lists every table carrying the tenant scope column at head.
// deleted_ids and purge_jobs carry it without a policy: they are the erasure
// deny-list and progress log, which must outlive the rows they describe.
var tenantScopedTables = []string{
	"users", "groups_", "devices", "agent_sessions", "web_push_subscriptions",
	"audit_events", "amt_devices", "enrollment_tokens", "security_groups",
	"security_group_members", "device_updates", "device_hardware",
	"device_processes", "device_inventory", "deleted_ids", "purge_jobs",
}

const (
	tenantScopeSettingBefore = "app.current_org"
	tenantScopeSettingAfter  = "app.current_tenant"
)

// tenantScopeColumnCount counts how many of tenantScopedTables carry a column of
// the given name.
func tenantScopeColumnCount(t *testing.T, ctx context.Context, db *sql.DB, column string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = $1 AND table_name = ANY($2)`,
		column, tenantScopedTables).Scan(&count))
	return count
}

// policiesReadingSetting counts the tenant policies whose USING or WITH CHECK
// expression reads the given scope setting.
func policiesReadingSetting(t *testing.T, ctx context.Context, db *sql.DB, setting string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_policies
		WHERE schemaname = 'public' AND policyname LIKE 'tenant_isolation_%'
		  AND coalesce(qual, '') LIKE '%' || $1 || '%'
		  AND coalesce(with_check, '') LIKE '%' || $1 || '%'`, setting).Scan(&count))
	return count
}

// assertTenancyRenamed confirms the rename migration moved the scope table, the
// scope column on every table that carries it, and the setting every tenant
// policy reads — and that nothing keeps the introduced names.
func assertTenancyRenamed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var tenants, organizations sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.tenants')`).Scan(&tenants))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.organizations')`).Scan(&organizations))
	assert.True(t, tenants.Valid, "the tenants table should exist after the rename")
	assert.False(t, organizations.Valid, "the tenants table should not be reachable under its introduced name")

	assert.Equal(t, len(tenantScopedTables), tenantScopeColumnCount(t, ctx, db, "tenant_id"),
		"every tenant-scoped table should carry tenant_id")
	assert.Zero(t, tenantScopeColumnCount(t, ctx, db, "org_id"),
		"no table should keep the introduced scope column name")

	assert.Positive(t, policiesReadingSetting(t, ctx, db, tenantScopeSettingAfter),
		"tenant policies should read app.current_tenant")
	assert.Zero(t, policiesReadingSetting(t, ctx, db, tenantScopeSettingBefore),
		"no tenant policy should still read app.current_org")

	var defaultName string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT name FROM tenants WHERE id = '00000000-0000-0000-0000-000000000002'`).Scan(&defaultName))
	assert.Equal(t, "Default Tenant", defaultName)
}

// assertTenancyRenameDownReversal confirms the rename rollback put every name
// back, so the migrations below it keep operating on the schema they expect.
func assertTenancyRenameDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var tenants, organizations sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.tenants')`).Scan(&tenants))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.organizations')`).Scan(&organizations))
	assert.False(t, tenants.Valid, "the rename rollback should remove the tenants name")
	assert.True(t, organizations.Valid, "the rename rollback should restore the organizations name")

	assert.Equal(t, len(tenantScopedTables), tenantScopeColumnCount(t, ctx, db, "org_id"))
	assert.Zero(t, tenantScopeColumnCount(t, ctx, db, "tenant_id"))
	assert.Positive(t, policiesReadingSetting(t, ctx, db, tenantScopeSettingBefore))
	assert.Zero(t, policiesReadingSetting(t, ctx, db, tenantScopeSettingAfter))
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
// connection with no device has no tenant to live in.
func assertAMTDeviceLink(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.Equal(t, 5, amtLinkColumnCount(t, ctx, db), "all five AMT hardware columns should exist after migration 008")
	assert.Equal(t, []string{"device_id"}, amtDeviceColumnNames(t, ctx, db),
		"amt_devices should keep connection state only, linked by device_id")

	var linked string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT device_id::text FROM amt_devices`).Scan(&linked),
		"exactly the linkable AMT row should survive — the orphan has no tenant to live in")
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

// groupOwnerNullability reports whether groups_.owner_id exists and, when it
// does, whether the column accepts NULL.
func groupOwnerNullability(t *testing.T, ctx context.Context, db *sql.DB) (bool, bool) {
	t.Helper()
	var nullable sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'groups_' AND column_name = 'owner_id'`).Scan(&nullable)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false
	}
	require.NoError(t, err)
	return true, nullable.String == "YES"
}

// groupOwnerIndexCount reports how many indexes cover the dropped
// (tenant_id, owner_id) pair.
func groupOwnerIndexCount(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'idx_groups_org_id_owner_id'`).Scan(&count))
	return count
}

// assertGroupOwnerDropped confirms migration 009 removed the group-ownership
// column and the index built on it. Tenant is the visibility boundary, so
// nothing reads a group owner any more.
//
// tenantColumn names the group's tenant scope column, which the rename
// migration moves — the caller passes whichever name the schema is on.
func assertGroupOwnerDropped(t *testing.T, ctx context.Context, db *sql.DB, tenantColumn string) {
	t.Helper()
	exists, _ := groupOwnerNullability(t, ctx, db)
	assert.False(t, exists, "groups_.owner_id should be gone after migration 009")
	assert.Zero(t, groupOwnerIndexCount(t, ctx, db), "the owner index should be gone after migration 009")

	// The surviving rows keep their tenant, which is what scopes them now.
	var orphaned int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM groups_ WHERE %s IS NULL`, sqlIdent(tenantColumn))).Scan(&orphaned))
	assert.Zero(t, orphaned, "every group should still carry its tenant")
}

// assertGroupOwnerDownReversal confirms the 009 down rollback re-adds owner_id.
// Dropping a column is lossy, so the restored column is nullable — the original
// NOT NULL cannot be recreated without the data it held.
func assertGroupOwnerDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	exists, nullable := groupOwnerNullability(t, ctx, db)
	assert.True(t, exists, "groups_.owner_id should be back after the 009 down rollback")
	assert.True(t, nullable, "the restored owner_id is nullable — the dropped values cannot be recovered")
	assert.Equal(t, 1, groupOwnerIndexCount(t, ctx, db), "the owner index should be back after the 009 down rollback")
}
