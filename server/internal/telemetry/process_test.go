package telemetry

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestPostgresProcessRepositoryTenantDeny(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := NewPostgresProcessRepository(store.DB())

	orgB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), orgB, false)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	ownerA := testutil.SeedUser(t, ctxA, store)
	groupA := testutil.SeedGroup(t, ctxA, store, ownerA.ID)
	deviceA := testutil.SeedDevice(t, ctxA, store, groupA.ID)

	ownerB := testutil.SeedUser(t, ctxB, store)
	groupB := testutil.SeedGroup(t, ctxB, store, ownerB.ID)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)

	ts := time.Now().UTC().Truncate(time.Second)
	hash := "0123456789abcdef"
	require.NoError(t, repo.UpsertReport(ctxA, deviceA.ID, ts, []ProcessSample{{
		Rank: 1, Basename: "tenant-a", PID: 101, CPU: 1.5, Mem: 2.5,
	}}))
	require.NoError(t, repo.UpsertReport(ctxB, deviceB.ID, ts, []ProcessSample{{
		Rank: 1, Basename: "tenant-b", CmdlineHash: &hash, PID: 202, CPU: 3.5, Mem: 4.5,
	}}))

	got, err := repo.ListLatest(ctxA, deviceA.ID, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "tenant-a", got[0].Basename)

	// A non-positive limit falls back to the default page size, so the single
	// seeded row is still returned (a stricter `< 0` guard would leave limit at
	// 0 and select nothing).
	got, err = repo.ListLatest(ctxA, deviceA.ID, 0)
	require.NoError(t, err)
	require.Len(t, got, 1, "limit=0 must fall back to the default page size")

	got, err = repo.ListLatest(ctxA, deviceB.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, got, "tenant A must not read tenant B process rows")

	_, err = repo.ListLatest(context.Background(), deviceA.ID, 10)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}

func TestUint32FromDBRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	_, err := uint32FromDB("rank", -1)
	require.Error(t, err)

	_, err = uint32FromDB("pid", 1<<32)
	require.Error(t, err)

	got, err := uint32FromDB("pid", 42)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), got)

	// Exact range boundaries must be accepted, pinning the `< 0` and
	// `> MaxUint32` comparisons against off-by-one mutations.
	got, err = uint32FromDB("rank", 0)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), got)

	got, err = uint32FromDB("pid", math.MaxUint32)
	require.NoError(t, err)
	assert.Equal(t, uint32(math.MaxUint32), got)
}
