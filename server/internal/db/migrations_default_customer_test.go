package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rehearsalTenantB          = "00000000-0000-0000-0000-000000000202"
	rehearsalEarliestCustomer = "00000000-0000-0000-0000-000000000901"
)

// seedEarliestCustomer gives rehearsal tenant B a customer older than the one 011 made for it.
func seedEarliestCustomer(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO organizations (id, tenant_id, name, created_at)
		 VALUES ($1, $2, 'Rehearsal Earliest', NOW() - INTERVAL '1 day')`,
		rehearsalEarliestCustomer, rehearsalTenantB)
}

// assertDefaultCustomerMarked checks 023 marked each tenant's oldest customer and no other.
func assertDefaultCustomerMarked(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var misplaced int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM organizations o
		  WHERE o.is_default <> (o.id = (SELECT f.id FROM organizations f
		                                  WHERE f.tenant_id = o.tenant_id
		                                  ORDER BY f.created_at, f.id LIMIT 1))`).Scan(&misplaced))
	assert.Zero(t, misplaced, "each tenant's oldest customer is its default, and no other is")

	var earliest bool
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT is_default FROM organizations WHERE id = $1`, rehearsalEarliestCustomer).Scan(&earliest))
	assert.True(t, earliest, "the oldest is read from the creation time, whatever the name")

	assertOneDefaultPerTenant(t, ctx, db)
}

// assertOneDefaultPerTenant checks every tenant with a customer has one default, and the database
// refuses a second.
func assertOneDefaultPerTenant(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var tenantsOff int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (
		   SELECT tenant_id FROM organizations
		    GROUP BY tenant_id
		   HAVING COUNT(*) FILTER (WHERE is_default) <> 1) off`).Scan(&tenantsOff))
	assert.Zero(t, tenantsOff, "every tenant with a customer has exactly one default")
	assert.True(t, indexExists(t, ctx, db, "idx_organizations_one_default"))

	_, err := db.ExecContext(ctx,
		`UPDATE organizations SET is_default = TRUE WHERE tenant_id = $1 AND NOT is_default`,
		rehearsalTenantB)
	assert.Error(t, err, "a second default in one tenant is refused")
}

func assertDefaultCustomerDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	assert.False(t, columnExists(t, ctx, db, "organizations", "is_default"))
	assert.False(t, indexExists(t, ctx, db, "idx_organizations_one_default"))
}
