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

// startRehearsalContainer brings up the throwaway Postgres the rehearsal walks
// its migrations against, and returns it with its connection string. It
// intentionally ignores POSTGRES_TEST_URL: dump/restore is destructive and needs
// matching client binaries.
func startRehearsalContainer(t *testing.T, ctx context.Context) (*postgres.PostgresContainer, string) {
	t.Helper()
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
	return container, dbURL
}

func TestMultitenancyMigrationRehearsal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, dbURL := startRehearsalContainer(t, ctx)

	runMigrationSteps(t, dbURL, 1)
	rehearsalDB := openRehearsalDB(t, ctx, dbURL)
	defer rehearsalDB.Close() //nolint:errcheck // test cleanup
	seedPreTenancyRows(t, ctx, rehearsalDB)
	t.Log("rehearsal: applied 001 and seeded pre-tenancy rows")

	// The forward walk, in the shape the rollback walk below reads backwards:
	// apply one migration, then prove what that migration built. A step may also
	// seed the rows the *next* one has to sort, which is what `before` is for.
	for _, step := range forwardMigrationSteps() {
		if step.before != nil {
			step.before(t, ctx, rehearsalDB)
		}
		runMigrationSteps(t, dbURL, 1)
		step.verify(t, ctx, rehearsalDB)
		t.Logf("rehearsal: %s", step.note)
	}
	assertMigrationNoChange(t, dbURL)
	t.Log("rehearsal: head is idempotent")

	restoreURL := dumpAndRestoreRehearsal(t, ctx, container, dbURL)
	restoredDB := openRehearsalDB(t, ctx, restoreURL)
	defer restoredDB.Close() //nolint:errcheck // test cleanup
	assertHeadSchema(t, ctx, restoredDB)
	t.Log("rehearsal: pg_dump -> pg_restore completed and restored DB re-verified")

	rollBackAndVerify(t, ctx, dbURL, rehearsalDB)
}

// rehearsalStep is one migration and what it has to have done. before seeds the
// rows that migration will sort, and runs while the schema is still the previous
// one.
type rehearsalStep struct {
	note   string
	before func(*testing.T, context.Context, *sql.DB)
	verify func(*testing.T, context.Context, *sql.DB)
}

// forwardMigrationSteps is migrations 002 onward. 001 is applied before the
// loop, because the connection every step below shares is opened against it.
func forwardMigrationSteps() []rehearsalStep {
	return []rehearsalStep{
		{note: "002 backfill, idempotence, cross-tenant deny, and admin bypass verified",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertAllRowsBackfilledToDefaultTenant(t, ctx, db)
				insertSecondTenantRows(t, ctx, db)
				assertRehearsalRLS(t, ctx, db, "public")
			}},
		{note: "003 process telemetry table and RLS verified",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				seedTelemetryProcessRows(t, ctx, db)
				assertTelemetryProcessRLS(t, ctx, db, "public")
			}},
		{note: "004 retired device_logs", verify: assertDeviceLogsRetired},
		{note: "005 discovery inventory table and RLS verified",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				seedInventoryRows(t, ctx, db)
				assertInventoryRLS(t, ctx, db, "public")
			}},
		{note: "006 data-lifecycle tables verified", verify: assertDataLifecycleTables},
		{note: "007 maintenance columns verified", verify: assertMaintenanceColumns},
		{note: "008 AMT device link verified",
			// Give 008 both a linkable and an unlinkable AMT row to sort. Seeded
			// here rather than pre-tenancy so the 002 backfill assertions keep
			// their one-row-per-table shape.
			before: func(t *testing.T, ctx context.Context, db *sql.DB) {
				rehearsalExecNoTx(t, ctx, db,
					`INSERT INTO amt_devices (uuid, org_id, hostname)
					 VALUES ('00000000-0000-0000-0000-000000000107', '00000000-0000-0000-0000-000000000002', 'rehearsal-a')`)
			},
			verify: assertAMTDeviceLink},
		{note: "009 dropped groups_.owner_id",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertSiteOwnerDropped(t, ctx, db, "groups_", "org_id")
			}},
		{note: "010 renamed the tenancy vocabulary",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertTenancyRenamed(t, ctx, db, "groups_")
				assertOrganizationsNameIsFree(t, ctx, db)
			}},
		{note: "011 gave every tenant a customer and every device an owner",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertOrganizationsIntroduced(t, ctx, db, "public")
			}},
		{note: "012 reparented the filing level under the customer",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertSitesIntroduced(t, ctx, db, "public")
			}},
		{note: "013 added rule bindings, rollout and coverage",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertRulesIntroduced(t, ctx, db, "public")
			}},
		{note: "014 added alerts, incidents and incident events",
			verify: func(t *testing.T, ctx context.Context, db *sql.DB) {
				assertInvestigationsIntroduced(t, ctx, db, "public")
			}},
	}
}

// assertHeadSchema re-runs every head-state assertion against db. It is applied
// to the pg_dump/pg_restore copy, so a restored database must look exactly like
// the one the migrations built.
func assertHeadSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assertRehearsalRLS(t, ctx, db, "public")
	assertTelemetryProcessRLS(t, ctx, db, "public")
	assertDeviceLogsRetired(t, ctx, db)
	assertInventoryRLS(t, ctx, db, "public")
	assertDataLifecycleTables(t, ctx, db)
	assertMaintenanceColumns(t, ctx, db)
	assertAMTDeviceLink(t, ctx, db)
	assertSiteOwnerDropped(t, ctx, db, "sites", "tenant_id")
	assertTenancyRenamed(t, ctx, db, "sites")
	assertOrganizationsIntroduced(t, ctx, db, "public")
	assertSitesIntroduced(t, ctx, db, "public")
	assertRulesIntroduced(t, ctx, db, "public")
	assertInvestigationsIntroduced(t, ctx, db, "public")
}

// rollBackAndVerify walks the migrations down one step at a time, asserting
// after each that the step reversed cleanly. The slice is in rollback order —
// newest migration first — and reaches the pre-tenancy schema at the end.
func rollBackAndVerify(t *testing.T, ctx context.Context, dbURL string, db *sql.DB) {
	t.Helper()
	steps := []struct {
		note   string
		verify func(*testing.T, context.Context, *sql.DB)
	}{
		{"014 removed the alert, incident and incident-event tables", assertInvestigationsDownReversal},
		{"013 removed the rule binding, rollout and coverage tables", assertRulesDownReversal},
		{"012 returned the filing level to a flat tenant label", assertSitesDownReversal},
		{"011 removed organizations and the device link cleanly", assertOrganizationsDownReversal},
		{"010 restored the introduced tenancy names", assertTenancyRenameDownReversal},
		{"009 re-added a nullable owner_id", assertSiteOwnerDownReversal},
		{"008 restored the original amt_devices shape", assertAMTDeviceLinkDownReversal},
		{"007 removed maintenance columns cleanly", assertMaintenanceColumnsDownReversal},
		{"006 removed data-lifecycle tables cleanly", assertDataLifecycleDownReversal},
		{"005 removed device_inventory cleanly", assertInventoryDownReversal},
		{"004 recreated device_logs cleanly", assertDeviceLogsRestored},
		{"003 removed device_processes cleanly", assertTelemetryDownReversal},
		{"002 removed tenants/tenant_id cleanly", assertMultitenancyDownReversal},
	}
	for _, step := range steps {
		runMigrationSteps(t, dbURL, -1)
		step.verify(t, ctx, db)
		t.Logf("rehearsal: down rollback %s", step.note)
	}
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

func beginTenantTx(t *testing.T, ctx context.Context, db *sql.DB, tenantID uuid.UUID, isAdmin bool) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL ROLE opengate_rls_test`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, setRehearsalTenantScopeSQL,
		tenantID.String(), strconv.FormatBool(isAdmin))
	require.NoError(t, err)
	return tx
}
