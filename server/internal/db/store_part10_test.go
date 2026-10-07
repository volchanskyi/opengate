package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queueIndexes holds the two page orders and the machine lookup that migration 015 adds.
var queueIndexes = []string{
	"idx_incidents_organization_id_last_seen_id",
	"idx_incidents_tenant_id_last_seen_id",
	"idx_alerts_incident_id_device_id",
}

func assertQueueIndexesIntroduced(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, index := range queueIndexes {
		assert.Truef(t, indexExists(t, ctx, db, index),
			"%s should exist after migration 015", index)
	}
	assert.False(t, indexExists(t, ctx, db, "idx_alerts_incident_id"),
		"the incident-only index is subsumed by the one carrying the machine, and keeping both "+
			"costs every alert write a second index for no read")
}

func assertQueueIndexesDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, index := range queueIndexes {
		assert.Falsef(t, indexExists(t, ctx, db, index),
			"%s should be gone after the 015 rollback", index)
	}
	assert.True(t, indexExists(t, ctx, db, "idx_alerts_incident_id"),
		"the index 015 replaced has to come back with the rollback")
}

// retentionIndexes holds the two timestamp-leading indexes the cross-tenant age sweep reads.
var retentionIndexes = []string{
	"idx_alerts_received_at",
	"idx_incidents_resolved_at",
}

func assertRetentionIndexesIntroduced(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, index := range retentionIndexes {
		assert.Truef(t, indexExists(t, ctx, db, index),
			"%s should exist after migration 017", index)
	}
}

func assertRetentionIndexesDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, index := range retentionIndexes {
		assert.Falsef(t, indexExists(t, ctx, db, index),
			"%s should be gone after the 017 rollback", index)
	}
}

func indexExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`,
		name).Scan(&count))
	return count > 0
}
