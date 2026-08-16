package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rehearsal assertions for migration 015: the orders the triage queue is
// actually read in.
//
// An index is easy to treat as a performance detail and skip in a rehearsal,
// but these are the difference between a page of fifty and a scan of every open
// incident a customer has — and the scan passes every functional test, at every
// fixture size anybody writes by hand. So the rehearsal asserts the indexes
// exist by name, and that the narrower one they replace is gone rather than
// left behind alongside them.

// queueIndexes is what migration 015 adds: the two orders a page is read in,
// and the machine lookup the device page's strip is answered from.
var queueIndexes = []string{
	"idx_incidents_organization_id_last_seen_id",
	"idx_incidents_tenant_id_last_seen_id",
	"idx_alerts_incident_id_device_id",
}

// assertQueueIndexesIntroduced confirms migration 015 built the queue's orders
// and retired the index the machine lookup subsumes.
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

// assertQueueIndexesDownReversal confirms the rollback took the queue's orders
// away and put back the index it replaced — a rollback that dropped one without
// restoring the other would leave the erasure recount without an index.
func assertQueueIndexesDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, index := range queueIndexes {
		assert.Falsef(t, indexExists(t, ctx, db, index),
			"%s should be gone after the 015 rollback", index)
	}
	assert.True(t, indexExists(t, ctx, db, "idx_alerts_incident_id"),
		"the index 015 replaced has to come back with the rollback")
}

// indexExists reports whether an index of that name is defined.
func indexExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`,
		name).Scan(&count))
	return count > 0
}
