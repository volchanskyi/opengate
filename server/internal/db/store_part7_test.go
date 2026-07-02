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
