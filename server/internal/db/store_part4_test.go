package db

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/volchanskyi/opengate/server/internal/testpg"
	"strconv"
	"testing"
	"time"
)

func TestMultitenancyMigrationRehearsal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Intentionally ignores POSTGRES_TEST_URL: dump/restore is destructive and needs matching client binaries.
	container, err := postgres.Run(ctx, testpg.PostgresImage,
		postgres.WithDatabase("opengate_rehearsal"),
		postgres.WithUsername("opengate"),
		postgres.WithPassword("opengate"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Logf("rehearsal: started dedicated %s container for migration dump/restore", testpg.PostgresImage)
	t.Cleanup(func() {
		terminateCtx, cancelTerminate := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelTerminate()
		require.NoError(t, container.Terminate(terminateCtx))
	})

	dbURL, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	runMigrationSteps(t, dbURL, 1)
	rehearsalDB := openRehearsalDB(t, ctx, dbURL)
	defer rehearsalDB.Close() //nolint:errcheck // test cleanup
	seedPreTenancyRows(t, ctx, rehearsalDB)
	t.Log("rehearsal: applied 001 and seeded pre-tenancy rows")

	runMigrationSteps(t, dbURL, 1)
	assertAllRowsBackfilledToDefaultOrg(t, ctx, rehearsalDB)
	insertSecondTenantRows(t, ctx, rehearsalDB)
	assertRehearsalRLS(t, ctx, rehearsalDB, "public")
	t.Log("rehearsal: 002 backfill, idempotence, cross-tenant deny, and admin bypass verified")

	runMigrationSteps(t, dbURL, 1)
	assertTelemetryProcessRLS(t, ctx, rehearsalDB, "public")
	t.Log("rehearsal: 003 process telemetry table and RLS verified")

	runMigrationSteps(t, dbURL, 1)
	assertDeviceLogsRetired(t, ctx, rehearsalDB)
	t.Log("rehearsal: 004 retired device_logs")

	runMigrationSteps(t, dbURL, 1)
	assertInventoryRLS(t, ctx, rehearsalDB, "public")
	t.Log("rehearsal: 005 discovery inventory table and RLS verified")

	runMigrationSteps(t, dbURL, 1)
	assertDataLifecycleTables(t, ctx, rehearsalDB)
	t.Log("rehearsal: 006 data-lifecycle tables verified")

	runMigrationSteps(t, dbURL, 1)
	assertMaintenanceColumns(t, ctx, rehearsalDB)
	assertMigrationNoChange(t, dbURL)
	t.Log("rehearsal: 007 maintenance columns verified; head is idempotent")

	restoreURL := dumpAndRestoreRehearsal(t, ctx, container, dbURL)
	restoredDB := openRehearsalDB(t, ctx, restoreURL)
	defer restoredDB.Close() //nolint:errcheck // test cleanup
	assertRehearsalRLS(t, ctx, restoredDB, "public")
	assertTelemetryProcessRLS(t, ctx, restoredDB, "public")
	assertDeviceLogsRetired(t, ctx, restoredDB)
	assertInventoryRLS(t, ctx, restoredDB, "public")
	assertDataLifecycleTables(t, ctx, restoredDB)
	assertMaintenanceColumns(t, ctx, restoredDB)
	t.Log("rehearsal: pg_dump -> pg_restore completed and restored DB re-verified")

	runMigrationSteps(t, dbURL, -1)
	assertMaintenanceColumnsDownReversal(t, ctx, rehearsalDB)
	t.Log("rehearsal: 007 down rollback removed maintenance columns cleanly")

	runMigrationSteps(t, dbURL, -1)
	assertDataLifecycleDownReversal(t, ctx, rehearsalDB)
	t.Log("rehearsal: 006 down rollback removed data-lifecycle tables cleanly")

	runMigrationSteps(t, dbURL, -1)
	assertInventoryDownReversal(t, ctx, rehearsalDB)
	t.Log("rehearsal: 005 down rollback removed device_inventory cleanly")

	runMigrationSteps(t, dbURL, -1)
	assertDeviceLogsRestored(t, ctx, rehearsalDB)
	t.Log("rehearsal: 004 down rollback recreated device_logs cleanly")

	runMigrationSteps(t, dbURL, -1)
	assertTelemetryDownReversal(t, ctx, rehearsalDB)
	t.Log("rehearsal: 003 down rollback removed device_processes cleanly")

	runMigrationSteps(t, dbURL, -1)
	assertMultitenancyDownReversal(t, ctx, rehearsalDB)
	t.Log("rehearsal: 002 down rollback removed organizations/org_id cleanly")
}

func ensureRLSRole(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'opengate_rls_test') THEN
				CREATE ROLE opengate_rls_test;
			END IF;
		END $$;
		GRANT USAGE ON SCHEMA opengate_test TO opengate_rls_test;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA opengate_test TO opengate_rls_test;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA opengate_test TO opengate_rls_test`)
	require.NoError(t, err)
}

func ensureRLSRoleInSchema(t *testing.T, ctx context.Context, db *sql.DB, roleName, schemaName string) {
	t.Helper()
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN
				CREATE ROLE %s;
			END IF;
		END $$;
		GRANT USAGE ON SCHEMA %s TO %s;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO %s;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s`,
		sqlQuoteLiteral(roleName),
		sqlIdent(roleName),
		sqlIdent(schemaName), sqlIdent(roleName),
		sqlIdent(schemaName), sqlIdent(roleName),
		sqlIdent(schemaName), sqlIdent(roleName)))
	require.NoError(t, err)
}

func beginTenantTx(t *testing.T, ctx context.Context, db *sql.DB, orgID uuid.UUID, isAdmin bool) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL ROLE opengate_rls_test`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx,
		`SELECT set_config('app.current_org', $1, true), set_config('app.is_admin', $2, true)`,
		orgID.String(), strconv.FormatBool(isAdmin))
	require.NoError(t, err)
	return tx
}
