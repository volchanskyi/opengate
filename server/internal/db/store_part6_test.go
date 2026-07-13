package db

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"io"
	"strings"
	"testing"
)

func assertAllRowsBackfilledToDefaultOrg(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var orgCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM organizations WHERE id = '00000000-0000-0000-0000-000000000002'`).Scan(&orgCount))
	assert.Equal(t, 1, orgCount)

	for tableName, expectedRows := range map[string]int{
		"users":                  1,
		"groups_":                1,
		"devices":                1,
		"agent_sessions":         1,
		"web_push_subscriptions": 1,
		"audit_events":           1,
		"amt_devices":            1,
		"enrollment_tokens":      1,
		"security_groups":        2,
		"security_group_members": 1,
		"device_updates":         1,
		"device_hardware":        1,
		"device_logs":            1,
	} {
		query := fmt.Sprintf(`SELECT COUNT(*), COUNT(*) FILTER (WHERE org_id = '00000000-0000-0000-0000-000000000002') FROM %s`, sqlIdent(tableName))
		var total, defaultScoped int
		require.NoError(t, db.QueryRowContext(ctx, query).Scan(&total, &defaultScoped))
		assert.Equal(t, expectedRows, total, tableName)
		assert.Equal(t, total, defaultScoped, tableName)
		t.Logf("rehearsal: %s rows=%d default_org_rows=%d", tableName, total, defaultScoped)
	}
}

func insertSecondTenantRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck // harmless after Commit
	rehearsalExec(t, ctx, tx, `INSERT INTO organizations (id, name) VALUES ('00000000-0000-0000-0000-000000000202', 'Rehearsal Tenant B')`)
	rehearsalExec(t, ctx, tx, `INSERT INTO users (id, org_id, email, password_hash) VALUES ('00000000-0000-0000-0000-000000000201', '00000000-0000-0000-0000-000000000202', 'rehearsal-b@example.com', 'hash')`)
	rehearsalExec(t, ctx, tx, `INSERT INTO groups_ (id, org_id, name, owner_id) VALUES ('00000000-0000-0000-0000-000000000203', '00000000-0000-0000-0000-000000000202', 'rehearsal-b', '00000000-0000-0000-0000-000000000201')`)
	rehearsalExec(t, ctx, tx, `INSERT INTO devices (id, org_id, group_id, hostname) VALUES ('00000000-0000-0000-0000-000000000204', '00000000-0000-0000-0000-000000000202', '00000000-0000-0000-0000-000000000203', 'rehearsal-b')`)
	require.NoError(t, tx.Commit())
}

func assertTelemetryProcessRLS(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()
	const roleName = "opengate_rls_rehearsal"
	ensureRLSRoleInSchema(t, ctx, db, roleName, schemaName)

	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO device_processes (org_id, device_id, ts, rank, basename, pid, cpu, mem)
		 VALUES
		   ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000103', '2026-07-02T00:00:00Z', 1, 'tenant-a', 100, 1, 2),
		   ('00000000-0000-0000-0000-000000000202', '00000000-0000-0000-0000-000000000204', '2026-07-02T00:00:00Z', 1, 'tenant-b', 200, 3, 4)
		 ON CONFLICT DO NOTHING`)

	txA := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse("00000000-0000-0000-0000-000000000002"), false)
	defer txA.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_processes`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA)

	adminTx := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse("00000000-0000-0000-0000-000000000002"), true)
	defer adminTx.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_processes`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}

func assertInventoryRLS(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()
	const roleName = "opengate_rls_rehearsal"
	ensureRLSRoleInSchema(t, ctx, db, roleName, schemaName)

	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO device_inventory (org_id, device_id, kind, name, port, first_seen, last_seen)
		 VALUES
		   ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000103', 'port', 'tenant-a', 5432, '2026-07-02T00:00:00Z', '2026-07-02T00:00:00Z'),
		   ('00000000-0000-0000-0000-000000000202', '00000000-0000-0000-0000-000000000204', 'port', 'tenant-b', 6379, '2026-07-02T00:00:00Z', '2026-07-02T00:00:00Z')
		 ON CONFLICT DO NOTHING`)

	txA := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse("00000000-0000-0000-0000-000000000002"), false)
	defer txA.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_inventory`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA)

	adminTx := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse("00000000-0000-0000-0000-000000000002"), true)
	defer adminTx.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_inventory`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}

func rehearsalExecNoTx(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(ctx, query, args...)
	require.NoError(t, err)
}

func rehearsalExec(t *testing.T, ctx context.Context, tx *sql.Tx, query string, args ...any) {
	t.Helper()
	_, err := tx.ExecContext(ctx, query, args...)
	require.NoError(t, err)
}

func assertRehearsalRLS(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()
	const roleName = "opengate_rls_rehearsal"
	ensureRLSRoleInSchema(t, ctx, db, roleName, schemaName)

	txA := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse("00000000-0000-0000-0000-000000000002"), false)
	defer txA.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA)
	var tenantBVisible int
	require.NoError(t, txA.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM devices WHERE id = '00000000-0000-0000-0000-000000000204'`).Scan(&tenantBVisible))
	assert.Zero(t, tenantBVisible)

	adminTx := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse("00000000-0000-0000-0000-000000000002"), true)
	defer adminTx.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}

func dumpAndRestoreRehearsal(t *testing.T, ctx context.Context, container *postgres.PostgresContainer, dbURL string) string {
	t.Helper()
	const (
		restoreDB = "opengate_rehearsal_restore"
		dumpPath  = "/tmp/opengate-rehearsal.dump"
	)
	execContainerCommand(t, ctx, container, []string{"createdb", "-U", "opengate", restoreDB})
	execContainerCommand(t, ctx, container, []string{"pg_dump", "-U", "opengate", "-d", "opengate_rehearsal", "-Fc", "-f", dumpPath})
	execContainerCommand(t, ctx, container, []string{"pg_restore", "-U", "opengate", "-d", restoreDB, dumpPath})
	return restoredDatabaseURL(t, dbURL, restoreDB)
}

func execContainerCommand(t *testing.T, ctx context.Context, container *postgres.PostgresContainer, cmd []string) {
	t.Helper()
	code, output, err := container.Exec(ctx, cmd)
	require.NoError(t, err)
	outputBytes, readErr := io.ReadAll(output)
	require.NoError(t, readErr)
	require.Equalf(t, 0, code, "%s failed:\n%s", strings.Join(cmd, " "), string(outputBytes))
}
