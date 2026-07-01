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

	orgA := uuid.New()
	orgB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	groupA := uuid.New()
	groupB := uuid.New()
	deviceA := uuid.New()
	deviceB := uuid.New()

	ensureRLSRole(t, ctx, s.db)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (id, name) VALUES ($1, $2), ($3, $4)`,
		orgA, "Tenant A", orgB, "Tenant B")
	require.NoError(t, err)

	insertTenantFixture := func(orgID, userID, groupID, deviceID uuid.UUID, email string) {
		t.Helper()
		tx := beginTenantTx(t, ctx, s.db, orgID, false)
		defer tx.Rollback() //nolint:errcheck // harmless after Commit
		_, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, org_id, email, password_hash) VALUES ($1, $2, $3, 'hash')`,
			userID, orgID, email)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO groups_ (id, org_id, name, owner_id) VALUES ($1, $2, $3, $4)`,
			groupID, orgID, "owned", userID)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO devices (id, org_id, group_id, hostname) VALUES ($1, $2, $3, $4)`,
			deviceID, orgID, groupID, "host-"+email)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}
	insertTenantFixture(orgA, userA, groupA, deviceA, "a@example.com")
	insertTenantFixture(orgB, userB, groupB, deviceB, "b@example.com")

	unscopedTx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer unscopedTx.Rollback() //nolint:errcheck // harmless after failed assertion
	_, err = unscopedTx.ExecContext(ctx, `SET LOCAL ROLE opengate_rls_test`)
	require.NoError(t, err)
	var unscopedCount int
	err = unscopedTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&unscopedCount)
	require.Error(t, err, "tenant tables must fail closed without app.current_org")

	txA := beginTenantTx(t, ctx, s.db, orgA, false)
	defer txA.Rollback() //nolint:errcheck // harmless after Commit
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA)
	var orgBVisibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id = $1`, deviceB).Scan(&orgBVisibleToA))
	assert.Zero(t, orgBVisibleToA)

	adminTx := beginTenantTx(t, ctx, s.db, orgA, true)
	defer adminTx.Rollback() //nolint:errcheck // harmless after Commit
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}
