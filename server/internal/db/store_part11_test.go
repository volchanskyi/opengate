package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rehearsal assertions for migration 016: what an operator can change about a
// curated rule, and the labels a rule can be aimed at.
//
// Every table it adds is keyed on the customer and walled at the tenant, so each
// carries the same three things every other tenant table does — a policy that
// also binds the owner, a tenant-leading index, and a composite key that refuses
// a customer belonging to somebody else. The rollout columns are asserted by
// name because they are settings an operator moves; a column silently missing
// would leave the screen writing to a default it cannot change.

// ruleAdministrationTables is what migration 016 adds.
var ruleAdministrationTables = []string{
	"device_tag_labels",
	"device_tags",
	"organization_alert_limits",
	"rule_binding_clamps",
}

// rolloutPaceColumns are the settings 016 adds to the rollout state: how far
// each stage reaches, and how long it is held.
var rolloutPaceColumns = []string{
	"canary_percent",
	"staged_percent",
	"canary_hold_secs",
	"staged_hold_secs",
}

// assertRuleAdministrationIntroduced confirms migration 016 built the tables the
// rules screen writes to, each behind the same wall as every other tenant table.
func assertRuleAdministrationIntroduced(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	for _, table := range ruleAdministrationTables {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+table).Scan(&reg))
		assert.Truef(t, reg.Valid, "%s should exist after migration 016", table)

		assert.Equalf(t, 1, policyCount(t, ctx, db, "tenant_isolation_"+table),
			"%s should carry its tenant policy", table)
		assertForcedRowSecurity(t, ctx, db, table)
		assertTenantLeadingIndex(t, ctx, db, table)
	}

	for _, column := range rolloutPaceColumns {
		assert.Truef(t, columnExists(t, ctx, db, "rule_rollout", column),
			"rule_rollout.%s should exist after migration 016", column)
	}

	// The pull-back is not configuration, so no column can switch it off. A
	// column added later would be the route a struct field never was.
	for _, column := range []string{"auto_revert", "rollback_enabled", "pull_back"} {
		assert.Falsef(t, columnExists(t, ctx, db, "rule_rollout", column),
			"rule_rollout must carry nothing that switches the automatic pull-back off, found %s", column)
	}

	assert.True(t, indexExists(t, ctx, db, "idx_alerts_organization_id_received_at_rule_id"),
		"the noise badge is one grouped read over a bounded window, which needs its index")
}

// assertRuleAdministrationDownReversal confirms the rollback took all of it away
// — including the rollout columns, which a rollback that dropped only the tables
// would leave behind pointing at settings nothing writes.
func assertRuleAdministrationDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	for _, table := range ruleAdministrationTables {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+table).Scan(&reg))
		assert.Falsef(t, reg.Valid, "%s should be gone after the 016 rollback", table)
	}

	for _, column := range rolloutPaceColumns {
		assert.Falsef(t, columnExists(t, ctx, db, "rule_rollout", column),
			"rule_rollout.%s should be gone after the 016 rollback", column)
	}

	assert.False(t, indexExists(t, ctx, db, "idx_alerts_organization_id_received_at_rule_id"),
		"the noise index should be gone after the 016 rollback")

	// What 013 built is still standing: rolling back the screen must not take
	// the rules with it.
	var reg sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public.rule_rollout").Scan(&reg))
	assert.True(t, reg.Valid, "rolling back 016 must leave the rollout state itself alone")
}

// columnExists reports whether a table carries a column of that name.
func columnExists(t *testing.T, ctx context.Context, db *sql.DB, table, column string) bool {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`,
		table, column).Scan(&count))
	return count > 0
}
