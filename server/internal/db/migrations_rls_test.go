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

// Fixed identifiers for the probe role and schema. Keeping them compile-time
// literals means every statement below is static SQL.
const (
	rlsProbeRole   = "opengate_rls_probe"
	rlsProbeSchema = "opengate_rls_test"
)

// TestMigrationsApplyUnderForcedRowLevelSecurity runs the whole migration chain
// under the privilege model the deployed environments use: the migrating role
// is NOSUPERUSER and NOBYPASSRLS, and it owns tables that carry FORCE ROW LEVEL
// SECURITY. The tenant policies read app.current_org with no missing_ok
// fallback, so any migration statement that touches rows aborts the migration
// unless the connection carries cross-tenant scope.
//
// The test container's default role is a superuser, which bypasses row-level
// security outright — so without this test a migration can be green across the
// entire suite and still fail on deploy.
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

	// The cross-tenant scope belongs to the migration connection alone. If it
	// ever reached the pool that serves requests, every tenant policy in the
	// schema would evaluate true and row-level isolation would be gone.
	var isAdmin, currentOrg string
	require.NoError(t, store.DB().QueryRowContext(ctx,
		`SELECT coalesce(current_setting('app.is_admin', true), ''),
		        coalesce(current_setting('app.current_org', true), '')`,
	).Scan(&isAdmin, &currentOrg))
	require.NotEqual(t, "true", isAdmin, "application pool must not inherit the migration admin scope")
	require.Empty(t, currentOrg, "application pool must not inherit a migration tenant scope")
}

// setupRLSProbe creates the unprivileged probe role and a schema it owns, and
// registers teardown. The role has no LOGIN: the connection authenticates as
// the superuser and drops into the role via the startup `role` parameter, so
// no test credential is needed.
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

// rlsProbeURL pins the connection to the probe schema and switches it into the
// probe role at startup. The `options` parameter is already populated here, so
// this also covers the migration connection merging its own scope in rather
// than replacing what the caller asked for.
func rlsProbeURL(baseURL string) string {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	params := url.Values{}
	params.Set("search_path", rlsProbeSchema)
	params.Set("options", "-c role="+rlsProbeRole)
	return baseURL + sep + params.Encode()
}
