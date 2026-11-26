package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rolloutClockColumns are what a stage held for a time read: the two waiting periods and the clock.
var rolloutClockColumns = []string{"canary_hold_secs", "staged_hold_secs", "stage_entered_at"}

// assertRolloutClockDropped checks 024 left the rollout its shares and nothing that times a stage.
func assertRolloutClockDropped(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, column := range rolloutClockColumns {
		assert.Falsef(t, columnExists(t, ctx, db, "rule_rollout", column),
			"rule_rollout.%s times a stage, and an administrator moves it", column)
	}
	for _, column := range rolloutPaceColumns {
		assert.Truef(t, columnExists(t, ctx, db, "rule_rollout", column),
			"rule_rollout.%s names a stage's share, which stays", column)
	}
}

// assertRolloutClockRestored checks the 024 rollback put the clock back with 016's defaults.
func assertRolloutClockRestored(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for column, want := range map[string]string{
		"canary_hold_secs": "3600",
		"staged_hold_secs": "21600",
		"stage_entered_at": "now()",
	} {
		var fallback sql.NullString
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT column_default FROM information_schema.columns
			  WHERE table_schema = 'public' AND table_name = 'rule_rollout' AND column_name = $1`,
			column).Scan(&fallback), column)
		assert.Equalf(t, want, fallback.String, "rule_rollout.%s defaults as 016 left it", column)
	}

	var bounded int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_constraint
		  WHERE conrelid = 'public.rule_rollout'::regclass AND contype = 'c'
		    AND pg_get_constraintdef(oid) ~ 'hold_secs'`).Scan(&bounded))
	assert.Equal(t, 2, bounded, "each waiting period is bounded again")
}
