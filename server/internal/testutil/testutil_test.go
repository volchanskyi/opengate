package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTestStoreIsFullyMigrated(t *testing.T) {
	t.Parallel()
	store := NewTestStore(t)
	ctx := context.Background()

	// Scoped to current_schema(): information_schema spans the whole database,
	// and sibling parallel tests hold their own schemas in it.
	var n int
	err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = current_schema()
		   AND table_name = 'devices' AND column_name = 'maintenance_on'`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 1, n, "devices.maintenance_on (last migration) must exist in a fresh test store")

	err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM security_groups`).Scan(&n)
	require.NoError(t, err)
	require.Positive(t, n, "seeded security_groups row must be present")
}

func TestNewTestStoresAreIsolated(t *testing.T) {
	t.Parallel()
	a := NewTestStore(t)
	b := NewTestStore(t)
	ctx := context.Background()

	user := SeedUser(t, ctx, a)

	var inA, inB int
	require.NoError(t, a.DB().QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = $1`, user.ID).Scan(&inA))
	require.NoError(t, b.DB().QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = $1`, user.ID).Scan(&inB))
	require.Equal(t, 1, inA, "the writing store must see its own row")
	require.Equal(t, 0, inB, "a sibling store must not see it")
}
