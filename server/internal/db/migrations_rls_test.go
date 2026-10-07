package db

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testpg"
)

const (
	rlsProbeRole   = "opengate_rls_probe"
	rlsProbeSchema = "opengate_rls_test"
)

func TestMigrationsApplyUnderForcedRowLevelSecurity(t *testing.T) {
	baseURL := testpg.BaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admin, err := sql.Open("pgx", baseURL)
	require.NoError(t, err, "open admin connection")
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	setupRLSProbe(ctx, t, admin)

	store, err := NewPostgresStore(ctx, rlsProbeURL(baseURL))
	require.NoError(t, err, "migrations must apply as a NOBYPASSRLS role under FORCE ROW LEVEL SECURITY")
	t.Cleanup(func() { _ = store.Close() })

	// The cross-tenant scope settings stay on the migration connection, off the request pool.
	var isAdmin, currentTenant, currentOrg string
	require.NoError(t, store.DB().QueryRowContext(ctx,
		`SELECT coalesce(current_setting('app.is_admin', true), ''),
		        coalesce(current_setting('app.current_tenant', true), ''),
		        coalesce(current_setting('app.current_org', true), '')`,
	).Scan(&isAdmin, &currentTenant, &currentOrg))
	require.NotEqual(t, "true", isAdmin, "application pool must not inherit the migration admin scope")
	require.Empty(t, currentTenant, "application pool must not inherit a migration tenant scope")
	require.Empty(t, currentOrg, "application pool must not inherit a migration tenant scope")
}

// The probe role has no LOGIN; connections drop into it through the startup role parameter.
func setupRLSProbe(ctx context.Context, t *testing.T, admin *sql.DB) {
	t.Helper()

	_, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+rlsProbeSchema+` CASCADE`)
	require.NoError(t, err, "drop stale probe schema")

	_, err = admin.ExecContext(ctx, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+rlsProbeRole+`') THEN
				CREATE ROLE `+rlsProbeRole+` NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION;
			END IF;
		END
		$$`)
	require.NoError(t, err, "create probe role")

	_, err = admin.ExecContext(ctx, `CREATE SCHEMA `+rlsProbeSchema+` AUTHORIZATION `+rlsProbeRole)
	require.NoError(t, err, "create probe schema")

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+rlsProbeSchema+` CASCADE`); err != nil {
			t.Logf("drop probe schema: %v", err)
		}
		if _, err := admin.ExecContext(cleanupCtx, `DROP OWNED BY `+rlsProbeRole+` CASCADE`); err != nil {
			t.Logf("drop probe role objects: %v", err)
		}
		if _, err := admin.ExecContext(cleanupCtx, `DROP ROLE IF EXISTS `+rlsProbeRole); err != nil {
			t.Logf("drop probe role: %v", err)
		}
	})
}

// The options parameter is pre-populated so the migration connection merges its own scope in.
func rlsProbeURL(baseURL string) string {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	params := url.Values{}
	params.Set("search_path", rlsProbeSchema)
	params.Set("options", "-c role="+rlsProbeRole)

	// A space is written %20 because a literal + reaches Postgres as the option name "+role".
	return baseURL + sep + strings.ReplaceAll(params.Encode(), "+", "%20")
}
