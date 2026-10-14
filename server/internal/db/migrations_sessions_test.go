package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rehearsalHostID is the rehearsal's tenant-A host, the one its seeded sessions are held on.
const rehearsalHostID = "00000000-0000-0000-0000-000000000103"

func assertProcessReportRetired(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.False(t, tableExists(t, ctx, db, "public.device_processes"),
		"nothing reads or writes process rows, so the table is gone")
}

func assertProcessReportDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	require.True(t, tableExists(t, ctx, db, "public.device_processes"),
		"the 018 rollback restores the table the earlier rollbacks rename and drop")
	var enabled, forced bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.device_processes'::regclass`).
		Scan(&enabled, &forced))
	assert.True(t, enabled && forced, "the restored table is walled by tenant again")
	assert.Equal(t, 1, policiesReadingSettingOn(t, ctx, db, "device_processes", tenantScopeSettingAfter))
}

// seedHeldSessions gives the rehearsal host a second, newer session before 019 keeps one.
func seedHeldSessions(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO agent_sessions (token, tenant_id, device_id, user_id, created_at)
		 VALUES ('session-a-newer', '00000000-0000-0000-0000-000000000002', $1,
		         '00000000-0000-0000-0000-000000000101', NOW() + INTERVAL '1 minute')`,
		rehearsalHostID)
}

func assertOneSessionPerHost(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var tokens []string
	rows, err := db.QueryContext(ctx,
		`SELECT token FROM agent_sessions WHERE device_id = $1 ORDER BY token`, rehearsalHostID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var token string
		require.NoError(t, rows.Scan(&token))
		tokens = append(tokens, token)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"session-a-newer"}, tokens, "a host keeps its newest session")

	assert.True(t, indexExists(t, ctx, db, "idx_agent_sessions_one_per_device"))
	_, err = db.ExecContext(ctx,
		`INSERT INTO agent_sessions (token, tenant_id, device_id, user_id)
		 VALUES ('session-a-second', '00000000-0000-0000-0000-000000000002', $1,
		         '00000000-0000-0000-0000-000000000101')`, rehearsalHostID)
	require.Error(t, err, "a second session on a held host is refused by the database")

	var outcomes int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT link_outcomes FROM agent_sessions WHERE token = 'session-a-newer'`).Scan(&outcomes))
	assert.Zero(t, outcomes, "a session starts having reported no direct-link outcome")
}

func assertOneSessionPerHostDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.False(t, indexExists(t, ctx, db, "idx_agent_sessions_one_per_device"))
	assert.True(t, indexExists(t, ctx, db, "idx_agent_sessions_device_id"),
		"the plain lookup index 019 replaced comes back")
	assert.False(t, columnExists(t, ctx, db, "agent_sessions", "link_outcomes"))
}

func tableExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var reg sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, name).Scan(&reg))
	return reg.Valid
}

func policiesReadingSettingOn(t *testing.T, ctx context.Context, db *sql.DB, table, setting string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_policies
		WHERE schemaname = 'public' AND tablename = $1
		  AND coalesce(qual, '') LIKE '%' || $2 || '%'
		  AND coalesce(with_check, '') LIKE '%' || $2 || '%'`, table, setting).Scan(&count))
	return count
}
