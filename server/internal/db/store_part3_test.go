package db

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMultitenancyRLSCrossTenantDeny(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	siteA := uuid.New()
	siteB := uuid.New()
	deviceA := uuid.New()
	deviceB := uuid.New()

	ensureRLSRole(t, ctx, s.db)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2), ($3, $4)`,
		tenantA, "Tenant A", tenantB, "Tenant B")
	require.NoError(t, err)

	insertTenantFixture := func(tenantID, userID, siteID, deviceID uuid.UUID, email string) {
		t.Helper()
		tx := beginTenantTx(t, ctx, s.db, tenantID, false)
		defer tx.Rollback() //nolint:errcheck // harmless after Commit
		_, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'hash')`,
			userID, tenantID, email)
		require.NoError(t, err)
		organizationID := uuid.New()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO organizations (id, tenant_id, name) VALUES ($1, $2, $3)`,
			organizationID, tenantID, "Customer "+organizationID.String())
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO sites (id, tenant_id, organization_id, name) VALUES ($1, $2, $3, $4)`,
			siteID, tenantID, organizationID, "owned")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO devices (id, tenant_id, organization_id, site_id, hostname) VALUES ($1, $2, $3, $4, $5)`,
			deviceID, tenantID, organizationID, siteID, "host-"+email)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}
	insertTenantFixture(tenantA, userA, siteA, deviceA, "a@example.com")
	insertTenantFixture(tenantB, userB, siteB, deviceB, "b@example.com")

	unscopedTx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer unscopedTx.Rollback() //nolint:errcheck // harmless after failed assertion
	_, err = unscopedTx.ExecContext(ctx, `SET LOCAL ROLE opengate_rls_test`)
	require.NoError(t, err)
	var unscopedCount int
	err = unscopedTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&unscopedCount)
	require.Error(t, err, "tenant tables must fail closed without app.current_tenant")

	txA := beginTenantTx(t, ctx, s.db, tenantA, false)
	defer txA.Rollback() //nolint:errcheck // harmless after Commit
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA)
	var tenantBVisibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id = $1`, deviceB).Scan(&tenantBVisibleToA))
	assert.Zero(t, tenantBVisibleToA)

	adminTx := beginTenantTx(t, ctx, s.db, tenantA, true)
	defer adminTx.Rollback() //nolint:errcheck // harmless after Commit
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}
