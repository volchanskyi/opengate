package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ruleAdministrationTables = []string{
	"device_tag_labels",
	"device_tags",
	"organization_alert_limits",
	"rule_binding_clamps",
}

var rolloutPaceColumns = []string{
	"canary_percent",
	"staged_percent",
	"canary_hold_secs",
	"staged_hold_secs",
}

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

	// The automatic pull-back has no switch column.
	for _, column := range []string{"auto_revert", "rollback_enabled", "pull_back"} {
		assert.Falsef(t, columnExists(t, ctx, db, "rule_rollout", column),
			"rule_rollout must carry nothing that switches the automatic pull-back off, found %s", column)
	}

	assert.True(t, indexExists(t, ctx, db, "idx_alerts_organization_id_received_at_rule_id"),
		"the noise badge is one grouped read over a bounded window, which needs its index")
}

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

	// The rollout state from 013 stays.
	var reg sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public.rule_rollout").Scan(&reg))
	assert.True(t, reg.Valid, "rolling back 016 must leave the rollout state itself alone")
}

func columnExists(t *testing.T, ctx context.Context, db *sql.DB, table, column string) bool {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`,
		table, column).Scan(&count))
	return count > 0
}
