package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rehearsal assertions for migration 013: the three tables that hold what a
// customer changed about a rule and which machines cannot evaluate one.
//
// Two properties matter more than the columns. First, the isolation is real
// against a role that is not the table owner — forced row-level security, and a
// tenant that cannot see another's rows. Second, the tables cannot be made to
// name a customer belonging to a different tenant, because the composite key
// refuses it rather than an application check somebody has to remember to run.

// The ids these assertions seed. They sit in their own range so nothing else in
// the rehearsal collides with them.
const (
	rulesTenantA = "00000000-0000-0000-0000-000000000002"
	rulesTenantB = "00000000-0000-0000-0000-000000000202"
	rulesOrgA    = "00000000-0000-0000-0000-000000000501"
	rulesOrgB    = "00000000-0000-0000-0000-000000000502"
	rulesDeviceA = "00000000-0000-0000-0000-000000000503"
)

// rulesTables is every table migration 013 creates.
var rulesTables = []string{"rule_bindings", "rule_rollout", "rule_coverage_unsupported"}

// assertRulesIntroduced confirms migration 013 built the rule configuration
// tables with the isolation and the keys they depend on.
func assertRulesIntroduced(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()

	for _, table := range rulesTables {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+table).Scan(&reg))
		assert.Truef(t, reg.Valid, "%s should exist after migration 013", table)

		assert.Equalf(t, 1, policyCount(t, ctx, db, "tenant_isolation_"+table),
			"%s should carry its tenant policy", table)
		assertForcedRowSecurity(t, ctx, db, table)
		assertTenantLeadingIndex(t, ctx, db, table)
	}

	// These assertions seed customers and machines of their own. They run
	// against the migrated database and again against its dump/restore copy,
	// which carries the first run's rows, so they clear their own fixtures on
	// both ends: once so a second run starts clean, once so the rehearsal's
	// shared counts still describe what the migrations built.
	clearRulesFixtures(t, ctx, db)
	defer clearRulesFixtures(t, ctx, db)

	assertRulesRLSDeniesAcrossTenants(t, ctx, db, schemaName)
	assertRuleRowsCannotNameAnotherTenantsCustomer(t, ctx, db)
	assertRuleSelectorPrecedenceIsUnique(t, ctx, db)
	assertRuleCoverageFollowsItsDevice(t, ctx, db)
}

// clearRulesFixtures removes everything the assertions below seed. Deleting the
// two customers is enough: their machines, bindings, rollout state and coverage
// rows all cascade from them.
func clearRulesFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rehearsalExecNoTx(t, ctx, db,
		`DELETE FROM organizations WHERE id IN ($1, $2)`, rulesOrgA, rulesOrgB)
}

// assertForcedRowSecurity proves the policy also binds the table's owner. Row
// security that stops at the owner is not a wall, because the application
// connects as one.
func assertForcedRowSecurity(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()
	var enabled, forced bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`,
		"public."+table).Scan(&enabled, &forced))
	assert.Truef(t, enabled, "%s should have row-level security enabled", table)
	assert.Truef(t, forced, "%s should force row-level security on its owner too", table)
}

// assertTenantLeadingIndex proves a scoped read never has to scan another
// tenant's rows: some index on the table leads with the tenant.
func assertTenantLeadingIndex(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_indexes
		  WHERE schemaname = 'public' AND tablename = $1
		    AND indexdef LIKE '%(tenant_id, organization_id%'`, table).Scan(&count))
	assert.Positivef(t, count, "%s should have a tenant-leading index", table)
}

// seedRulesFixtures inserts one customer per tenant plus a machine in the first,
// which is the least the isolation and key assertions need to point at.
func seedRulesFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO organizations (id, tenant_id, name) VALUES ($1, $2, 'Contoso Rules'), ($3, $4, 'Fabrikam Rules')
		 ON CONFLICT DO NOTHING`, rulesOrgA, rulesTenantA, rulesOrgB, rulesTenantB)
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO devices (id, tenant_id, organization_id, hostname) VALUES ($1, $2, $3, 'rules-fs01')
		 ON CONFLICT DO NOTHING`, rulesDeviceA, rulesTenantA, rulesOrgA)
}

// assertRulesRLSDeniesAcrossTenants proves the wall holds for a role that is not
// the table owner: each tenant sees its own row and nothing else, while an admin
// context sees both.
func assertRulesRLSDeniesAcrossTenants(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()
	const roleName = "opengate_rls_rehearsal"
	ensureRLSRoleInSchema(t, ctx, db, roleName, schemaName)
	seedRulesFixtures(t, ctx, db)

	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO rule_rollout (tenant_id, organization_id, rule_id, enabled)
		 VALUES ($1, $2, 'disk-critical', TRUE), ($3, $4, 'disk-critical', TRUE)
		 ON CONFLICT DO NOTHING`, rulesTenantA, rulesOrgA, rulesTenantB, rulesOrgB)

	txA := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse(rulesTenantA), false)
	defer txA.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_rollout`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA, "a tenant should see only its own rollout state")

	// The wall also refuses a write into the other tenant's customer.
	_, err := txA.ExecContext(ctx,
		`INSERT INTO rule_rollout (tenant_id, organization_id, rule_id) VALUES ($1, $2, 'cpu-saturated')`,
		rulesTenantB, rulesOrgB)
	assert.Error(t, err, "a tenant must not be able to write another tenant's rollout state")

	adminTx := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse(rulesTenantA), true)
	defer adminTx.Rollback() //nolint:errcheck // harmless after assertions
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_rollout`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}

// assertRuleRowsCannotNameAnotherTenantsCustomer proves the composite key does
// the refusing. A row pairing one tenant with another's customer is the mismatch
// that would quietly break every scoped read built on it.
func assertRuleRowsCannotNameAnotherTenantsCustomer(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedRulesFixtures(t, ctx, db)

	_, err := db.ExecContext(ctx,
		`INSERT INTO rule_bindings (id, tenant_id, organization_id, rule_id, level, level_key)
		 VALUES ($1, $2, $3, 'disk-critical', 'organization', $3)`,
		uuid.New(), rulesTenantA, rulesOrgB)
	assert.Error(t, err, "a binding must not pair one tenant with another's customer")

	_, err = db.ExecContext(ctx,
		`INSERT INTO rule_coverage_unsupported (tenant_id, organization_id, device_id, rule_id)
		 VALUES ($1, $2, $3, 'io-stalled')`,
		rulesTenantA, rulesOrgB, rulesDeviceA)
	assert.Error(t, err, "a coverage row must not pair one tenant with another's customer")
}

// assertRuleSelectorPrecedenceIsUnique proves the database refuses the one
// ambiguity resolution would otherwise have to guess its way out of.
func assertRuleSelectorPrecedenceIsUnique(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedRulesFixtures(t, ctx, db)

	insert := func(selector string, precedence int) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO rule_bindings (id, tenant_id, organization_id, rule_id, level, level_key, selector, precedence)
			 VALUES ($1, $2, $3, 'cpu-saturated', 'organization', $3, $4::jsonb, $5)`,
			uuid.New(), rulesTenantA, rulesOrgA, selector, precedence)
		return err
	}

	require.NoError(t, insert(`{"role": "file-server"}`, 10))
	assert.Error(t, insert(`{"env": "prod"}`, 10),
		"two selectors at one rung with one precedence must be refused")
	assert.NoError(t, insert(`{"env": "prod"}`, 20),
		"a stated precedence says which one wins, so the pair is allowed")

	// The rung's blanket binding carries no selector and needs no precedence of
	// its own, so it is outside the constraint.
	require.NoError(t, insert(`{}`, 10))
}

// assertRuleCoverageFollowsItsDevice proves a decommissioned machine stops being
// counted. A coverage row that outlived its machine would inflate a customer's
// blind spot forever.
func assertRuleCoverageFollowsItsDevice(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedRulesFixtures(t, ctx, db)

	const doomed = "00000000-0000-0000-0000-000000000504"
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO devices (id, tenant_id, organization_id, hostname) VALUES ($1, $2, $3, 'rules-doomed')
		 ON CONFLICT DO NOTHING`, doomed, rulesTenantA, rulesOrgA)
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO rule_coverage_unsupported (tenant_id, organization_id, device_id, rule_id)
		 VALUES ($1, $2, $3, 'io-stalled') ON CONFLICT DO NOTHING`, rulesTenantA, rulesOrgA, doomed)

	rehearsalExecNoTx(t, ctx, db, `DELETE FROM devices WHERE id = $1`, doomed)

	var left int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rule_coverage_unsupported WHERE device_id = $1`, doomed).Scan(&left))
	assert.Zero(t, left, "deleting a machine should take its coverage rows with it")
}

// assertRulesDownReversal confirms migration 013's down rollback dropped the
// three tables and the function and index they were the only users of.
func assertRulesDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range rulesTables {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+table).Scan(&reg))
		assert.Falsef(t, reg.Valid, "%s should be gone after the 013 rollback", table)
	}

	var functions int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_proc WHERE proname = 'app_tenant_visible'`).Scan(&functions))
	assert.Zero(t, functions, "the shared tenant test should be gone with its tables")

	var indexes int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_indexes
		  WHERE schemaname = 'public' AND indexname = 'organizations_tenant_id_id_key'`).Scan(&indexes))
	assert.Zero(t, indexes, "the composite key's target should be gone too")
}
